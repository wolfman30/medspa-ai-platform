package archive

import (
	"crypto/sha256"
	"fmt"
	"regexp"
)

var (
	emailRe = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	phoneRe = regexp.MustCompile(`\+?1?[-.\s]?\(?[0-9]{3}\)?[-.\s]?[0-9]{3}[-.\s]?[0-9]{4}`)
)

// HashPhone returns the hex-encoded SHA-256 hash of a phone number.
func HashPhone(phone string) string {
	h := sha256.Sum256([]byte(phone))
	return fmt.Sprintf("%x", h)
}

// ScrubPII replaces emails with [EMAIL] and phone numbers with [PHONE].
// Names are kept for training context.
func ScrubPII(text string) string {
	text = emailRe.ReplaceAllString(text, "[EMAIL]")
	text = phoneRe.ReplaceAllString(text, "[PHONE]")
	return text
}

// ScrubMessages applies PII scrubbing to all messages in-place.
func ScrubMessages(msgs []Message) {
	for i := range msgs {
		msgs[i].Content = ScrubPII(msgs[i].Content)
	}
}
