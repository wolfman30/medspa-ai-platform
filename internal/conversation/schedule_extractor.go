package conversation

import "strings"

// ---------- schedule detection ----------

// scheduleFromShortReply detects flexible schedule replies when the assistant
// just asked about preferred days/times.
func scheduleFromShortReply(history []ChatMessage) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != ChatRoleUser {
			continue
		}
		if !assistantAskedSchedule(history, i) {
			continue
		}
		reply := strings.ToLower(strings.TrimSpace(history[i].Content))
		reply = strings.Trim(reply, ".,!?")
		flexPatterns := []string{
			"whenever", "whenever works", "whenever is fine", "whenever works for me",
			"anything", "anything works", "anything is fine",
			"open", "i'm open", "im open", "i am open", "wide open",
			"no preference", "no pref", "doesn't matter", "dont matter",
			"don't care", "dont care", "whatever", "whatever works",
			"any", "any time", "anytime", "any day", "any of the day",
			"any time of the day", "any day of the week", "any of the week",
			"flexible", "i'm flexible", "im flexible", "pretty flexible",
			"free", "i'm free", "im free", "i am free",
			"works for me", "all good", "good with anything",
		}
		for _, pat := range flexPatterns {
			if reply == pat || strings.Contains(reply, pat) {
				return "flexible"
			}
		}
	}
	return ""
}

func assistantAskedSchedule(history []ChatMessage, userIndex int) bool {
	prev := previousAssistantMessage(history, userIndex)
	if prev == "" {
		return false
	}
	content := strings.ToLower(prev)
	scheduleIndicators := []string{
		"days and times",
		"day and time",
		"what days",
		"what times",
		"when works",
		"when would",
		"preferred time",
		"preferred day",
		"schedule",
		"availability",
		"work best for you",
		"work for you",
		"convenient for you",
	}
	for _, ind := range scheduleIndicators {
		if strings.Contains(content, ind) {
			return true
		}
	}
	return false
}
