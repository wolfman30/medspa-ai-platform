package conversation

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/browser"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	moxieclient "github.com/wolfman30/medspa-ai-platform/internal/moxie"
)

// PresentedSlot represents a time slot that was presented to the user
type PresentedSlot struct {
	Index     int       // 1-based index shown to user
	DateTime  time.Time // Full date and time
	TimeStr   string    // Display string like "Mon Feb 10 at 10:00 AM"
	Service   string    // Service name
	Available bool      // Whether it was available when presented
}

// TimeSelectionState tracks the state of time selection for a conversation
type TimeSelectionState struct {
	PresentedSlots []PresentedSlot // Slots shown to user
	Service        string          // Service being booked
	BookingURL     string          // Clinic booking URL
	PresentedAt    time.Time       // When options were presented
	SlotSelected   bool            // True after patient picks a slot (prevents re-scraping)
}

// maxSlotsToPresent is the maximum number of slots to show at once
const maxSlotsToPresent = 6

// daysToSearch is the number of days to search for availability (non-fallback).
const daysToSearch = 7

// perDateTimeout is the timeout in ms for each individual date fetch.
const perDateTimeout = 15000

// batchSize is the number of days per progressive search batch.
const batchSize = 14

// maxCalendarDays is the Moxie calendar horizon (~3 months).
const maxCalendarDays = 90

// relaxedFallbackDays is the window for relaxed-preference fallback searches.
const relaxedFallbackDays = 28

// CalendarSlotsProvider is an optional capability of the browser client.
// Used via type assertion so existing mocks/tests don't break.
type CalendarSlotsProvider interface {
	GetCalendarSlots(ctx context.Context, req browser.CalendarSlotsRequest) (*browser.CalendarSlotsResponse, error)
}

// dateResult holds the result of fetching availability for a single date.
type dateResult struct {
	dateStr string
	slots   []PresentedSlot
}

// AvailabilityResult wraps the output of FetchAvailableTimesWithFallback.
type AvailabilityResult struct {
	Slots        []PresentedSlot
	ExactMatch   bool   // true if slots match user preferences
	SearchedDays int    // how many days were searched
	Message      string // message for when no slots found at all
}

// FetchAvailableTimes fetches available times from the browser sidecar
// and filters based on user preferences. Dates are fetched in parallel.
func FetchAvailableTimes(
	ctx context.Context,
	adapter *BrowserAdapter,
	bookingURL string,
	serviceName string,
	prefs TimePreferences,
) ([]PresentedSlot, error) {
	return fetchSlotsForDateRange(ctx, adapter, bookingURL, serviceName, prefs, 0, daysToSearch)
}

