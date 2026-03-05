package conversation

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	moxieclient "github.com/wolfman30/medspa-ai-platform/internal/emr/moxie"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
)

// FetchAvailableTimesFromMoxieAPI fetches available time slots directly from
// Moxie's GraphQL API.
func FetchAvailableTimesFromMoxieAPI(
	ctx context.Context,
	moxie *moxieclient.Client,
	cfg *clinic.Config,
	serviceName string,
	prefs TimePreferences,
	onProgress func(ctx context.Context, msg string),
	patientFacingServiceName ...string,
) (*AvailabilityResult, error) {
	return FetchAvailableTimesFromMoxieAPIWithProvider(ctx, moxie, cfg, serviceName, "", prefs, onProgress, patientFacingServiceName...)
}

// FetchAvailableTimesFromMoxieAPIWithProvider is like FetchAvailableTimesFromMoxieAPI
// but also filters by provider preference name (e.g., "Gale").
func FetchAvailableTimesFromMoxieAPIWithProvider(
	ctx context.Context,
	moxie *moxieclient.Client,
	cfg *clinic.Config,
	serviceName string,
	providerPreference string,
	prefs TimePreferences,
	onProgress func(ctx context.Context, msg string),
	patientFacingServiceName ...string,
) (*AvailabilityResult, error) {
	if moxie == nil || cfg == nil || cfg.MoxieConfig == nil {
		return nil, fmt.Errorf("moxie API not configured")
	}

	mc := cfg.MoxieConfig
	// Resolve service to Moxie serviceMenuItemId
	normalizedService := strings.ToLower(serviceName)
	serviceMenuItemID := mc.ServiceMenuItems[normalizedService]
	if serviceMenuItemID == "" {
		resolved := cfg.ResolveServiceName(normalizedService)
		serviceMenuItemID = mc.ServiceMenuItems[strings.ToLower(resolved)]
	}
	if serviceMenuItemID == "" {
		return nil, fmt.Errorf("no serviceMenuItemId for service %q", serviceName)
	}

	displayName := serviceName
	if len(patientFacingServiceName) > 0 && patientFacingServiceName[0] != "" {
		displayName = patientFacingServiceName[0]
	}
	if onProgress != nil {
		onProgress(ctx, fmt.Sprintf("Checking available times for %s... this may take a moment.", displayName))
	}

	// Search 3 months out in one API call
	now := time.Now()
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		loc = time.UTC
	}
	today := now.In(loc)
	startDate := today.Format("2006-01-02")
	endDate := today.AddDate(0, 3, 0).Format("2006-01-02")

	// Resolve provider preference to Moxie userMedspaId
	providerID := cfg.ResolveProviderID(providerPreference)
	noProviderPref := providerID == ""

	// Try noPreference=true first for "no preference" patients.
	// Moxie quirk: this returns empty for many clinics, so we fall back.
	var result *moxieclient.AvailabilityResult
	if noProviderPref {
		r, err := moxie.GetAvailableSlots(ctx, mc.MedspaID, startDate, endDate, serviceMenuItemID, true)
		if err != nil {
			return nil, fmt.Errorf("moxie availability query failed: %w", err)
		}
		if countMoxieSlots(r) > 0 {
			result = r
		}
	}

	// If noPreference returned nothing (Moxie quirk) or patient chose a provider,
	// query per-provider. Fan out to all providers for "no preference" patients.
	if result == nil {
		log.Printf("[DEBUG] fan-out check: noProviderPref=%v providerNames=%d serviceProviders=%d serviceMenuItemID=%s",
			noProviderPref, len(mc.ProviderNames), len(mc.ServiceProviders), serviceMenuItemID)
		if noProviderPref && mc.ProviderNames != nil && len(mc.ProviderNames) > 0 {
			// Fan out: prefer service-specific providers if available
			providerIDs := mc.ServiceProviders[serviceMenuItemID]
			if len(providerIDs) == 0 {
				// Fall back to all providers
				providerIDs = make([]string, 0, len(mc.ProviderNames))
				for pid := range mc.ProviderNames {
					providerIDs = append(providerIDs, pid)
				}
			}
			log.Printf("[DEBUG] fan-out: querying %d providers for service %s", len(providerIDs), serviceMenuItemID)
			result = &moxieclient.AvailabilityResult{}
			for _, pid := range providerIDs {
				r, err := moxie.GetAvailableSlots(ctx, mc.MedspaID, startDate, endDate, serviceMenuItemID, false, pid)
				if err != nil {
					log.Printf("[DEBUG] fan-out: provider %s error: %v", pid, err)
					continue // skip failing providers
				}
				slotCount := countMoxieSlots(r)
				log.Printf("[DEBUG] fan-out: provider %s returned %d slots", pid, slotCount)
				result.Dates = append(result.Dates, r.Dates...)
			}
		} else {
			// Specific provider requested, or single-provider fallback
			if noProviderPref && mc.DefaultProviderID != "" {
				providerID = mc.DefaultProviderID
			}
			r, err := moxie.GetAvailableSlots(ctx, mc.MedspaID, startDate, endDate, serviceMenuItemID, false, providerID)
			if err != nil {
				return nil, fmt.Errorf("moxie availability query failed: %w", err)
			}
			result = r
		}
	}

	// Convert API response to PresentedSlots, filtering by preferences.
	// Deduplicate by start time (fan-out queries may return the same slot
	// from multiple providers).
	seen := make(map[int64]bool)
	var allSlots []PresentedSlot
	for _, dateSlots := range result.Dates {
		if len(dateSlots.Slots) == 0 {
			continue
		}
		for _, slot := range dateSlots.Slots {
			slotLocal, err := ParseSlotTime(slot.Start, cfg.Timezone)
			if err != nil {
				continue
			}
			key := slotLocal.Unix()
			if seen[key] {
				continue
			}
			seen[key] = true
			if matchesTimePreferences(slotLocal, prefs) {
				allSlots = append(allSlots, PresentedSlot{
					DateTime:  slotLocal,
					TimeStr:   formatSlotForDisplay(slotLocal),
					Service:   serviceName,
					Available: true,
				})
			}
		}
	}

	// Sort by date/time
	sort.Slice(allSlots, func(i, j int) bool {
		return allSlots[i].DateTime.Before(allSlots[j].DateTime)
	})

	// Spread slots across multiple days (max 2 per day, aim for 3+ days)
	allSlots = spreadSlotsAcrossDays(allSlots, maxSlotsToPresent, 2)

	// Assign indices
	for i := range allSlots {
		allSlots[i].Index = i + 1
	}

	if len(allSlots) == 0 {
		return &AvailabilityResult{
			Slots:        nil,
			ExactMatch:   false,
			SearchedDays: maxCalendarDays,
			Message:      fmt.Sprintf("I searched 3 months of availability for %s but couldn't find times matching your preferences. Would you like to try different days or times?", displayName),
		}, nil
	}

	return &AvailabilityResult{
		Slots:        allSlots,
		ExactMatch:   true,
		SearchedDays: maxCalendarDays,
	}, nil
}

