package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/emr/boulevard"
)

// checkAvailability handles the "check_availability" tool call by querying
// the clinic's EMR (Moxie or Boulevard) for open appointment slots.
func (h *ToolHandler) checkAvailability(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Service            string `json:"service"`
		PreferredDays      string `json:"preferred_days"`
		PreferredTimes     string `json:"preferred_times"`
		ProviderPreference string `json:"provider_preference"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("checkAvailability: parse input: %w", err)
	}

	if h.deps == nil || h.deps.ClinicStore == nil {
		h.logger.Warn("voice-tool: check_availability — no clinic store, returning fallback")
		return `{"message": "I don't have access to the scheduling system right now. Let me take your preferences and I'll text you available times."}`, nil
	}

	cfg, err := h.deps.ClinicStore.Get(ctx, h.orgID)
	if err != nil {
		return "", fmt.Errorf("checkAvailability: get clinic config: %w", err)
	}

	loc := time.FixedZone("EST", -5*60*60)
	if cfg.Timezone != "" {
		if l, err := time.LoadLocation(cfg.Timezone); err == nil {
			loc = l
		}
	}
	now := time.Now().In(loc)

	// Parse "after X" time preferences (e.g., "after 4", "after 3pm")
	afterHour := -1
	if pref := strings.ToLower(params.PreferredTimes); pref != "" {
		afterHour = parseAfterHour(pref)
	}

	// Route to Boulevard or Moxie based on clinic config
	if cfg.BookingPlatform == "boulevard" {
		return h.checkBoulevardAvailability(ctx, cfg, params.Service, params.ProviderPreference, now, loc, afterHour, params.PreferredTimes)
	}

	return h.checkMoxieAvailability(ctx, cfg, params.Service, now, loc, afterHour, params.PreferredTimes)
}

// parseAfterHour extracts the hour from "after 4", "after 4pm", "after 3:00 PM" etc.
// Returns -1 if no "after" pattern found.
func parseAfterHour(pref string) int {
	pref = strings.ToLower(strings.TrimSpace(pref))

	// Match "after X" patterns
	prefixes := []string{"after ", "past "}
	for _, prefix := range prefixes {
		if idx := strings.Index(pref, prefix); idx >= 0 {
			rest := strings.TrimSpace(pref[idx+len(prefix):])
			// Remove "pm"/"am" suffix
			rest = strings.TrimSuffix(rest, "pm")
			rest = strings.TrimSuffix(rest, "am")
			rest = strings.TrimSpace(rest)
			// Remove ":00" etc.
			if colonIdx := strings.Index(rest, ":"); colonIdx >= 0 {
				rest = rest[:colonIdx]
			}
			var hour int
			if _, err := fmt.Sscanf(rest, "%d", &hour); err == nil {
				// If hour <= 12 and original had "pm" or no am/pm and >= 1 && <= 6, assume PM
				if hour <= 12 && (strings.Contains(pref, "pm") || (hour >= 1 && hour <= 6 && !strings.Contains(pref, "am"))) {
					if hour != 12 {
						hour += 12
					}
				}
				return hour
			}
		}
	}
	return -1
}

// filterSlotByTime checks if a slot time matches the preferred time filter.
// afterHour: strict "after" filter (exclusive — "after 4" excludes 4:00).
// prefTimes: general preference like "morning", "afternoon", "evening".
func filterSlotByTime(t time.Time, afterHour int, prefTimes string) bool {
	hour := t.Hour()

	// Strict "after X" filter takes priority.
	// "After 4" excludes 4:00 but includes 4:30 and later.
	if afterHour >= 0 {
		if hour > afterHour {
			return true
		}
		if hour < afterHour {
			return false
		}
		return t.Minute() > 0 || t.Second() > 0
	}

	// General time-of-day preferences
	pref := strings.ToLower(prefTimes)
	switch {
	case pref == "morning":
		return hour < 12
	case pref == "afternoon":
		return hour >= 12 && hour < 17
	case pref == "evening":
		return hour >= 17
	}
	return true // no filter
}

// checkBoulevardAvailability queries the Boulevard API for available appointment
// slots and filters them by time preferences.
func (h *ToolHandler) checkBoulevardAvailability(ctx context.Context, cfg *clinic.Config, service, provider string, now time.Time, loc *time.Location, afterHour int, prefTimes string) (string, error) {
	if cfg.BoulevardBusinessID == "" || cfg.BoulevardLocationID == "" {
		h.logger.Warn("voice-tool: check_availability — no boulevard config")
		return `{"message": "I'm having trouble checking availability right now. I'll text you available times shortly."}`, nil
	}

	// Create per-clinic Boulevard client and adapter
	blvdClient := boulevard.NewBoulevardClient(cfg.BoulevardBusinessID, cfg.BoulevardLocationID, nil)
	dryRun := true // always dry-run for voice (no real bookings yet)
	adapter := boulevard.NewBoulevardAdapter(blvdClient, dryRun, nil)

	// Resolve service name via aliases
	resolvedService := service
	if cfg.ServiceAliases != nil {
		normalized := strings.ToLower(strings.TrimSpace(service))
		if alias, ok := cfg.ServiceAliases[normalized]; ok {
			resolvedService = alias
		}
	}

	slots, err := adapter.ResolveAvailability(ctx, resolvedService, provider, now)
	if err != nil {
		h.logger.Error("voice-tool: boulevard availability error", "error", err)
		return `{"message": "I'm having trouble checking availability right now. I'll text you available times shortly."}`, nil
	}

	var filtered []string
	for _, slot := range slots {
		t := slot.StartAt
		if t.Before(now) {
			continue // skip past slots
		}
		if !filterSlotByTime(t, afterHour, prefTimes) {
			continue
		}
		filtered = append(filtered, t.In(loc).Format("Monday, January 2 at 3:04 PM"))
		if len(filtered) >= 5 {
			break
		}
	}

	if len(filtered) == 0 {
		return `{"message": "I don't see any available slots matching your preferences in the next two weeks. Would you like me to check different days or times?"}`, nil
	}

	slotsJSON, _ := json.Marshal(filtered)
	return fmt.Sprintf(`{"available_slots": %s}`, string(slotsJSON)), nil
}

// checkMoxieAvailability queries the Moxie API for available appointment slots
// and filters them by time preferences.
func (h *ToolHandler) checkMoxieAvailability(ctx context.Context, cfg *clinic.Config, service string, now time.Time, loc *time.Location, afterHour int, prefTimes string) (string, error) {
	if h.deps.MoxieClient == nil || cfg.MoxieConfig == nil {
		return `{"message": "Online scheduling is not configured for this clinic. I'll text you available times shortly."}`, nil
	}

	// Resolve service to Moxie service menu item ID
	serviceMenuItemID := ""
	if cfg.MoxieConfig.ServiceMenuItems != nil {
		normalized := strings.ToLower(strings.TrimSpace(service))
		serviceMenuItemID = cfg.MoxieConfig.ServiceMenuItems[normalized]
		if serviceMenuItemID == "" && cfg.ServiceAliases != nil {
			if alias, ok := cfg.ServiceAliases[normalized]; ok {
				serviceMenuItemID = cfg.MoxieConfig.ServiceMenuItems[strings.ToLower(alias)]
			}
		}
	}
	if serviceMenuItemID == "" {
		return fmt.Sprintf(`{"message": "I couldn't find the service '%s' in our booking system. Could you tell me more about what you're looking for?"}`, service), nil
	}

	startDate := now.Format("2006-01-02")
	endDate := now.AddDate(0, 0, 14).Format("2006-01-02")

	result, err := h.deps.MoxieClient.GetAvailableSlots(ctx, cfg.MoxieConfig.MedspaID, startDate, endDate, serviceMenuItemID, true)
	if err != nil {
		h.logger.Error("voice-tool: moxie availability error", "error", err)
		return `{"message": "I'm having trouble checking availability right now. I'll text you available times shortly."}`, nil
	}

	var slots []string
	for _, ds := range result.Dates {
		for _, slot := range ds.Slots {
			t, err := time.Parse(time.RFC3339, slot.Start)
			if err != nil {
				continue
			}
			t = t.In(loc)
			if t.Before(now) {
				continue // skip past slots
			}
			if !filterSlotByTime(t, afterHour, prefTimes) {
				continue
			}
			slots = append(slots, t.Format("Monday, January 2 at 3:04 PM"))
			if len(slots) >= 5 {
				break
			}
		}
		if len(slots) >= 5 {
			break
		}
	}

	if len(slots) == 0 {
		return `{"message": "I don't see any available slots matching your preferences in the next two weeks. Would you like me to check different days or times?"}`, nil
	}

	slotsJSON, _ := json.Marshal(slots)
	return fmt.Sprintf(`{"available_slots": %s}`, string(slotsJSON)), nil
}
