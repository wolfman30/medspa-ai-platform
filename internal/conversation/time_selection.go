package conversation

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/browser"
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
	// Check AfterTime
	if prefs.AfterTime != "" {
		afterMinutes := parseTimeToMinutes(prefs.AfterTime)
		slotMinutes := slotTime.Hour()*60 + slotTime.Minute()
		if slotMinutes < afterMinutes {
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

// DetectTimeSelection parses user message to detect time slot selection.
// prefs is used to disambiguate bare hours (e.g., "6" when both 6am and 6pm exist).
// Returns the selected slot or nil if not a selection.
func DetectTimeSelection(message string, presentedSlots []PresentedSlot, prefs TimePreferences) *PresentedSlot {
	message = strings.TrimSpace(strings.ToLower(message))
	if message == "" || len(presentedSlots) == 0 {
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
	for word, num := range ordinalMap {
		if strings.Contains(message, word) && num >= 1 && num <= len(presentedSlots) {
			return &presentedSlots[num-1]
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
	prefs, ok := extractPreferences(history)
	if !ok {
		return false
	}

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

	// Email is collected on the Moxie booking page, not via SMS
	return true
}
