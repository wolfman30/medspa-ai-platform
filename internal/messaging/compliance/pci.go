package compliance

import (
	"regexp"
	"strings"
	"unicode"
)

var panCandidateRE = regexp.MustCompile(`(?:\d[ -]?){13,19}`)

// RedactPAN detects likely payment card numbers (PAN) and returns a redacted version of the text.
// The returned string is safe to persist/log (it does not contain the full PAN).
func RedactPAN(text string) (string, bool) {
	original := text
	if strings.TrimSpace(text) == "" {
		return text, false
	}

	matches := panCandidateRE.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return text, false
	}

	var out strings.Builder
	out.Grow(len(text))
	last := 0
	redacted := false

	for _, m := range matches {
		start, end := m[0], m[1]
		if start < last {
			continue
		}

		candidate := text[start:end]
		digits := digitsOnly(candidate)
		if len(digits) < 13 || len(digits) > 19 || !luhnValid(digits) {
			continue
		}

		out.WriteString(text[last:start])
		last4 := digits
		if len(last4) > 4 {
			last4 = last4[len(last4)-4:]
		}
		out.WriteString("[REDACTED_CARD_")
		out.WriteString(last4)
		out.WriteString("]")
		last = end
		redacted = true
	}

	if !redacted {
		return original, false
	}
	out.WriteString(text[last:])
	return out.String(), true
}

func digitsOnly(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func luhnValid(digits string) bool {
	sum := 0
	alt := false
	for i := len(digits) - 1; i >= 0; i-- {
		r := rune(digits[i])
		if !unicode.IsDigit(r) {
			return false
		}
		n := int(r - '0')
		if alt {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
		alt = !alt
	}
	return sum%10 == 0
}