// countMoxieSlots returns the total number of slots in a Moxie availability result.
func countMoxieSlots(r *moxieclient.AvailabilityResult) int {
	if r == nil {
		return 0
	}
	n := 0
	for _, d := range r.Dates {
		n += len(d.Slots)
	}
	return n
}

// matchesTimePreferences checks if a slot time matches user preferences
func matchesTimePreferences(slotTime time.Time, prefs TimePreferences) bool {
	// Check DaysOfWeek
	if len(prefs.DaysOfWeek) > 0 {
		weekday := int(slotTime.Weekday())
		match := false
		for _, d := range prefs.DaysOfWeek {
			if d == weekday {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}

	// Check AfterTime — "after 3pm" means strictly after, not at 3:00 PM
	if prefs.AfterTime != "" {
		afterMinutes := parseTimeToMinutes(prefs.AfterTime)
		slotMinutes := slotTime.Hour()*60 + slotTime.Minute()
		if slotMinutes <= afterMinutes {
			return false
		}
	}

	// Check BeforeTime
	if prefs.BeforeTime != "" {
		beforeMinutes := parseTimeToMinutes(prefs.BeforeTime)
		slotMinutes := slotTime.Hour()*60 + slotTime.Minute()
		if slotMinutes >= beforeMinutes {
			return false
		}
	}

	return true
}

// parseTimeToMinutes converts "HH:MM" to minutes since midnight
func parseTimeToMinutes(timeStr string) int {
	parts := strings.Split(timeStr, ":")
	if len(parts) != 2 {
		return 0
	}
	hours, _ := strconv.Atoi(parts[0])
	minutes, _ := strconv.Atoi(parts[1])
	return hours*60 + minutes
}

// ShouldFetchAvailability checks if we have all required info to fetch availability.
// Returns true if we have: name, service, time preferences, and patient type.
// Note: Email is NOT required here - for Moxie clinics, email is collected on the booking page.
func ShouldFetchAvailability(history []ChatMessage, lead interface{}) bool {
	return ShouldFetchAvailabilityWithConfig(history, lead, nil)
}

// ShouldFetchAvailabilityWithConfig checks whether all required qualifications are met
// to trigger an availability fetch. When cfg is non-nil and the service has multiple
// providers, provider preference is also required.
func ShouldFetchAvailabilityWithConfig(history []ChatMessage, lead interface{}, cfg *clinic.Config) bool {
	prefs, ok := extractPreferences(history, serviceAliasesFromConfig(cfg))
	if !ok {
		log.Printf("[DEBUG] ShouldFetchAvailability: extractPreferences returned not ok")
		return false
	}

	// Merge with saved lead preferences from system context messages.
	// This handles the case where early user messages got trimmed from history
	// but the lead's saved preferences are injected as system context.
	mergeLeadContextIntoPrefs(&prefs, history)

	log.Printf("[DEBUG] ShouldFetchAvailability: name=%q service=%q patientType=%q days=%q times=%q providerPref=%q",
		prefs.Name, prefs.ServiceInterest, prefs.PatientType, prefs.PreferredDays, prefs.PreferredTimes, prefs.ProviderPreference)

	// Must have name
	if prefs.Name == "" {
		return false
	}

	// Must have service
	if prefs.ServiceInterest == "" {
		return false
	}

	// Must have patient type
	if prefs.PatientType == "" {
		return false
	}

	// Must have some scheduling preferences (days or times)
	if prefs.PreferredDays == "" && prefs.PreferredTimes == "" {
		return false
	}

	// If provider preference is empty, try matching against known providers from config.
	// This handles cases like "I want lip filler with Gale" where the patient volunteers
	// a provider name before the assistant ever lists providers.
	if cfg != nil && prefs.ProviderPreference == "" {
		prefs.ProviderPreference = matchProviderFromConfig(history, cfg)
		if prefs.ProviderPreference != "" {
			log.Printf("[DEBUG] ShouldFetchAvailability: matched provider %q from config", prefs.ProviderPreference)
		}
	}

	// If the service has multiple providers, must have provider preference.
	// BUT: skip this check if the service has variants (in-person/virtual) —
	// the variant question will be asked first during availability fetch,
	// and the resolved variant may only have 1 provider.
	if cfg != nil && prefs.ProviderPreference == "" {
		hasVariants := len(cfg.GetServiceVariants(prefs.ServiceInterest)) > 0
		if !hasVariants && cfg.ServiceNeedsProviderPreference(prefs.ServiceInterest) {
			log.Printf("[DEBUG] ShouldFetchAvailability: service %q needs provider preference (multiple providers)", prefs.ServiceInterest)
			return false
		}
	}

	// Email is collected on the Moxie booking page, not via SMS
	return true
}

// matchProviderFromConfig checks if any user message contains a known provider's
// first name from the clinic config. This is the most reliable source since it
// doesn't depend on fragile pattern matching in system prompt text.
func matchProviderFromConfig(history []ChatMessage, cfg *clinic.Config) string {
	if cfg == nil || cfg.MoxieConfig == nil || cfg.MoxieConfig.ProviderNames == nil {
		return ""
	}

	// Collect all user message text
	var userText strings.Builder
	for _, msg := range history {
		if msg.Role == ChatRoleUser {
			userText.WriteString(strings.ToLower(msg.Content))
			userText.WriteString(" ")
		}
	}
	lower := userText.String()

	// Check each provider's first name against user messages
	for _, fullName := range cfg.MoxieConfig.ProviderNames {
		parts := strings.Fields(fullName)
		if len(parts) == 0 {
			continue
		}
		firstName := strings.ToLower(parts[0])
		if len(firstName) < 3 {
			continue // Skip very short names to avoid false positives
		}
		if strings.Contains(lower, firstName) {
			return fullName
		}
	}
	return ""
}

// mergeLeadContextIntoPrefs fills in missing preferences from system context messages
// that contain saved lead data (e.g., "- Name: Andrea Jones", "- Service: lip filler").
// This handles history trimming: even when early user messages are gone, the lead's
// saved preferences are re-injected by appendContext on every turn.
func mergeLeadContextIntoPrefs(prefs *leads.SchedulingPreferences, history []ChatMessage) {
	for _, msg := range history {
		if msg.Role != ChatRoleSystem {
			continue
		}
		if !strings.Contains(msg.Content, "scheduling preferences") && !strings.Contains(msg.Content, "patient preferences") {
			continue
		}
		lines := strings.Split(msg.Content, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "- ") {
				continue
			}
			parts := strings.SplitN(line[2:], ": ", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(strings.ToLower(parts[0]))
			val := strings.TrimSpace(parts[1])
			if val == "" {
				continue
			}
			switch {
			case (key == "name" || key == "name (first only)") && prefs.Name == "":
				prefs.Name = val
			case key == "service" && prefs.ServiceInterest == "":
				prefs.ServiceInterest = val
			case key == "patient type" && prefs.PatientType == "":
				prefs.PatientType = val
			case key == "preferred days" && prefs.PreferredDays == "":
				prefs.PreferredDays = val
			case key == "preferred times" && prefs.PreferredTimes == "":
				prefs.PreferredTimes = val
			case key == "provider preference" && prefs.ProviderPreference == "":
				prefs.ProviderPreference = val
			}
		}
	}
}
