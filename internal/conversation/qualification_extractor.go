package conversation

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
)

// serviceAliasesFromConfig extracts the ServiceAliases map from a clinic config.
// Returns nil if cfg is nil.
func serviceAliasesFromConfig(cfg *clinic.Config) map[string]string {
	if cfg == nil {
		return nil
	}
	return cfg.ServiceAliases
}

// ---------- package-level compiled regexes (used by extractPreferences) ----------

var (
	timeRangeRE    = regexp.MustCompile(`(?i)(\d{1,2})(?::(\d{2}))?\s*(?:am|pm|a|p)?\s*[-–—]\s*(\d{1,2})(?::(\d{2}))?\s*(a\.m\.|p\.m\.|am|pm|a|p)`)
	betweenRE      = regexp.MustCompile(`(?i)between\s+(\d{1,2})(?::(\d{2}))?\s*(?:am|pm|a|p)?\s+and\s+(\d{1,2})(?::(\d{2}))?\s*(a\.m\.|p\.m\.|am|pm|a|p)`)
	specificTimeRE = regexp.MustCompile(`(?i)(around |about |at |after |before )?(\d{1,2})(?::(\d{2}))?\s*(a\.m\.|p\.m\.|am|pm|a|p)\b`)
	noonRE         = regexp.MustCompile(`(?i)\b(noon|midday)\b`)

	// Day abbreviation matching — word-boundary patterns to avoid false positives
	dayAbbrevRE = regexp.MustCompile(`(?i)\b(?:mon(?:days?)?|tue(?:s(?:days?)?)?|wed(?:nesdays?)?|thu(?:rs(?:days?)?)?|fri(?:days?)?|sat(?:urdays?)?|sun(?:days?)?)\b`)

	dayAbbreviations = []struct {
		re   *regexp.Regexp
		full string
	}{
		{regexp.MustCompile(`(?i)\bmon(?:days?)?\b`), "monday"},
		{regexp.MustCompile(`(?i)\btue(?:s(?:days?)?)?\b`), "tuesday"},
		{regexp.MustCompile(`(?i)\bwed(?:nesdays?)?\b`), "wednesday"},
		{regexp.MustCompile(`(?i)\bthu(?:rs(?:days?)?)?\b`), "thursday"},
		{regexp.MustCompile(`(?i)\bfri(?:days?)?\b`), "friday"},
		{regexp.MustCompile(`(?i)\bsat(?:urdays?)?\b`), "saturday"},
		{regexp.MustCompile(`(?i)\bsun(?:days?)?\b`), "sunday"},
	}
)

// ---------- helpers ----------

func collectUserMessages(history []ChatMessage) (lowercase string, original string) {
	var lowerBuilder strings.Builder
	var originalBuilder strings.Builder
	for _, msg := range history {
		if msg.Role != ChatRoleUser {
			continue
		}
		lowerBuilder.WriteString(strings.ToLower(msg.Content))
		lowerBuilder.WriteString(" ")
		originalBuilder.WriteString(msg.Content)
		originalBuilder.WriteString(" ")
	}
	return lowerBuilder.String(), originalBuilder.String()
}

func previousAssistantMessage(history []ChatMessage, start int) string {
	for i := start - 1; i >= 0; i-- {
		if history[i].Role == ChatRoleSystem {
			continue
		}
		if history[i].Role != ChatRoleAssistant {
			return ""
		}
		return history[i].Content
	}
	return ""
}

// ---------- main extraction function ----------

