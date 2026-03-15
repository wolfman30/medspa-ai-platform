package conversation

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ---------- name extraction ----------

const nameWordPattern = `[\p{L}][\p{L}\p{M}'-]*`

var namePhrasePattern = nameWordPattern + `(?:\s+` + nameWordPattern + `){0,2}`

var namePatterns = buildNamePatterns()

var nameTextNormalizer = strings.NewReplacer(
	"\u2019", "'", // right single quote
	"\u2018", "'", // left single quote
	"\u2032", "'", // prime symbol
)

func buildNamePatterns() []*regexp.Regexp {
	name := namePhrasePattern
	return []*regexp.Regexp{
		regexp.MustCompile(`(?i)my name is\s+(` + name + `)`),
		regexp.MustCompile(`(?i)i'?m\s+(` + name + `)(?:\s|,|\.|!|$)`),
		regexp.MustCompile(`(?i)i am\s+(` + name + `)(?:\s|,|\.|!|$)`),
		regexp.MustCompile(`(?i)this is\s+(` + name + `)`),
		regexp.MustCompile(`(?i)call me\s+(` + name + `)`),
		regexp.MustCompile(`(?i)it'?s\s+(` + name + `)(?:\s|,|\.|!|$)`),
		regexp.MustCompile(`(?i)name'?s\s+(` + name + `)`),
	}
}

func normalizeNameText(text string) string {
	if text == "" {
		return ""
	}
	return nameTextNormalizer.Replace(text)
}

func findNameInUserMessages(userMessages string) (fullName, firstName string) {
	normalized := normalizeNameText(userMessages)
	for _, pattern := range namePatterns {
		matches := pattern.FindAllStringSubmatch(normalized, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			full, first := fullAndFirstNameFromParts(extractNameParts(match[1]))
			if full != "" {
				return full, ""
			}
			if firstName == "" && first != "" {
				firstName = first
			}
		}
	}
	return "", firstName
}

func nameFromReplyAfterNameQuestion(history []ChatMessage) (fullName, firstName string) {
	for i, msg := range history {
		if msg.Role != ChatRoleUser {
			continue
		}
		prev := previousAssistantMessage(history, i)
		if prev == "" || !assistantAskedForName(prev) {
			continue
		}
		full, first := fullAndFirstNameFromParts(extractNameParts(msg.Content))
		if full != "" || first != "" {
			return full, first
		}
	}
	return "", ""
}

func combineSplitNameReplies(history []ChatMessage, firstName string) string {
	first := strings.TrimSpace(firstName)
	for i, msg := range history {
		if msg.Role != ChatRoleUser {
			continue
		}
		prev := previousAssistantMessage(history, i)
		if prev == "" {
			continue
		}
		if first == "" && (assistantAskedForName(prev) || assistantAskedForFirstName(prev)) {
			full, firstOnly := fullAndFirstNameFromParts(extractNameParts(msg.Content))
			if full != "" {
				return full
			}
			if firstOnly != "" {
				first = firstOnly
			}
			continue
		}
		if first != "" && assistantAskedForLastName(prev) {
			parts := extractNameParts(msg.Content)
			if len(parts) == 0 {
				continue
			}
			if len(parts) >= 2 {
				return parts[0] + " " + parts[1]
			}
			return first + " " + parts[0]
		}
	}
	return ""
}

// lastAssistantAskedForName checks if the most recent assistant message in history
// already asked for the patient's name, to avoid duplicate name requests.
func lastAssistantAskedForName(history []ChatMessage) bool {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == ChatRoleAssistant {
			return assistantAskedForName(history[i].Content)
		}
	}
	return false
}

func assistantAskedForName(message string) bool {
	message = strings.ToLower(normalizeNameText(message))
	if !strings.Contains(message, "name") {
		return false
	}
	if strings.Contains(message, "full name") || strings.Contains(message, "first and last") {
		return true
	}
	if strings.Contains(message, "first name") || strings.Contains(message, "last name") {
		return true
	}
	if strings.Contains(message, "your name") {
		return true
	}
	if strings.Contains(message, "may i") || strings.Contains(message, "what") || strings.Contains(message, "can i") || strings.Contains(message, "could i") {
		return true
	}
	return false
}

func assistantAskedForFirstName(message string) bool {
	message = strings.ToLower(normalizeNameText(message))
	return strings.Contains(message, "first name")
}

