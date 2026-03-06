package conversation

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/leads"
)

// PresentedSlot represents a time slot that was presented to the user
type PresentedSlot struct {
	Index       int       // 1-based index shown to user
	DateTime    time.Time // Full date and time
	EndDateTime time.Time // End date and time (from Moxie slot)
	TimeStr     string    // Display string like "Mon Feb 10 at 10:00 AM"
	Service     string    // Service name
	Available   bool      // Whether it was available when presented
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

// maxCalendarDays is the Moxie calendar horizon (~3 months).
const maxCalendarDays = 90

// AvailabilityResult wraps the output of an availability fetch.
type AvailabilityResult struct {
	Slots        []PresentedSlot
	ExactMatch   bool   // true if slots match user preferences
	SearchedDays int    // how many days were searched
	Message      string // message for when no slots found at all
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

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var lastMonth time.Month
	for _, m := range matches {
		monthStr := strings.ToLower(m[1])
		if mon, ok := months[monthStr]; ok {
			day, _ := strconv.Atoi(m[2])
			if day >= 1 && day <= 31 {
				year := now.Year()
				d := time.Date(year, mon, day, 0, 0, 0, 0, now.Location())
				if d.Before(today) {
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
				if d.Before(today) {
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