// extractPreferences extracts scheduling preferences from conversation history.
// serviceAliases maps patient-facing terms to canonical service names (from clinic config).
// Pass nil if no config is available.
func extractPreferences(history []ChatMessage, serviceAliases map[string]string) (leads.SchedulingPreferences, bool) {
	prefs := leads.SchedulingPreferences{}
	hasPreferences := false

	userMessages, userMessagesOriginal := collectUserMessages(history)

	// --- Name extraction ---
	fullName, firstNameFallback := findNameInUserMessages(userMessagesOriginal)
	if fullName == "" {
		fullNameFromPrompt, firstFromPrompt := nameFromReplyAfterNameQuestion(history)
		if fullNameFromPrompt != "" {
			fullName = fullNameFromPrompt
		}
		if firstNameFallback == "" {
			firstNameFallback = firstFromPrompt
		}
	}
	if fullName == "" {
		fullName = combineSplitNameReplies(history, firstNameFallback)
	}
	if fullName != "" {
		prefs.Name = fullName
		hasPreferences = true
	} else if firstNameFallback != "" {
		prefs.Name = firstNameFallback
		hasPreferences = true
	}

	// --- Patient type (unified) ---
	if pt := detectPatientType(userMessages, history); pt != "" {
		prefs.PatientType = pt
		hasPreferences = true
	}

	// --- Past services ---
	if prefs.PatientType == "existing" || strings.Contains(userMessages, "before") || strings.Contains(userMessages, "previously") || strings.Contains(userMessages, "last time") {
		var pastServices []string
		for _, svc := range pastServicePatterns {
			if strings.Contains(userMessages, svc.pattern) {
				found := false
				for _, existing := range pastServices {
					if strings.EqualFold(existing, svc.name) {
						found = true
						break
					}
				}
				if !found {
					pastServices = append(pastServices, svc.name)
				}
			}
		}
		if len(pastServices) > 0 {
			prefs.PastServices = strings.Join(pastServices, ", ")
			hasPreferences = true
		}
	}

	// --- Service interest (config-driven + fallback) ---
	allMessages := userMessages
	for _, msg := range history {
		if msg.Role == ChatRoleAssistant {
			allMessages += strings.ToLower(msg.Content) + " "
		}
	}

	// First check user messages, then fall back to full conversation context
	if svc := matchService(userMessages, serviceAliases); svc != "" {
		prefs.ServiceInterest = svc
		hasPreferences = true
	} else if svc := matchService(allMessages, serviceAliases); svc != "" {
		prefs.ServiceInterest = svc
		hasPreferences = true
	}

	// --- Day preferences ---
	if strings.Contains(userMessages, "weekday") {
		prefs.PreferredDays = "weekdays"
		hasPreferences = true
	} else if strings.Contains(userMessages, "weekend") {
		prefs.PreferredDays = "weekends"
		hasPreferences = true
	} else if strings.Contains(userMessages, "any day") || strings.Contains(userMessages, "flexible") || strings.Contains(userMessages, "anytime") || strings.Contains(userMessages, "whenever") || strings.Contains(userMessages, "open schedule") {
		prefs.PreferredDays = "any"
		hasPreferences = true
	} else if dayAbbrevRE.MatchString(userMessages) {
		days := []string{}
		for _, entry := range dayAbbreviations {
			if entry.re.MatchString(userMessages) {
				// Avoid duplicates
				found := false
				for _, d := range days {
					if d == entry.full {
						found = true
						break
					}
				}
				if !found {
					days = append(days, entry.full)
				}
			}
		}
		if len(days) > 0 {
			prefs.PreferredDays = strings.Join(days, ", ")
			hasPreferences = true
		}
	}

	// --- Time preferences ---
	rangeMatched := false
	for _, re := range []*regexp.Regexp{timeRangeRE, betweenRE} {
		if m := re.FindStringSubmatch(userMessages); len(m) >= 6 {
			endAMPM := strings.ToLower(strings.ReplaceAll(m[5], ".", ""))
			if endAMPM == "a" {
				endAMPM = "am"
			} else if endAMPM == "p" {
				endAMPM = "pm"
			}
			startHour := m[1]
			startMin := m[2]
			endHour := m[3]
			endMin := m[4]
			startStr := startHour
			if startMin != "" {
				startStr += ":" + startMin
			}
			startStr += endAMPM
			endStr := endHour
			if endMin != "" {
				endStr += ":" + endMin
			}
			endStr += endAMPM
			prefs.PreferredTimes = "after " + startStr + ", before " + endStr
			hasPreferences = true
			rangeMatched = true
			break
		}
	}

	if !rangeMatched {
		if matches := specificTimeRE.FindAllStringSubmatch(userMessages, -1); len(matches) > 0 {
			times := []string{}
			for _, match := range matches {
				qualifier := strings.TrimSpace(strings.ToLower(match[1]))
				hour := match[2]
				minutes := match[3]
				ampm := strings.ToLower(strings.ReplaceAll(match[4], ".", ""))
				if ampm == "a" {
					ampm = "am"
				} else if ampm == "p" {
					ampm = "pm"
				}
				timeStr := ""
				if qualifier == "after" || qualifier == "before" {
					timeStr = qualifier + " "
				}
				timeStr += hour
				if minutes != "" {
					timeStr += ":" + minutes
				}
				timeStr += ampm
				times = append(times, timeStr)
			}
			if len(times) > 0 {
				prefs.PreferredTimes = strings.Join(times, ", ")
				hasPreferences = true
			}
		}
	}

	// Bare number fallback: "after 3", "before 5" (no am/pm — assume PM for 1-7)
	if !hasPreferences || prefs.PreferredTimes == "" {
		bareTimeRE := regexp.MustCompile(`(?i)(after|before)\s+(\d{1,2})(?:\s|,|$)`)
		if matches := bareTimeRE.FindAllStringSubmatch(userMessages, -1); len(matches) > 0 {
			times := []string{}
			for _, match := range matches {
				qualifier := strings.ToLower(match[1])
				hour, _ := strconv.Atoi(match[2])
				// Assume PM for ambiguous hours 1-7
				ampm := "pm"
				if hour >= 8 && hour <= 11 {
					ampm = "am"
				} else if hour == 12 {
					ampm = "pm"
				}
				times = append(times, fmt.Sprintf("%s %d%s", qualifier, hour, ampm))
			}
			if len(times) > 0 {
				prefs.PreferredTimes = strings.Join(times, ", ")
				hasPreferences = true
			}
		}
	}

	// General time preferences fallback
	if prefs.PreferredTimes == "" {
		if noonRE.MatchString(userMessages) {
			prefs.PreferredTimes = "noon"
			hasPreferences = true
		} else if strings.Contains(userMessages, "morning") {
			prefs.PreferredTimes = "morning"
			hasPreferences = true
		} else if strings.Contains(userMessages, "afternoon") {
			prefs.PreferredTimes = "afternoon"
			hasPreferences = true
		} else if strings.Contains(userMessages, "evening") || strings.Contains(userMessages, "after work") || strings.Contains(userMessages, "late") {
			prefs.PreferredTimes = "evening"
			hasPreferences = true
		} else if strings.Contains(userMessages, "anytime") || strings.Contains(userMessages, "any time") || strings.Contains(userMessages, "flexible") || strings.Contains(userMessages, "whenever") || strings.Contains(userMessages, "doesn't matter") || strings.Contains(userMessages, "don't care") || strings.Contains(userMessages, "works for me") || strings.Contains(userMessages, "i'm free") || strings.Contains(userMessages, "i am free") || strings.Contains(userMessages, "open schedule") {
			prefs.PreferredTimes = "flexible"
			hasPreferences = true
		}
	}

	// Short-reply fallback for schedule
	if prefs.PreferredDays == "" && prefs.PreferredTimes == "" {
		if schedPref := scheduleFromShortReply(history); schedPref != "" {
			prefs.PreferredDays = "any"
			prefs.PreferredTimes = schedPref
			hasPreferences = true
		}
	}

	// --- Provider preference ---
	noPreferencePatterns := []string{
		"no preference", "no provider preference", "don't care", "doesn't matter",
		"either is fine", "either one", "anyone", "any provider", "whoever",
		"whoever is available", "no pref", "don't have a preference",
	}
	for _, pat := range noPreferencePatterns {
		if strings.Contains(userMessages, pat) {
			prefs.ProviderPreference = "no preference"
			hasPreferences = true
			break
		}
	}
	if prefs.ProviderPreference == "" {
		prefs.ProviderPreference = providerPreferenceFromReply(history)
	}
	if prefs.ProviderPreference == "" {
		prefs.ProviderPreference = matchProviderNameInText(userMessages, history)
	}

	return prefs, hasPreferences
}
