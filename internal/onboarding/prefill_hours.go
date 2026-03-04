package onboarding

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
)

func parseBusinessHours(text string) clinic.BusinessHours {
	var hours clinic.BusinessHours
	hoursText := extractBetween(text, "Hours:", []string{"Connect", "Contact", "Book"})
	if hoursText == "" {
		return hours
	}

	dayPattern := `(?i)(?:mon(?:day)?|tue(?:sday)?|tues|wed(?:nesday)?|thu(?:rsday)?|thur|thurs|fri(?:day)?|sat(?:urday)?|sun(?:day)?)(?:\s*(?:&|and|,|-)\s*(?:mon(?:day)?|tue(?:sday)?|tues|wed(?:nesday)?|thu(?:rsday)?|thur|thurs|fri(?:day)?|sat(?:urday)?|sun(?:day)?))*`
	re := regexp.MustCompile(dayPattern)
	matches := re.FindAllStringIndex(hoursText, -1)
	for i, match := range matches {
		if len(match) < 2 {
			continue
		}
		dayGroup := strings.TrimSpace(hoursText[match[0]:match[1]])
		start := match[1]
		end := len(hoursText)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		timePart := strings.TrimSpace(hoursText[start:end])
		timePart = strings.TrimSpace(strings.TrimLeft(timePart, ":-"))
		days := extractDays(dayGroup)
		if len(days) == 0 {
			continue
		}
		if strings.Contains(strings.ToLower(timePart), "closed") {
			for _, day := range days {
				setDayHours(&hours, day, nil)
			}
			continue
		}
		open, close := parseTimeRange(timePart)
		if open == "" || close == "" {
			continue
		}
		for _, day := range days {
			setDayHours(&hours, day, &clinic.DayHours{Open: open, Close: close})
		}
	}
	return hours
}

func extractDays(text string) []string {
	re := regexp.MustCompile(`(?i)\b(mon(?:day)?|tue(?:sday)?|tues|wed(?:nesday)?|thu(?:rsday)?|thur|thurs|fri(?:day)?|sat(?:urday)?|sun(?:day)?)\b`)
	matches := re.FindAllStringSubmatch(text, -1)
	result := []string{}
	seen := map[string]bool{}
	for _, match := range matches {
		normalized := normalizeDay(match[1])
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		result = append(result, normalized)
	}
	return result
}

func normalizeDay(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch raw {
	case "mon", "monday":
		return "monday"
	case "tue", "tues", "tuesday":
		return "tuesday"
	case "wed", "wednesday":
		return "wednesday"
	case "thu", "thur", "thurs", "thursday":
		return "thursday"
	case "fri", "friday":
		return "friday"
	case "sat", "saturday":
		return "saturday"
	case "sun", "sunday":
		return "sunday"
	default:
		return ""
	}
}

func parseTimeRange(raw string) (string, string) {
	re := regexp.MustCompile(`(?i)(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\s*-\s*(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
	match := re.FindStringSubmatch(raw)
	if len(match) < 7 {
		return "", ""
	}
	startMeridiem := strings.ToLower(match[3])
	endMeridiem := strings.ToLower(match[6])
	if endMeridiem == "" {
		endMeridiem = startMeridiem
	}
	open := formatTime(match[1], match[2], startMeridiem)
	close := formatTime(match[4], match[5], endMeridiem)
	return open, close
}

func formatTime(hourRaw, minRaw, meridiem string) string {
	hour := atoi(hourRaw)
	min := atoi(minRaw)
	if meridiem == "" {
		meridiem = "am"
	}
	if meridiem == "pm" && hour < 12 {
		hour += 12
	}
	if meridiem == "am" && hour == 12 {
		hour = 0
	}
	return fmt.Sprintf("%02d:%02d", hour, min)
}

func atoi(raw string) int {
	if raw == "" {
		return 0
	}
	n := 0
	for _, r := range raw {
		if r < '0' || r > '9' {
			continue
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func setDayHours(hours *clinic.BusinessHours, day string, value *clinic.DayHours) {
	switch day {
	case "monday":
		hours.Monday = value
	case "tuesday":
		hours.Tuesday = value
	case "wednesday":
		hours.Wednesday = value
	case "thursday":
		hours.Thursday = value
	case "friday":
		hours.Friday = value
	case "saturday":
		hours.Saturday = value
	case "sunday":
		hours.Sunday = value
	}
}

func timezoneForState(state string) string {
	state = strings.ToUpper(strings.TrimSpace(state))
	if state == "" {
		return ""
	}
	switch state {
	case "CT", "DE", "FL", "GA", "IN", "KY", "ME", "MD", "MA", "MI", "NH", "NJ", "NY", "NC", "OH", "PA", "RI", "SC", "TN", "VT", "VA", "WV", "DC":
		return "America/New_York"
	case "AL", "AR", "IL", "IA", "LA", "MN", "MS", "MO", "OK", "WI", "TX", "KS", "NE", "ND", "SD":
		return "America/Chicago"
	case "AZ", "CO", "ID", "MT", "NM", "UT", "WY":
		return "America/Denver"
	case "CA", "NV", "OR", "WA":
		return "America/Los_Angeles"
	case "AK":
		return "America/Anchorage"
	case "HI":
		return "Pacific/Honolulu"
	default:
		return ""
	}
}
