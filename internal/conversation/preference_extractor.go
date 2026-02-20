package conversation

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// TimePreferences represents a customer's scheduling preferences extracted from natural language.
type TimePreferences struct {
	// DaysOfWeek contains the preferred days (0=Sunday, 1=Monday, ..., 6=Saturday)
	DaysOfWeek []int `json:"days_of_week,omitempty"`
	// AfterTime is the earliest acceptable time in 24-hour format (e.g., "16:00" for 4pm)
	AfterTime string `json:"after_time,omitempty"`
	// BeforeTime is the latest acceptable time in 24-hour format (e.g., "12:00" for noon)
	BeforeTime string `json:"before_time,omitempty"`
	// RawText is the original natural language input
	RawText string `json:"raw_text,omitempty"`
}

// ExtractTimePreferences parses natural language scheduling preferences.
// Examples:
//   - "Mondays or Thursdays after 4pm" → {DaysOfWeek: [1,4], AfterTime: "16:00"}
//   - "Weekdays before noon" → {DaysOfWeek: [1,2,3,4,5], BeforeTime: "12:00"}
//   - "Mornings on Tuesdays and Fridays" → {DaysOfWeek: [2,5], BeforeTime: "12:00"}
func ExtractTimePreferences(text string) TimePreferences {
	text = strings.ToLower(text)
	prefs := TimePreferences{
		RawText: text,
	}

	// Extract days of week (handles ranges like "tuesday-thursday" → Tue, Wed, Thu)
	prefs.DaysOfWeek = extractDaysOfWeek(text)

	// Check for time ranges first (e.g., "5-6pm", "5pm-6pm", "between 3 and 5pm")
	if after, before, ok := extractTimeRange(text); ok {
		prefs.AfterTime = after
		prefs.BeforeTime = before
	} else {
		// Extract individual time constraints
		prefs.AfterTime = extractAfterTime(text)
		prefs.BeforeTime = extractBeforeTime(text)
	}

	return prefs
}

// extractDaysOfWeek finds day-of-week mentions in text.
func extractDaysOfWeek(text string) []int {
	var days []int
	seen := make(map[int]bool)

	// Individual days
	dayMap := map[string]int{
		"sunday":    0,
		"sun":       0,
		"monday":    1,
		"mon":       1,
		"tuesday":   2,
		"tues":      2,
		"tue":       2,
		"wednesday": 3,
		"wed":       3,
		"thursday":  4,
		"thurs":     4,
		"thu":       4,
		"friday":    5,
		"fri":       5,
		"saturday":  6,
		"sat":       6,
	}

	// Check for day ranges first: "tuesday-thursday", "mon - fri", "tuesday through thursday"
	dayRangeRE := regexp.MustCompile(`(sun(?:day)?|mon(?:day)?|tue(?:s(?:day)?)?|wed(?:nesday)?|thu(?:rs(?:day)?)?|fri(?:day)?|sat(?:urday)?)\s*[-–—]\s*(sun(?:day)?|mon(?:day)?|tue(?:s(?:day)?)?|wed(?:nesday)?|thu(?:rs(?:day)?)?|fri(?:day)?|sat(?:urday)?)`)
	dayThroughRE := regexp.MustCompile(`(sun(?:day)?|mon(?:day)?|tue(?:s(?:day)?)?|wed(?:nesday)?|thu(?:rs(?:day)?)?|fri(?:day)?|sat(?:urday)?)\s+(?:through|thru|to)\s+(sun(?:day)?|mon(?:day)?|tue(?:s(?:day)?)?|wed(?:nesday)?|thu(?:rs(?:day)?)?|fri(?:day)?|sat(?:urday)?)`)

	for _, re := range []*regexp.Regexp{dayRangeRE, dayThroughRE} {
		if m := re.FindStringSubmatch(text); len(m) == 3 {
			startDay := dayToNumber(m[1], dayMap)
			endDay := dayToNumber(m[2], dayMap)
			if startDay >= 0 && endDay >= 0 {
				// Fill in the range (handles wrap-around, e.g., fri-mon)
				d := startDay
				for {
					if !seen[d] {
						days = append(days, d)
						seen[d] = true
					}
					if d == endDay {
						break
					}
					d = (d + 1) % 7
				}
			}
		}
	}

	// Individual days (only add if not already from a range)
	for dayName, dayNum := range dayMap {
		if strings.Contains(text, dayName) {
			if !seen[dayNum] {
				days = append(days, dayNum)
				seen[dayNum] = true
			}
		}
	}

	// Day ranges/groups
	if strings.Contains(text, "weekday") || strings.Contains(text, "weekdays") {
		for i := 1; i <= 5; i++ {
			if !seen[i] {
				days = append(days, i)
				seen[i] = true
			}
		}
	}

	if strings.Contains(text, "weekend") || strings.Contains(text, "weekends") {
		for _, i := range []int{0, 6} {
			if !seen[i] {
				days = append(days, i)
				seen[i] = true
			}
		}
	}

	if strings.Contains(text, "any day") || strings.Contains(text, "anytime") {
		for i := 0; i <= 6; i++ {
			if !seen[i] {
				days = append(days, i)
				seen[i] = true
			}
		}
	}

	// Sort days for consistent ordering
	sort.Ints(days)

	return days
}

