package conversation

import (
	"regexp"
	"strings"
)

// ---------- patient type detection ----------

var (
	newPatientRE       = regexp.MustCompile(`(?i)\bnew patient\b|first time|\bi'm new\b|\bi am new\b|\bnever been\b|,\s*new\s*[,.]|\bnew here\b`)
	returningPatientRE = regexp.MustCompile(`(?i)\breturning\b|\bexisting patient\b|\bi've been\b|\bi have been\b|\bbeen there\b|\bbeen before\b|\bvisited before\b|\bcome before\b|\bcome here before\b|,\s*returning\s*[,.]|,\s*existing\s*[,.]`)
)

// detectPatientType merges all patient-type detection approaches into one function.
// Priority: 1) regex scan of all user messages, 2) short-reply context check.
func detectPatientType(userMessages string, history []ChatMessage) string {
	// 1. Regex scan across all user messages
	if newPatientRE.MatchString(userMessages) {
		return "new"
	}
	if returningPatientRE.MatchString(userMessages) {
		return "existing"
	}

	// 2. Short reply after assistant asked about patient type
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != ChatRoleUser {
			continue
		}
		reply := normalizePatientTypeReply(history[i].Content)
		if reply == "" {
			continue
		}
		if !assistantAskedPatientType(history, i) {
			continue
		}
		return reply
	}
	return ""
}

func normalizePatientTypeReply(message string) string {
	cleaned := strings.ToLower(strings.TrimSpace(message))
	cleaned = strings.Trim(cleaned, ".,!?")
	switch cleaned {
	case "new", "new patient", "new here", "first time", "first-time", "this time", "never been", "never been before",
		"i'm new", "im new", "i am new", "no", "and no", "nope", "no i haven't", "no i have not",
		"not yet", "haven't been", "i haven't", "i have not", "never", "no never",
		"it's my first time", "its my first time", "this is my first time", "this would be my first time":
		return "new"
	case "existing", "returning", "existing patient", "returning patient", "been before", "i've been before",
		"i have been before", "not new", "visited before", "i've visited before", "i have visited before",
		"come before", "i've come before", "been here before", "yes i have", "yes", "and yes",
		"yeah", "yep", "yup", "i have", "i've been there", "i have yeah":
		return "existing"
	default:
		return ""
	}
}

func assistantAskedPatientType(history []ChatMessage, userIndex int) bool {
	prev := previousAssistantMessage(history, userIndex)
	if prev == "" {
		return false
	}
	content := strings.ToLower(prev)
	if strings.Contains(content, "new patient") || strings.Contains(content, "existing patient") || strings.Contains(content, "returning patient") {
		return true
	}
	if strings.Contains(content, "visited") && strings.Contains(content, "before") {
		return true
	}
	if strings.Contains(content, "been") && strings.Contains(content, "before") {
		return true
	}
	if strings.Contains(content, "new") && (strings.Contains(content, "existing") || strings.Contains(content, "returning")) {
		return true
	}
	if strings.Contains(content, "are you new") && (strings.Contains(content, "patient") || strings.Contains(content, "here") || strings.Contains(content, "before")) {
		return true
	}
	return false
}