func assistantAskedForLastName(message string) bool {
	message = strings.ToLower(normalizeNameText(message))
	if strings.Contains(message, "last name") {
		return true
	}
	if strings.Contains(message, "surname") || strings.Contains(message, "family name") {
		return true
	}
	return false
}

func fullAndFirstNameFromParts(parts []string) (fullName, firstName string) {
	if len(parts) >= 2 {
		return parts[0] + " " + parts[1], parts[0]
	}
	if len(parts) == 1 {
		return "", parts[0]
	}
	return "", ""
}

func extractNameParts(raw string) []string {
	raw = normalizeNameText(raw)
	words := strings.Fields(strings.TrimSpace(raw))
	nameWords := make([]string, 0, 2)
	for _, word := range words {
		cleaned := cleanNameToken(word)
		if cleaned == "" {
			continue
		}
		if !looksLikeNameWord(cleaned) {
			if len(nameWords) > 0 {
				break
			}
			continue
		}
		nameWords = append(nameWords, capitalizeNameWord(cleaned))
		if len(nameWords) == 2 {
			break
		}
	}
	return nameWords
}

func cleanNameToken(word string) string {
	word = strings.TrimSpace(word)
	if word == "" {
		return ""
	}
	word = strings.Trim(word, ".,!?\"()[]{}")
	word = strings.Trim(word, "'-")
	return word
}

func looksLikeNameWord(word string) bool {
	count := utf8.RuneCountInString(word)
	if count < 2 || count > 30 {
		return false
	}
	firstRune, _ := utf8.DecodeRuneInString(word)
	if !unicode.IsLetter(firstRune) {
		return false
	}
	if isCommonWord(strings.ToLower(word)) {
		return false
	}
	return true
}

func capitalizeNameWord(word string) string {
	if word == "" {
		return ""
	}
	firstRune, size := utf8.DecodeRuneInString(word)
	if firstRune == utf8.RuneError || size == 0 {
		return word
	}
	return strings.ToUpper(string(firstRune)) + strings.ToLower(word[size:])
}

// isCommonWord checks if a word is a common English word that shouldn't be treated as a name
func isCommonWord(word string) bool {
	common := map[string]bool{
		"the": true, "and": true, "for": true, "are": true, "but": true,
		"not": true, "you": true, "all": true, "can": true, "her": true,
		"was": true, "one": true, "our": true, "out": true, "day": true,
		"had": true, "has": true, "his": true, "how": true, "its": true,
		"may": true, "new": true, "now": true, "old": true, "see": true,
		"way": true, "who": true, "boy": true, "did": true, "get": true,
		"let": true, "put": true, "say": true, "she": true, "too": true,
		"use": true, "yes": true, "no": true, "hi": true, "hey": true,
		"thanks": true, "thank": true, "please": true, "ok": true, "okay": true,
		"sure": true, "good": true, "great": true, "fine": true, "well": true,
		"just": true, "like": true, "want": true, "need": true, "have": true,
		"interested": true, "looking": true, "book": true, "booking": true, "appointment": true,
		"morning": true, "afternoon": true, "evening": true, "weekday": true,
		"weekend": true, "available": true, "schedule": true, "scheduling": true, "time": true,
		"botox": true, "filler": true, "facial": true, "laser": true,
		"consultation": true, "treatment": true, "service": true,
		"existing": true, "returning": true, "patient": true, "calling": true, "texting": true,
		"in": true, "on": true, "at": true, "to": true, "of": true, "is": true, "it": true,
		"an": true, "as": true, "be": true, "by": true, "do": true, "if": true, "or": true,
		"so": true, "up": true, "we": true, "me": true, "my": true, "he": true,
		"weight": true, "loss": true, "skin": true, "body": true, "face": true, "lip": true,
		"hair": true, "nail": true, "peel": true, "tox": true,
		"about": true, "with": true, "from": true, "this": true, "that": true, "what": true,
		"when": true, "your": true, "some": true, "been": true, "were": true, "them": true,
		"then": true, "than": true, "also": true, "very": true, "more": true, "much": true,
		"here": true, "there": true, "where": true, "which": true, "their": true,
		"would": true, "could": true, "should": true, "will": true,
		"inquiring": true, "writing": true, "reaching": true, "contacting": true,
		"wondering": true, "asking": true, "checking": true, "getting": true,
		"rid": true, "remove": true, "fix": true, "reduce": true, "smooth": true,
		"wrinkle": true, "wrinkles": true, "lines": true, "aging": true,
		"these": true, "those": true, "around": true, "eyes": true,
	}
	return common[strings.ToLower(word)]
}