// FetchAvailableTimesWithFallback progressively searches for available times:
// Phase 1: Exact preferences in 14-day batches up to 90 days
// Phase 2: Adjacent alternatives (same time/different days, same days/different times)
// Phase 3: Nothing found — suggest adjusting preferences
func FetchAvailableTimesWithFallback(
	ctx context.Context,
	adapter *BrowserAdapter,
	bookingURL string,
	serviceName string,
	prefs TimePreferences,
	onProgress func(ctx context.Context, msg string),
	patientFacingServiceName ...string, // optional: display name for progress messages (e.g. "Botox" instead of "Tox")
) (*AvailabilityResult, error) {
	// Send initial progress message using patient-facing name
	displayName := serviceName
	if len(patientFacingServiceName) > 0 && patientFacingServiceName[0] != "" {
		displayName = patientFacingServiceName[0]
	}
	if onProgress != nil {
		onProgress(ctx, fmt.Sprintf(
			"Checking available times for %s... this may take a moment.",
			displayName,
		))
	}

	// Phase 0: Try smart calendar search (single-session, scans multiple months)
	smartSlots, smartErr := fetchCalendarSlots(ctx, adapter, bookingURL, serviceName, prefs)
	if smartErr == nil && len(smartSlots) > 0 {
		for i := range smartSlots {
			smartSlots[i].Index = i + 1
		}
		return &AvailabilityResult{
			Slots:        smartSlots,
			ExactMatch:   true,
			SearchedDays: maxCalendarDays,
		}, nil
	}

	// Phase 1: Collect all qualifying dates up front, then search in batches of 31
	// (the max the sidecar accepts). Each batch reuses a single browser session,
	// so we want as many dates per batch as possible to avoid expensive re-setup.
	allQualifyingDates := collectQualifyingDates(prefs, 0, maxCalendarDays)
	searchedUpTo := maxCalendarDays

	// Search in batches of up to 31 dates (sidecar limit)
	const maxDatesPerBatch = 31
	for batchStart := 0; batchStart < len(allQualifyingDates); batchStart += maxDatesPerBatch {
		if ctx.Err() != nil {
			break
		}

		batchEnd := batchStart + maxDatesPerBatch
		if batchEnd > len(allQualifyingDates) {
			batchEnd = len(allQualifyingDates)
		}
		batchDates := allQualifyingDates[batchStart:batchEnd]

		// Send progress update before non-first batches
		if onProgress != nil && batchStart > 0 {
			onProgress(ctx, fmt.Sprintf(
				"I've checked %d dates so far — no availability matching your preferences yet. Searching further out...",
				batchStart,
			))
		}

		slots, err := fetchSlotsForDates(ctx, adapter, bookingURL, serviceName, prefs, batchDates)
		if err != nil {
			return nil, err
		}

		if len(slots) > 0 {
			return &AvailabilityResult{
				Slots:        slots,
				ExactMatch:   true,
				SearchedDays: maxCalendarDays,
			}, nil
		}
	}

	// Phase 2: Adjacent alternatives (only when patient has BOTH day AND time prefs)
	hasDayPrefs := len(prefs.DaysOfWeek) > 0
	hasTimePrefs := prefs.AfterTime != "" || prefs.BeforeTime != ""

	if ctx.Err() == nil && hasDayPrefs && hasTimePrefs {
		// Try two targeted relaxations using optimized single-batch approach
		// 2a: Same time, different days (drop DaysOfWeek, keep time filter)
		sameTimePrefs := TimePreferences{
			AfterTime:  prefs.AfterTime,
			BeforeTime: prefs.BeforeTime,
		}
		sameTimeDates := collectQualifyingDates(sameTimePrefs, 0, relaxedFallbackDays)
		sameTimeSlots, err := fetchSlotsForDates(ctx, adapter, bookingURL, serviceName, sameTimePrefs, sameTimeDates)
		if err != nil {
			return nil, err
		}

		// 2b: Same days, different times (keep DaysOfWeek, drop time filter)
		var sameDaySlots []PresentedSlot
		if ctx.Err() == nil {
			sameDayPrefs := TimePreferences{
				DaysOfWeek: prefs.DaysOfWeek,
			}
			sameDayDates := collectQualifyingDates(sameDayPrefs, 0, relaxedFallbackDays)
			sameDaySlots, err = fetchSlotsForDates(ctx, adapter, bookingURL, serviceName, sameDayPrefs, sameDayDates)
			if err != nil {
				return nil, err
			}
		}

		hasSameTime := len(sameTimeSlots) > 0
		hasSameDay := len(sameDaySlots) > 0

		if hasSameTime && hasSameDay {
			// Both alternatives exist — ask patient to choose
			return &AvailabilityResult{
				Slots:        nil,
				ExactMatch:   false,
				SearchedDays: searchedUpTo,
				Message: fmt.Sprintf(
					"I've searched the entire availability for %s over the next %s and couldn't find times matching your exact preferences. "+
						"I can help with a slight adjustment:\n"+
						"1. Same time on different days of the week\n"+
						"2. Different times on your preferred days\n\n"+
						"Just let me know which you'd prefer!",
					serviceName, humanizeDays(searchedUpTo),
				),
			}, nil
		} else if hasSameTime {
			// Only same-time-different-days has results — present directly
			return &AvailabilityResult{
				Slots:        sameTimeSlots,
				ExactMatch:   false,
				SearchedDays: searchedUpTo,
			}, nil
		} else if hasSameDay {
			// Only same-days-different-times has results — present directly
			return &AvailabilityResult{
				Slots:        sameDaySlots,
				ExactMatch:   false,
				SearchedDays: searchedUpTo,
			}, nil
		}
	} else if ctx.Err() == nil && hasDayPrefs {
		// Only day prefs, no time prefs — try relaxing day filter
		relaxedPrefs := TimePreferences{
			AfterTime:  prefs.AfterTime,
			BeforeTime: prefs.BeforeTime,
		}
		relaxedDates := collectQualifyingDates(relaxedPrefs, 0, relaxedFallbackDays)
		slots, err := fetchSlotsForDates(ctx, adapter, bookingURL, serviceName, relaxedPrefs, relaxedDates)
		if err != nil {
			return nil, err
		}
		if len(slots) > 0 {
			return &AvailabilityResult{
				Slots:        slots,
				ExactMatch:   false,
				SearchedDays: searchedUpTo,
			}, nil
		}
	}

	// Phase 3: Nothing found after exhaustive search
	if searchedUpTo == 0 {
		searchedUpTo = maxCalendarDays
	}
	return &AvailabilityResult{
		Slots:        nil,
		ExactMatch:   false,
		SearchedDays: searchedUpTo,
		Message: fmt.Sprintf(
			"I've searched the entire availability for %s over the next %s and couldn't find times matching your preferences. "+
				"Would you like to try different days or a wider time window?",
			serviceName, humanizeDays(searchedUpTo),
		),
	}, nil
}

// FetchAvailableTimesFromMoxieAPI fetches available time slots directly from
// Moxie's GraphQL API. This is much faster than browser scraping (~1s vs 30-60s).
// Falls back to browser scraper if Moxie API fails or clinic lacks MoxieConfig.
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

	result, err := moxie.GetAvailableSlots(ctx, mc.MedspaID, startDate, endDate, serviceMenuItemID, noProviderPref, providerID)
	if err != nil {
		return nil, fmt.Errorf("moxie availability query failed: %w", err)
	}

	// Convert API response to PresentedSlots, filtering by preferences
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

