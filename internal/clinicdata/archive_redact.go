package clinicdata

import (
	"regexp"
	"strings"
	"time"
)

// redactConversationID removes the phone number from the conversation ID.
// Input: "sms:org-uuid:15551234567" -> Output: "sms:org-uuid:[PHONE]"
func redactConversationID(convID string) string {
	parts := strings.Split(convID, ":")
	if len(parts) >= 3 {
		parts[2] = "[PHONE]"
		return strings.Join(parts, ":")
	}
	return convID
}

// extractNames splits a full name into individual name components for redaction.
func extractNames(fullName string) []string {
	fullName = strings.TrimSpace(fullName)
	if fullName == "" {
		return nil
	}

	var names []string
	parts := strings.Fields(fullName)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		// Only include names that are at least 2 characters
		if len(part) >= 2 {
			names = append(names, part)
		}
	}
	return names
}

// Regex patterns for detecting names in text.
var (
	// nameIntroPatterns matches common name introductions like
	// "I'm Sarah", "I am John", "My name is Jane", "This is Mike", "call me Bob".
	nameIntroPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:i'?m|i am|my name is|this is|call me|it's|its)\s+([A-Z][a-z]+(?:\s+[A-Z][a-z]+)?)`),
		regexp.MustCompile(`(?i)\b([A-Z][a-z]+(?:\s+[A-Z][a-z]+)?)\s+here\b`),
	}

	// phonePatterns matches various US phone number formats.
	phonePatterns = []*regexp.Regexp{
		regexp.MustCompile(`\+?1?[-.\s]?\(?[0-9]{3}\)?[-.\s]?[0-9]{3}[-.\s]?[0-9]{4}`),
		regexp.MustCompile(`\b[0-9]{10,11}\b`),
	}

	// emailPattern matches standard email addresses.
	emailPattern = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
)

// redactPII removes personally identifiable information from text.
// Redacts: names (known and detected), phone numbers, emails.
func redactPII(text string, knownNames []string) string {
	if text == "" {
		return text
	}

	// Redact known names (from lead record)
	for _, name := range knownNames {
		if len(name) >= 2 {
			// Case-insensitive replacement
			pattern := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(name) + `\b`)
			text = pattern.ReplaceAllString(text, "[NAME]")
		}
	}

	// Detect and redact names from common introduction patterns
	for _, pattern := range nameIntroPatterns {
		text = pattern.ReplaceAllStringFunc(text, func(match string) string {
			// Find the name part and redact it
			submatches := pattern.FindStringSubmatch(match)
			if len(submatches) >= 2 {
				name := submatches[1]
				return strings.Replace(match, name, "[NAME]", 1)
			}
			return match
		})
	}

	// Redact phone numbers
	for _, pattern := range phonePatterns {
		text = pattern.ReplaceAllString(text, "[PHONE]")
	}

	// Redact email addresses
	text = emailPattern.ReplaceAllString(text, "[EMAIL]")

	return text
}

// formatTime formats a time as RFC3339 in UTC, returning empty string for zero times.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
