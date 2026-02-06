package conversation

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
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
}

// maxSlotsToPresent is the maximum number of slots to show at once
const maxSlotsToPresent = 6

// daysToSearch is the number of days to search for availability
const daysToSearch = 7

// perDateTimeout is the timeout in ms for each individual date fetch.
const perDateTimeout = 15000

// extendedDaysToSearch is the number of days to search in the extended fallback.
const extendedDaysToSearch = 28

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

// FetchAvailableTimesWithFallback tries multiple strategies to find available times:
// 1. Exact preferences for 7 days
// 2. Relaxed day-of-week filter (keep time filter) for 7 days
// 3. Exact preferences for days 8-28
// 4. Returns a helpful message if nothing found
func FetchAvailableTimesWithFallback(
	ctx context.Context,
	adapter *BrowserAdapter,
	bookingURL string,
	serviceName string,
	prefs TimePreferences,
) (*AvailabilityResult, error) {
	// Step 1: Exact preferences, 7 days
	slots, err := fetchSlotsForDateRange(ctx, adapter, bookingURL, serviceName, prefs, 0, daysToSearch)
	if err != nil {
		return nil, err
	}
	if len(slots) > 0 {
		return &AvailabilityResult{
			Slots:        slots,
			ExactMatch:   true,
			SearchedDays: daysToSearch,
		}, nil
	}

	// Step 2: Relax day-of-week filter (keep time filter), 7 days
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if len(prefs.DaysOfWeek) > 0 {
		relaxedPrefs := TimePreferences{
			AfterTime:  prefs.AfterTime,
			BeforeTime: prefs.BeforeTime,
			// DaysOfWeek intentionally omitted
		}
		slots, err = fetchSlotsForDateRange(ctx, adapter, bookingURL, serviceName, relaxedPrefs, 0, daysToSearch)
		if err != nil {
			return nil, err
		}
		if len(slots) > 0 {
			return &AvailabilityResult{
				Slots:        slots,
				ExactMatch:   false,
				SearchedDays: daysToSearch,
			}, nil
		}
	}

	// Step 3: Exact preferences, days 8-28
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	slots, err = fetchSlotsForDateRange(ctx, adapter, bookingURL, serviceName, prefs, daysToSearch, extendedDaysToSearch)
	if err != nil {
		return nil, err
	}
	if len(slots) > 0 {
		return &AvailabilityResult{
			Slots:        slots,
			ExactMatch:   true,
			SearchedDays: extendedDaysToSearch,
		}, nil
	}

	// Step 4: Nothing found at all
	return &AvailabilityResult{
		Slots:        nil,
		ExactMatch:   false,
		SearchedDays: extendedDaysToSearch,
		Message: fmt.Sprintf(
			"I wasn't able to find available times for %s matching your preferences in the next 4 weeks. "+
				"Would you like to try different days or times?",
			serviceName,
		),
	}, nil
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

	// Fetch all qualifying dates in parallel
	var mu sync.Mutex
	var results []dateResult
	var wg sync.WaitGroup

	for _, dateStr := range qualifyingDates {
		wg.Add(1)
		go func(d string) {
			defer wg.Done()
			resp, err := adapter.client.GetAvailability(ctx, browser.AvailabilityRequest{
				BookingURL:  bookingURL,
				Date:        d,
				ServiceName: serviceName,
				Timeout:     perDateTimeout,
			})
			if err != nil || !resp.Success {
				return
			}

			var slots []PresentedSlot
			for _, slot := range resp.Slots {
				if !slot.Available {
					continue
				}
				slotTime, err := parseTimeSlot(d, slot.Time)
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
				mu.Lock()
				results = append(results, dateResult{dateStr: d, slots: slots})
				mu.Unlock()
			}
		}(dateStr)
	}
	wg.Wait()

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
// Returns the selected slot or nil if not a selection.
func DetectTimeSelection(message string, presentedSlots []PresentedSlot) *PresentedSlot {
	message = strings.TrimSpace(strings.ToLower(message))
	if message == "" || len(presentedSlots) == 0 {
		return nil
	}

	// Try to extract a number
	var selectedIndex int

	// Pattern 1: Just a number
	if num, err := strconv.Atoi(message); err == nil && num >= 1 && num <= len(presentedSlots) {
		selectedIndex = num
	}

	// Pattern 2: "option N" or "number N" or "#N"
	if selectedIndex == 0 {
		re := regexp.MustCompile(`(?i)(?:option|number|#|choice)?\s*(\d+)`)
		if matches := re.FindStringSubmatch(message); len(matches) > 1 {
			if num, err := strconv.Atoi(matches[1]); err == nil && num >= 1 && num <= len(presentedSlots) {
				selectedIndex = num
			}
		}
	}

	// Pattern 3: Ordinal words
	if selectedIndex == 0 {
		for word, num := range ordinalMap {
			if strings.Contains(message, word) && num >= 1 && num <= len(presentedSlots) {
				selectedIndex = num
				break
			}
		}
	}

	// Pattern 4: Time match - find slot that matches mentioned time
	if selectedIndex == 0 {
		timeRE := regexp.MustCompile(`(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
		if matches := timeRE.FindStringSubmatch(message); len(matches) > 0 {
			hour, _ := strconv.Atoi(matches[1])
			minute := 0
			if matches[2] != "" {
				minute, _ = strconv.Atoi(matches[2])
			}
			meridiem := strings.ToLower(matches[3])
			if meridiem == "pm" && hour != 12 {
				hour += 12
			} else if meridiem == "am" && hour == 12 {
				hour = 0
			}

			// Find matching slot
			for _, slot := range presentedSlots {
				if slot.DateTime.Hour() == hour && slot.DateTime.Minute() == minute {
					selectedIndex = slot.Index
					break
				}
			}
		}
	}

	// Return selected slot
	if selectedIndex > 0 && selectedIndex <= len(presentedSlots) {
		return &presentedSlots[selectedIndex-1]
	}

	return nil
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