// dayToNumber converts a day abbreviation/name to its numeric value using the dayMap.
func dayToNumber(s string, dayMap map[string]int) int {
	s = strings.ToLower(strings.TrimSpace(s))
	if num, ok := dayMap[s]; ok {
		return num
	}
	// Try prefix match for abbreviated forms
	for name, num := range dayMap {
		if strings.HasPrefix(name, s) || strings.HasPrefix(s, name) {
			return num
		}
	}
	return -1
}

// extractTimeRange detects time ranges like "5-6pm", "5pm-6pm", "between 3 and 5pm".
// Returns (afterTime, beforeTime, matched).
func extractTimeRange(text string) (string, string, bool) {
	// Pattern: "5-6pm", "5-6p", "5pm-6pm", "3:00pm-4:30pm"
	rangeRE := regexp.MustCompile(`(\d{1,2})(?::(\d{2}))?\s*(am|pm|a|p)?\s*[-–—]\s*(\d{1,2})(?::(\d{2}))?\s*(am|pm|a|p)`)
	if m := rangeRE.FindStringSubmatch(text); len(m) >= 7 {
		endAMPM := normalizeAMPM(m[6])
		startAMPM := normalizeAMPM(m[3])
		if startAMPM == "" {
			startAMPM = endAMPM // "5-6pm" → both are PM
		}
		startTime := to24Hour(m[1], m[2], startAMPM)
		endTime := to24Hour(m[4], m[5], endAMPM)
		return startTime, endTime, true
	}

	// Pattern: "between 3 and 5pm", "between 3pm and 5pm"
	betweenRE := regexp.MustCompile(`between\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm|a|p)?\s+and\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm|a|p)`)
	if m := betweenRE.FindStringSubmatch(text); len(m) >= 7 {
		endAMPM := normalizeAMPM(m[6])
		startAMPM := normalizeAMPM(m[3])
		if startAMPM == "" {
			startAMPM = endAMPM
		}
		startTime := to24Hour(m[1], m[2], startAMPM)
		endTime := to24Hour(m[4], m[5], endAMPM)
		return startTime, endTime, true
	}

	return "", "", false
}

func normalizeAMPM(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "a":
		return "am"
	case "p":
		return "pm"
	default:
		return s
	}
}

func to24Hour(hourStr, minStr, ampm string) string {
	h := 0
	for _, c := range hourStr {
		h = h*10 + int(c-'0')
	}
	m := 0
	if minStr != "" {
		for _, c := range minStr {
			m = m*10 + int(c-'0')
		}
	}
	if ampm == "pm" && h != 12 {
		h += 12
	} else if ampm == "am" && h == 12 {
		h = 0
	}
	return fmt.Sprintf("%02d:%02d", h, m)
}

// extractAfterTime finds "after X" time constraints.
func extractAfterTime(text string) string {
	// Patterns: "after 4pm", "after 4:00pm", "after 4", "4pm or later", etc.
	patterns := []string{
		`after\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`,
		`(\d{1,2})(?::(\d{2}))?\s*(am|pm)\s+or\s+later`,
		`(\d{1,2})(?::(\d{2}))?\s*(am|pm)\s+onwards?`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(text); len(matches) > 0 {
			return parse24HourTime(matches)
		}
	}

	// Fallback: bare time like "3pm" without "after"/"before" — treat as "after" since
	// patients saying "3pm" typically mean "3pm or later", not "exactly at 3pm only"
	bareTimeRE := regexp.MustCompile(`(?:^|\s)(\d{1,2})(?::(\d{2}))?\s*(am|pm)`)
	if matches := bareTimeRE.FindStringSubmatch(text); len(matches) > 0 {
		return parse24HourTime(matches)
	}

	// Common time-of-day phrases
	if strings.Contains(text, "afternoon") || strings.Contains(text, "afternoons") {
		return "12:00" // After noon
	}
	if strings.Contains(text, "evening") || strings.Contains(text, "evenings") {
		return "17:00" // After 5pm
	}
	if strings.Contains(text, "after work") || strings.Contains(text, "after-work") {
		return "17:00" // After 5pm
	}
	if strings.Contains(text, "late") {
		return "17:00" // Late typically means after 5pm
	}

	return ""
}