// collectQualifyingDates returns date strings (YYYY-MM-DD) that match the
// day-of-week preferences within the given day range from today.
func collectQualifyingDates(prefs TimePreferences, startDay, endDay int) []string {
	now := time.Now()
	var dates []string
	for i := startDay; i < endDay; i++ {
		date := now.AddDate(0, 0, i)
		if len(prefs.DaysOfWeek) > 0 {
			weekday := int(date.Weekday())
			match := false
			for _, d := range prefs.DaysOfWeek {
				if d == weekday {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		dates = append(dates, date.Format("2006-01-02"))
	}
	return dates
}

// fetchCalendarSlots tries the smart single-session calendar search.
// Returns nil, error if the client doesn't support it (triggers fallback).
func fetchCalendarSlots(
	ctx context.Context,
	adapter *BrowserAdapter,
	bookingURL string,
	serviceName string,
	prefs TimePreferences,
) ([]PresentedSlot, error) {
	if adapter == nil || !adapter.IsConfigured() {
		return nil, fmt.Errorf("browser adapter not configured")
	}

	provider, ok := adapter.client.(CalendarSlotsProvider)
	if !ok {
		return nil, fmt.Errorf("client does not support calendar slots")
	}

	req := browser.CalendarSlotsRequest{
		BookingURL:  bookingURL,
		ServiceName: serviceName,
		MaxSlots:    maxSlotsToPresent,
		MaxMonths:   3,
		Timeout:     120000,
	}
	if len(prefs.DaysOfWeek) > 0 {
		req.DaysOfWeek = prefs.DaysOfWeek
	}
	if prefs.AfterTime != "" {
		req.AfterTime = prefs.AfterTime
	}
	if prefs.BeforeTime != "" {
		req.BeforeTime = prefs.BeforeTime
	}

	resp, err := provider.GetCalendarSlots(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("calendar slots fetch failed: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("calendar slots failed: %s", resp.Error)
	}

	var allSlots []PresentedSlot
	for _, result := range resp.Results {
		dateStr := result.Date
		for _, slot := range result.Slots {
			if !slot.Available {
				continue
			}
			slotTime, err := parseTimeSlot(dateStr, slot.Time)
			if err != nil {
				continue
			}
			allSlots = append(allSlots, PresentedSlot{
				DateTime:  slotTime,
				TimeStr:   formatSlotForDisplay(slotTime),
				Service:   serviceName,
				Available: true,
			})
			if len(allSlots) >= maxSlotsToPresent {
				break
			}
		}
		if len(allSlots) >= maxSlotsToPresent {
			break
		}
	}

	return allSlots, nil
}

// fetchSlotsForDates fetches availability for a pre-built list of date strings,
// sends them in a single batch request, and filters by time preferences.
func fetchSlotsForDates(
	ctx context.Context,
	adapter *BrowserAdapter,
	bookingURL string,
	serviceName string,
	prefs TimePreferences,
	dates []string,
) ([]PresentedSlot, error) {
	if adapter == nil || !adapter.IsConfigured() {
		return nil, fmt.Errorf("browser adapter not configured")
	}
	if bookingURL == "" || len(dates) == 0 {
		return nil, nil
	}

	batchResp, err := adapter.client.GetBatchAvailability(ctx, browser.BatchAvailabilityRequest{
		BookingURL:  bookingURL,
		Dates:       dates,
		ServiceName: serviceName,
		Timeout:     perDateTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("batch availability fetch failed: %w", err)
	}

	var results []dateResult
	for _, resp := range batchResp.Results {
		if !resp.Success {
			continue
		}
		var slots []PresentedSlot
		for _, slot := range resp.Slots {
			if !slot.Available {
				continue
			}
			slotTime, err := parseTimeSlot(resp.Date, slot.Time)
			if err != nil {
				continue
			}
			if !matchesTimePreferences(slotTime, prefs) {
				continue
			}
			slots = append(slots, PresentedSlot{
				DateTime:  slotTime,
				TimeStr:   formatSlotForDisplay(slotTime),
				Service:   serviceName,
				Available: true,
			})
		}
		if len(slots) > 0 {
			results = append(results, dateResult{dateStr: resp.Date, slots: slots})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].dateStr < results[j].dateStr
	})

	var allSlots []PresentedSlot
	for _, dr := range results {
		for _, s := range dr.slots {
			allSlots = append(allSlots, s)
			if len(allSlots) >= maxSlotsToPresent*2 {
				break
			}
		}
		if len(allSlots) >= maxSlotsToPresent*2 {
			break
		}
	}

	if len(allSlots) > maxSlotsToPresent {
		allSlots = allSlots[:maxSlotsToPresent]
	}

	for i := range allSlots {
		allSlots[i].Index = i + 1
	}

	return allSlots, nil
}

// fetchSlotsForDateRange fetches availability for a range of days in parallel,
// filtering by preferences. startDay and endDay are offsets from today (0-based).
func fetchSlotsForDateRange(
	ctx context.Context,
	adapter *BrowserAdapter,
	bookingURL string,
	serviceName string,
	prefs TimePreferences,
	startDay, endDay int,
) ([]PresentedSlot, error) {
	if adapter == nil || !adapter.IsConfigured() {
		return nil, fmt.Errorf("browser adapter not configured")
	}
	if bookingURL == "" {
		return nil, fmt.Errorf("booking URL is required")
	}

	// Build list of qualifying dates
	now := time.Now()
	var qualifyingDates []string
	for i := startDay; i < endDay; i++ {
		date := now.AddDate(0, 0, i)
		dateStr := date.Format("2006-01-02")

		// Filter by day of week if specified
		if len(prefs.DaysOfWeek) > 0 {
			dayMatches := false
			weekday := int(date.Weekday())
			for _, d := range prefs.DaysOfWeek {
				if d == weekday {
					dayMatches = true
					break
				}
			}
			if !dayMatches {
				continue
			}
		}
		qualifyingDates = append(qualifyingDates, dateStr)
	}

	if len(qualifyingDates) == 0 {
		return nil, nil
	}

	// Use batch endpoint: single browser session, service selection once,
	// then calendar navigation per date (~2-3s/date instead of ~15-25s/date).
	batchResp, err := adapter.client.GetBatchAvailability(ctx, browser.BatchAvailabilityRequest{
		BookingURL:  bookingURL,
		Dates:       qualifyingDates,
		ServiceName: serviceName,
		Timeout:     perDateTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("batch availability fetch failed: %w", err)
	}

	var results []dateResult
	for _, resp := range batchResp.Results {
		if !resp.Success {
			continue
		}
		var slots []PresentedSlot
		for _, slot := range resp.Slots {
			if !slot.Available {
				continue
			}
			slotTime, err := parseTimeSlot(resp.Date, slot.Time)
			if err != nil {
				continue
			}
			if !matchesTimePreferences(slotTime, prefs) {
				continue
			}
			slots = append(slots, PresentedSlot{
				DateTime:  slotTime,
				TimeStr:   formatSlotForDisplay(slotTime),
				Service:   serviceName,
				Available: true,
			})
		}
		if len(slots) > 0 {
			results = append(results, dateResult{dateStr: resp.Date, slots: slots})
		}
	}

	// Sort results by date to maintain chronological order
	sort.Slice(results, func(i, j int) bool {
		return results[i].dateStr < results[j].dateStr
	})

	// Flatten and limit
	var allSlots []PresentedSlot
	for _, dr := range results {
		for _, s := range dr.slots {
			allSlots = append(allSlots, s)
			if len(allSlots) >= maxSlotsToPresent*2 {
				break
			}
		}
		if len(allSlots) >= maxSlotsToPresent*2 {
			break
		}
	}

	// Trim to maxSlotsToPresent
	if len(allSlots) > maxSlotsToPresent {
		allSlots = allSlots[:maxSlotsToPresent]
	}

	// Index slots 1 to N
	for i := range allSlots {
		allSlots[i].Index = i + 1
	}

	return allSlots, nil
}

// FetchAlternativeTimes fetches times without preference filtering.
// Used when exact matches are not available.
func FetchAlternativeTimes(
	ctx context.Context,
	adapter *BrowserAdapter,
	bookingURL string,
	serviceName string,
) ([]PresentedSlot, error) {
	return FetchAvailableTimes(ctx, adapter, bookingURL, serviceName, TimePreferences{})
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

// humanizeDays converts a day count to a human-readable duration string.
func humanizeDays(days int) string {
	switch {
	case days <= 7:
		return "week"
	case days <= 14:
		return "2 weeks"
	case days >= 84:
		return "3 months"
	case days >= 56:
		return "2 months"
	case days >= 28:
		return "month"
	default:
		return fmt.Sprintf("%d weeks", days/7)
	}
}

// formatSlotForDisplay formats a time slot for SMS display
// spreadSlotsAcrossDays picks slots spread across multiple days.
// maxPerDay limits slots from any single day. total caps the result.
// Slots must be pre-sorted by time.
func spreadSlotsAcrossDays(slots []PresentedSlot, total, maxPerDay int) []PresentedSlot {
	if len(slots) <= total {
		return slots
	}

	// Group by date
	type dayGroup struct {
		date  string
		slots []PresentedSlot
	}
	var days []dayGroup
	dayMap := map[string]int{} // date -> index in days
	for _, s := range slots {
		d := s.DateTime.Format("2006-01-02")
		if idx, ok := dayMap[d]; ok {
			days[idx].slots = append(days[idx].slots, s)
		} else {
			dayMap[d] = len(days)
			days = append(days, dayGroup{date: d, slots: []PresentedSlot{s}})
		}
	}

	// Round-robin: pick up to maxPerDay from each day until we have enough
	var result []PresentedSlot
	for round := 0; round < maxPerDay && len(result) < total; round++ {
		for i := range days {
			if round < len(days[i].slots) && len(result) < total {
				result = append(result, days[i].slots[round])
			}
		}
	}

	// Sort result by time
	sort.Slice(result, func(i, j int) bool {
		return result[i].DateTime.Before(result[j].DateTime)
	})

	return result
}

func formatSlotForDisplay(t time.Time) string {
	// Format: "Mon Feb 10 at 10:00 AM"
	return t.Format("Mon Jan 2 at 3:04 PM")
}

// FormatTimeSlotsForSMS formats slots as a numbered list for SMS
func FormatTimeSlotsForSMS(slots []PresentedSlot, service string, exactMatch bool) string {
	if len(slots) == 0 {
		return fmt.Sprintf("I couldn't find any available times for %s in the next week. Would you like me to check different dates or times?", service)
	}

	var sb strings.Builder

	if exactMatch {
		sb.WriteString(fmt.Sprintf("Great! I found these available times for %s:\n\n", service))
	} else {
		sb.WriteString(fmt.Sprintf("I couldn't find exact matches for your preferences, but here are the closest available times for %s:\n\n", service))
	}

	for _, slot := range slots {
		sb.WriteString(fmt.Sprintf("%d. %s\n", slot.Index, slot.TimeStr))
	}

	sb.WriteString("\nReply with the number of your preferred time.")

	return sb.String()
}

// timeSelectionPattern matches common time selection formats
var timeSelectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^(\d+)$`),                          // Just a number: "1", "2"
	regexp.MustCompile(`(?i)^(option|number|#)?\s*(\d+)$`), // "option 1", "number 2", "#3"
	regexp.MustCompile(`(?i)^the\s+(\w+)\s+one$`),          // "the first one", "the second one"
	regexp.MustCompile(`(?i)(\d{1,2}):(\d{2})\s*(am|pm)?`), // Time like "10:30am"
}

// ordinalMap converts ordinal words to numbers
var ordinalMap = map[string]int{
	"first": 1, "second": 2, "third": 3, "fourth": 4, "fifth": 5, "sixth": 6,
	"1st": 1, "2nd": 2, "3rd": 3, "4th": 4, "5th": 5, "6th": 6,
}

// isMoreTimesRequest returns true if the message is asking for more/different/later
// times rather than selecting a slot. E.g. "any later times on Mar 2 and 4th?"
func isMoreTimesRequest(message string) bool {
	morePatterns := []string{
		"more times", "more options", "other times", "other options",
		"different times", "different options", "later times", "earlier times",
		"any times", "any other", "anything else", "what else",
		"more availability", "other availability",
		"check again", "look again", "search again",
		"any later", "any earlier", "anything later", "anything earlier",
	}
	for _, pat := range morePatterns {
		if strings.Contains(message, pat) {
			return true
		}
	}
	return false
}

// buildRefinedTimePreferences adjusts time preferences based on a "more times" request.
// For example, "any later times on Mar 2 and 4th?" extracts specific dates and adjusts
// the time filter to find times not already shown.
func buildRefinedTimePreferences(message string, originalPrefs leads.SchedulingPreferences, previousSlots []PresentedSlot) TimePreferences {
	msg := strings.ToLower(message)
	base := ExtractTimePreferences(originalPrefs.PreferredDays + " " + originalPrefs.PreferredTimes)

	// Check if the patient mentioned specific dates — extract month+day references
	specificDates := extractSpecificDates(msg)
	if len(specificDates) > 0 {
		// Convert specific dates to days of week
		var days []int
		seen := map[int]bool{}
		for _, d := range specificDates {
			wd := int(d.Weekday())
			if !seen[wd] {
				days = append(days, wd)
				seen[wd] = true
			}
		}
		base.DaysOfWeek = days
	}

	// If patient says "later" or "later times", shift the after-time past the latest
	// slot already shown on the requested days
	if strings.Contains(msg, "later") {
		latestShown := findLatestShownTime(previousSlots, specificDates)
		if latestShown > 0 {
			// Set after-time to 1 minute past the latest shown slot
			newAfter := latestShown + 1
			h := newAfter / 60
			m := newAfter % 60
			base.AfterTime = fmt.Sprintf("%02d:%02d", h, m)
		}
	}

	// If patient says "earlier", shift before-time before the earliest shown
	if strings.Contains(msg, "earlier") {
		earliestShown := findEarliestShownTime(previousSlots, specificDates)
		if earliestShown > 0 {
			newBefore := earliestShown
			h := newBefore / 60
			m := newBefore % 60
			base.BeforeTime = fmt.Sprintf("%02d:%02d", h, m)
			base.AfterTime = "" // clear after-time when looking for earlier
		}
	}

	return base
}

// extractSpecificDates parses month+day references from a message like "Mar 2 and 4th"
func extractSpecificDates(msg string) []time.Time {
	months := map[string]time.Month{
		"jan": time.January, "january": time.January,
		"feb": time.February, "february": time.February,
		"mar": time.March, "march": time.March,
		"apr": time.April, "april": time.April,
		"may": time.May,
		"jun": time.June, "june": time.June,
		"jul": time.July, "july": time.July,
		"aug": time.August, "august": time.August,
		"sep": time.September, "september": time.September,
		"oct": time.October, "october": time.October,
		"nov": time.November, "november": time.November,
		"dec": time.December, "december": time.December,
	}

	now := time.Now()
	var dates []time.Time

	// Pattern: "Mar 2 and 4th" or "March 2nd and March 4th"
	// First find explicit month+day pairs
	re := regexp.MustCompile(`(?i)(jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|jun(?:e)?|jul(?:y)?|aug(?:ust)?|sep(?:tember)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)\s+(\d{1,2})`)
	matches := re.FindAllStringSubmatch(msg, -1)

	var lastMonth time.Month
	for _, m := range matches {
		monthStr := strings.ToLower(m[1])
		if mon, ok := months[monthStr]; ok {
			day, _ := strconv.Atoi(m[2])
			if day >= 1 && day <= 31 {
				year := now.Year()
				d := time.Date(year, mon, day, 0, 0, 0, 0, now.Location())
				if d.Before(now) {
					d = d.AddDate(1, 0, 0)
				}
				dates = append(dates, d)
				lastMonth = mon
			}
		}
	}

	// Look for bare numbers after "and" that refer to the same month
	// e.g., "Mar 2 and 4th" → the "4" refers to March
	if lastMonth > 0 {
		bareRe := regexp.MustCompile(`(?:and|&|,)\s+(\d{1,2})(?:st|nd|rd|th)?(?:\s|$|\?)`)
		bareMatches := bareRe.FindAllStringSubmatch(msg, -1)
		for _, bm := range bareMatches {
			day, _ := strconv.Atoi(bm[1])
			if day >= 1 && day <= 31 {
				// Check this date isn't already captured
				year := now.Year()
				d := time.Date(year, lastMonth, day, 0, 0, 0, 0, now.Location())
				if d.Before(now) {
					d = d.AddDate(1, 0, 0)
				}
				alreadyHave := false
				for _, existing := range dates {
					if existing.Equal(d) {
						alreadyHave = true
						break
					}
				}
				if !alreadyHave {
					dates = append(dates, d)
				}
			}
		}
	}

	return dates
}

// findLatestShownTime returns the latest time-of-day (in minutes since midnight)
// from previously shown slots. If specificDates is non-empty, only considers slots on those dates.
func findLatestShownTime(slots []PresentedSlot, specificDates []time.Time) int {
	latest := 0
	for _, slot := range slots {
		if len(specificDates) > 0 {
			onDate := false
			for _, d := range specificDates {
				if slot.DateTime.Year() == d.Year() && slot.DateTime.Month() == d.Month() && slot.DateTime.Day() == d.Day() {
					onDate = true
					break
				}
			}
			if !onDate {
				continue
			}
		}
		mins := slot.DateTime.Hour()*60 + slot.DateTime.Minute()
		if mins > latest {
			latest = mins
		}
	}
	return latest
}

// findEarliestShownTime returns the earliest time-of-day from previously shown slots.
func findEarliestShownTime(slots []PresentedSlot, specificDates []time.Time) int {
	earliest := 24 * 60
	for _, slot := range slots {
		if len(specificDates) > 0 {
			onDate := false
			for _, d := range specificDates {
				if slot.DateTime.Year() == d.Year() && slot.DateTime.Month() == d.Month() && slot.DateTime.Day() == d.Day() {
					onDate = true
					break
				}
			}
			if !onDate {
				continue
			}
		}
		mins := slot.DateTime.Hour()*60 + slot.DateTime.Minute()
		if mins < earliest {
			earliest = mins
		}
	}
	if earliest == 24*60 {
		return 0
	}
	return earliest
}

// filterOutPreviousSlots removes slots that were already shown to the patient.
func filterOutPreviousSlots(newSlots, previousSlots []PresentedSlot) []PresentedSlot {
	prevSet := make(map[string]bool)
	for _, s := range previousSlots {
		key := s.DateTime.Format(time.RFC3339)
		prevSet[key] = true
	}
	var filtered []PresentedSlot
	for _, s := range newSlots {
		key := s.DateTime.Format(time.RFC3339)
		if !prevSet[key] {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

// DetectTimeSelection parses user message to detect time slot selection.
// prefs is used to disambiguate bare hours (e.g., "6" when both 6am and 6pm exist).
// Returns the selected slot or nil if not a selection.
func DetectTimeSelection(message string, presentedSlots []PresentedSlot, prefs TimePreferences) *PresentedSlot {
	message = strings.TrimSpace(strings.ToLower(message))
	if message == "" || len(presentedSlots) == 0 {
		return nil
	}

	// Bail early if this looks like a request for more/different times
	if isMoreTimesRequest(message) {
		return nil
	}

	// Priority 1: Explicit "option N", "#N", "choice N" — always slot index
	optionRE := regexp.MustCompile(`(?i)^(?:option|number|#|choice)\s*(\d+)$`)
	if matches := optionRE.FindStringSubmatch(message); len(matches) > 1 {
		if num, err := strconv.Atoi(matches[1]); err == nil && num >= 1 && num <= len(presentedSlots) {
			return &presentedSlots[num-1]
		}
	}

	// Priority 2: Ordinal words ("the first one", "second", "3rd")
	// Only match ordinals that appear as standalone selection (not as part of dates like "Mar 4th")
	dateContextRE := regexp.MustCompile(`(?i)(?:jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)\w*\s+\d`)
	hasDateContext := dateContextRE.MatchString(message)
	if !hasDateContext {
		for word, num := range ordinalMap {
			if strings.Contains(message, word) && num >= 1 && num <= len(presentedSlots) {
				return &presentedSlots[num-1]
			}
		}
	}

	// Priority 3: Time with explicit am/pm/a/p — match against slot times
	// Handles: "2pm", "10:30am", "3p", "I'll take the 2pm", "I want 6pm"
	timeWithMeridiemRE := regexp.MustCompile(`(\d{1,2})(?::(\d{2}))?\s*(a\.?m\.?|p\.?m\.?|am|pm|a|p)\b`)
	if matches := timeWithMeridiemRE.FindStringSubmatch(message); len(matches) > 0 {
		hour, _ := strconv.Atoi(matches[1])
		minute := 0
		if matches[2] != "" {
			minute, _ = strconv.Atoi(matches[2])
		}
		meridiem := strings.ToLower(matches[3])
		meridiem = strings.ReplaceAll(meridiem, ".", "")
		if meridiem == "p" {
			meridiem = "pm"
		} else if meridiem == "a" {
			meridiem = "am"
		}
		if meridiem == "pm" && hour != 12 {
			hour += 12
		} else if meridiem == "am" && hour == 12 {
			hour = 0
		}

		for i := range presentedSlots {
			if presentedSlots[i].DateTime.Hour() == hour && presentedSlots[i].DateTime.Minute() == minute {
				return &presentedSlots[i]
			}
		}
		// Explicit time given but no slot matches — fall through to return nil
		return nil
	}

	// Priority 3.5: Date-based selection — "Feb 28", "Monday", "the 28th", "February 28"
	// Match against presented slot dates. If exactly one slot matches the date, return it.
	// If multiple slots on that date, pick the first (patient chose the day, we pick the time).
	dateSlotMatches := matchSlotsByDate(message, presentedSlots)
	if len(dateSlotMatches) == 1 {
		return dateSlotMatches[0]
	} else if len(dateSlotMatches) > 1 {
		// Multiple slots on the same day — use preference disambiguation, else first slot
		filtered := disambiguateByPrefs(dateSlotMatches, prefs)
		if len(filtered) == 1 {
			return filtered[0]
		}
		return dateSlotMatches[0]
	}

	// Priority 4: Extract a bare number from the message
	// Could be a slot index OR a bare hour — need to disambiguate
	bareNumRE := regexp.MustCompile(`\b(\d{1,2})\b`)
	if matches := bareNumRE.FindStringSubmatch(message); len(matches) > 1 {
		num, _ := strconv.Atoi(matches[1])
		isValidIndex := num >= 1 && num <= len(presentedSlots)

		// If it's a valid slot index, prefer index.
		// The SMS says "Reply with the number of your preferred time",
		// so small numbers (1-6) are primarily slot indices.
		// For time-based selection, patient should use am/pm (handled by Priority 3).
		if isValidIndex {
			return &presentedSlots[num-1]
		}

		// Number is out of index range — try as a bare hour match.
		// E.g., "6" with only 3 slots → look for 6:00 AM or 6:00 PM.
		var hourMatches []*PresentedSlot
		for i := range presentedSlots {
			slotHour := presentedSlots[i].DateTime.Hour()
			if slotHour == num || slotHour == num+12 || (num == 12 && slotHour == 0) {
				hourMatches = append(hourMatches, &presentedSlots[i])
			}
		}

		switch len(hourMatches) {
		case 1:
			return hourMatches[0]
		case 0:
			return nil
		default:
			// Multiple slots share this hour (e.g., 6am and 6pm)
			filtered := disambiguateByPrefs(hourMatches, prefs)
			if len(filtered) == 1 {
				return filtered[0]
			}
			return nil
		}
	}

	return nil
}

// disambiguateByPrefs filters candidate slots using the patient's time preferences.
// matchSlotsByDate matches a patient's date reference against presented slots.
// Handles: "Feb 28", "February 28", "Monday", "the 28th", "feb 28th", "2/28"
func matchSlotsByDate(message string, slots []PresentedSlot) []*PresentedSlot {
	msg := strings.ToLower(strings.TrimSpace(message))

	var matches []*PresentedSlot

	// Month name + day number: "feb 28", "february 28", "feb 28th"
	monthDayRE := regexp.MustCompile(`(?i)(jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|jun(?:e)?|jul(?:y)?|aug(?:ust)?|sep(?:tember)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)\s+(\d{1,2})(?:st|nd|rd|th)?`)
	if m := monthDayRE.FindStringSubmatch(msg); len(m) > 2 {
		monthStr := strings.ToLower(m[1])[:3]
		dayNum, _ := strconv.Atoi(m[2])
		monthMap := map[string]time.Month{
			"jan": time.January, "feb": time.February, "mar": time.March, "apr": time.April,
			"may": time.May, "jun": time.June, "jul": time.July, "aug": time.August,
			"sep": time.September, "oct": time.October, "nov": time.November, "dec": time.December,
		}
		if month, ok := monthMap[monthStr]; ok {
			for i := range slots {
				if slots[i].DateTime.Month() == month && slots[i].DateTime.Day() == dayNum {
					matches = append(matches, &slots[i])
				}
			}
			if len(matches) > 0 {
				return matches
			}
		}
	}

	// Numeric date: "2/28", "02/28"
	numDateRE := regexp.MustCompile(`(\d{1,2})/(\d{1,2})`)
	if m := numDateRE.FindStringSubmatch(msg); len(m) > 2 {
		monthNum, _ := strconv.Atoi(m[1])
		dayNum, _ := strconv.Atoi(m[2])
		if monthNum >= 1 && monthNum <= 12 {
			for i := range slots {
				if int(slots[i].DateTime.Month()) == monthNum && slots[i].DateTime.Day() == dayNum {
					matches = append(matches, &slots[i])
				}
			}
			if len(matches) > 0 {
				return matches
			}
		}
	}

	// Day of week: "monday", "tuesday", etc.
	dayOfWeekMap := map[string]time.Weekday{
		"monday": time.Monday, "tuesday": time.Tuesday, "wednesday": time.Wednesday,
		"thursday": time.Thursday, "friday": time.Friday, "saturday": time.Saturday, "sunday": time.Sunday,
		"mon": time.Monday, "tue": time.Tuesday, "tues": time.Tuesday, "wed": time.Wednesday,
		"thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday,
		"fri": time.Friday, "sat": time.Saturday, "sun": time.Sunday,
	}
	for word, dow := range dayOfWeekMap {
		if strings.Contains(msg, word) {
			for i := range slots {
				if slots[i].DateTime.Weekday() == dow {
					matches = append(matches, &slots[i])
				}
			}
			if len(matches) > 0 {
				return matches
			}
		}
	}

	// "the 28th", "the 24th" — bare day number with ordinal
	theDayRE := regexp.MustCompile(`(?:the\s+)?(\d{1,2})(?:st|nd|rd|th)`)
	if m := theDayRE.FindStringSubmatch(msg); len(m) > 1 {
		dayNum, _ := strconv.Atoi(m[1])
		if dayNum >= 1 && dayNum <= 31 {
			for i := range slots {
				if slots[i].DateTime.Day() == dayNum {
					matches = append(matches, &slots[i])
				}
			}
			if len(matches) > 0 {
				return matches
			}
		}
	}

	return nil
}

func disambiguateByPrefs(candidates []*PresentedSlot, prefs TimePreferences) []*PresentedSlot {
	if prefs.AfterTime == "" && prefs.BeforeTime == "" {
		return candidates // no preferences to filter with
	}
	var filtered []*PresentedSlot
	for _, slot := range candidates {
		if matchesTimePreferences(slot.DateTime, prefs) {
			filtered = append(filtered, slot)
		}
	}
	return filtered
}

// VerifySlotStillAvailable re-checks if a specific slot is still available
func VerifySlotStillAvailable(
	ctx context.Context,
	adapter *BrowserAdapter,
	bookingURL string,
	serviceName string,
	selectedTime time.Time,
) (bool, error) {
	if adapter == nil || !adapter.IsConfigured() {
		return false, fmt.Errorf("browser adapter not configured")
	}

	dateStr := selectedTime.Format("2006-01-02")
	resp, err := adapter.client.GetAvailability(ctx, browser.AvailabilityRequest{
		BookingURL:  bookingURL,
		Date:        dateStr,
		ServiceName: serviceName,
		Timeout:     30000,
	})
	if err != nil {
		return false, fmt.Errorf("failed to verify availability: %w", err)
	}
	if !resp.Success {
		return false, fmt.Errorf("availability check failed: %s", resp.Error)
	}

	// Check if the selected time is still in the available slots
	targetTime := selectedTime.Format("3:04 PM")
	targetTimeLower := strings.ToLower(targetTime)

	for _, slot := range resp.Slots {
		if !slot.Available {
			continue
		}
		slotTimeLower := strings.ToLower(slot.Time)
		// Match by time string (normalized)
		if slotTimeLower == targetTimeLower {
			return true, nil
		}
		// Also try parsing and comparing
		slotTime, err := parseTimeSlot(dateStr, slot.Time)
		if err == nil && slotTime.Hour() == selectedTime.Hour() && slotTime.Minute() == selectedTime.Minute() {
			return true, nil
		}
	}

	return false, nil
}

// FormatSlotNoLongerAvailableMessage formats message when selected slot was taken
func FormatSlotNoLongerAvailableMessage(selectedTime time.Time, remainingSlots []PresentedSlot) string {
	timeStr := selectedTime.Format("3:04 PM")
	if len(remainingSlots) == 0 {
		return fmt.Sprintf("I'm sorry, but the %s slot was just booked. Would you like me to check for other available times?", timeStr)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("I'm sorry, but the %s slot was just booked. Here are the remaining times:\n\n", timeStr))

	for i, slot := range remainingSlots {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, slot.TimeStr))
	}

	sb.WriteString("\nReply with the number of your preferred time.")

	return sb.String()
}

// FormatTimeSelectionConfirmation formats the confirmation message after time selection
func FormatTimeSelectionConfirmation(selectedTime time.Time, service string, depositAmount int) string {
	timeStr := selectedTime.Format("Monday, January 2 at 3:04 PM")
	depositDollars := float64(depositAmount) / 100.0

	return fmt.Sprintf(
		"Perfect! I've reserved %s for your %s appointment.\n\nTo confirm your booking, please complete the $%.0f refundable deposit:",
		timeStr, service, depositDollars,
	)
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
	prefs, ok := extractPreferences(history)
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
