package messaging

import "strings"

// NormalizeE164 ensures the value begins with + and only contains digits afterward.
func NormalizeE164(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	digits := sanitizePhone(value)
	if digits == "" {
		return ""
	}
	return "+" + digits
}