// extractBeforeTime finds "before X" time constraints.
func extractBeforeTime(text string) string {
	// Patterns: "before 5pm", "before noon", etc.
	patterns := []string{
		`before\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`,
		`by\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(text); len(matches) > 0 {
			return parse24HourTime(matches)
		}
	}

	// Common time-of-day phrases
	if strings.Contains(text, "morning") || strings.Contains(text, "mornings") {
		return "12:00" // Before noon
	}
	if strings.Contains(text, "before noon") || strings.Contains(text, "before lunch") {
		return "12:00"
	}
	if strings.Contains(text, "early") {
		return "12:00" // Early typically means morning
	}

	return ""
}

// parse24HourTime converts regex matches to 24-hour format.
// matches[1] = hour, matches[2] = minute (optional), matches[3] = am/pm (optional)
func parse24HourTime(matches []string) string {
	if len(matches) < 2 {
		return ""
	}

	hour := 0
	minute := 0
	meridiem := ""

	// Parse hour (matches[1])
	if matches[1] != "" {
		var err error
		_, err = time.Parse("15", matches[1])
		if err == nil {
			// Parse succeeded
			h := 0
			if _, err := time.ParseDuration(matches[1] + "h"); err == nil {
				// Simple hour parsing
				for i := 0; i < len(matches[1]); i++ {
					h = h*10 + int(matches[1][i]-'0')
				}
				hour = h
			}
		}
	}

	// Parse minute (matches[2]) if present
	if len(matches) > 2 && matches[2] != "" {
		m := 0
		for i := 0; i < len(matches[2]); i++ {
			m = m*10 + int(matches[2][i]-'0')
		}
		minute = m
	}

	// Parse meridiem (matches[3]) if present
	if len(matches) > 3 && matches[3] != "" {
		meridiem = strings.ToLower(matches[3])
	}

	// Convert to 24-hour format
	if meridiem == "pm" && hour != 12 {
		hour += 12
	} else if meridiem == "am" && hour == 12 {
		hour = 0
	}

	// Default to PM if hour is ambiguous and between 1-7 (common afternoon/evening range)
	if meridiem == "" && hour >= 1 && hour <= 7 {
		hour += 12
	}

	return strings.TrimSpace(formatTime24(hour, minute))
}

// formatTime24 formats hour and minute as HH:MM in 24-hour format.
func formatTime24(hour, minute int) string {
	if hour < 0 || hour > 23 {
		return ""
	}
	if minute < 0 || minute > 59 {
		minute = 0
	}
	return padZero(hour) + ":" + padZero(minute)
}

// padZero pads single-digit numbers with a leading zero.
func padZero(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

// FormatPreferencesForLLM converts preferences to human-readable text for the LLM.
func FormatPreferencesForLLM(prefs TimePreferences) string {
	if prefs.RawText == "" && len(prefs.DaysOfWeek) == 0 && prefs.AfterTime == "" && prefs.BeforeTime == "" {
		return "any day/time"
	}

	var parts []string

	if len(prefs.DaysOfWeek) > 0 {
		dayNames := make([]string, 0, len(prefs.DaysOfWeek))
		dayMap := []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
		for _, d := range prefs.DaysOfWeek {
			if d >= 0 && d <= 6 {
				dayNames = append(dayNames, dayMap[d])
			}
		}
		if len(dayNames) > 0 {
			parts = append(parts, strings.Join(dayNames, ", "))
		}
	}

	if prefs.AfterTime != "" {
		parts = append(parts, "after "+formatDisplayTime(prefs.AfterTime))
	}

	if prefs.BeforeTime != "" {
		parts = append(parts, "before "+formatDisplayTime(prefs.BeforeTime))
	}

	if len(parts) == 0 && prefs.RawText != "" {
		return prefs.RawText
	}

	return strings.Join(parts, " ")
}

// formatDisplayTime converts 24-hour time to 12-hour display format.
func formatDisplayTime(time24 string) string {
	parts := strings.Split(time24, ":")
	if len(parts) != 2 {
		return time24
	}

	hour := 0
	for i := 0; i < len(parts[0]); i++ {
		hour = hour*10 + int(parts[0][i]-'0')
	}

	minute := parts[1]

	if hour == 0 {
		return "12:" + minute + "am"
	} else if hour < 12 {
		return padZero(hour) + ":" + minute + "am"
	} else if hour == 12 {
		return "12:" + minute + "pm"
	} else {
		return padZero(hour-12) + ":" + minute + "pm"
	}
}

// emailPattern matches common email address formats
var emailPattern = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)

// ExtractEmail extracts an email address from text.
// Returns the first valid email found, or empty string if none found.
func ExtractEmail(text string) string {
	match := emailPattern.FindString(text)
	if match != "" {
		return strings.ToLower(match)
	}
	return ""
}

// ExtractEmailFromHistory scans conversation history for an email address.
// Returns the first valid email found in user messages.
func ExtractEmailFromHistory(history []ChatMessage) string {
	for _, msg := range history {
		if msg.Role == ChatRoleUser {
			if email := ExtractEmail(msg.Content); email != "" {
				return email
			}
		}
	}
	return ""
}
