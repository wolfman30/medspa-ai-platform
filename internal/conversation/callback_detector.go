package conversation

import (
	"regexp"
	"strings"
)

// callbackPatterns matches patient messages requesting a voice callback.
var callbackPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bcall\s*(me\s*)?(back|please)\b`),
	regexp.MustCompile(`(?i)\bcallback\b`),
	regexp.MustCompile(`(?i)\bprefer\s*(a\s*)?call\b`),
	regexp.MustCompile(`(?i)\brather\s*(talk|speak|call)\b`),
	regexp.MustCompile(`(?i)\bcan\s*(you|someone)\s*call\s*(me)?\b`),
	regexp.MustCompile(`(?i)\bwant\s*(a\s*)?call\b`),
	regexp.MustCompile(`(?i)\bjust\s*call\b`),
	regexp.MustCompile(`(?i)\bphone\s*call\b`),
	regexp.MustCompile(`(?i)\bspeak\s*(to|with)\s*(someone|a\s*person)\b`),
	regexp.MustCompile(`(?i)\btalk\s*(to|with)\s*(someone|a\s*person)\b`),
}

// IsCallbackRequest returns true if the message indicates the patient wants
// a voice callback instead of continuing via text.
func IsCallbackRequest(message string) bool {
	message = strings.TrimSpace(message)
	if message == "" {
		return false
	}
	for _, pat := range callbackPatterns {
		if pat.MatchString(message) {
			return true
		}
	}
	return false
}
