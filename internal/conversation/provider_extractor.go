package conversation

import "strings"

// ---------- provider preference ----------

// matchProviderNameInText scans user messages for known provider names from the
// conversation's system prompt (which lists available providers).
func matchProviderNameInText(userMessages string, history []ChatMessage) string {
	var providerNames []string
	for _, msg := range history {
		if msg.Role == ChatRoleSystem || msg.Role == ChatRoleAssistant {
			lower := strings.ToLower(msg.Content)
			if strings.Contains(lower, "provider") || strings.Contains(lower, "brandi") || strings.Contains(lower, "gale") {
				for _, pattern := range []string{"we have ", "available providers: ", "providers: "} {
					idx := strings.Index(lower, pattern)
					if idx >= 0 {
						segment := msg.Content[idx+len(pattern):]
						segment = strings.ReplaceAll(segment, " and ", ", ")
						if dashIdx := strings.Index(segment, " - "); dashIdx >= 0 {
							segment = segment[:dashIdx]
						}
						if dotIdx := strings.IndexAny(segment, ".!?\n"); dotIdx >= 0 {
							segment = segment[:dotIdx]
						}
						parts := strings.Split(segment, ", ")
						for _, p := range parts {
							name := strings.TrimSpace(p)
							if name != "" && looksLikePersonName(name) {
								providerNames = append(providerNames, name)
							}
						}
					}
				}
			}
		}
	}

	for _, fullName := range providerNames {
		firstName := strings.ToLower(strings.Fields(fullName)[0])
		if len(firstName) > 2 && strings.Contains(userMessages, firstName) {
			return fullName
		}
	}
	return ""
}

// looksLikePersonName filters out extracted "provider names" that are actually
// service names or other non-person text.
func looksLikePersonName(name string) bool {
	words := strings.Fields(name)
	if len(words) < 2 || len(words) > 4 {
		return false
	}
	serviceWords := map[string]bool{
		"lip": true, "filler": true, "fillers": true, "augmentation": true,
		"injection": true, "botox": true, "peel": true, "laser": true,
		"hair": true, "removal": true, "weight": true, "loss": true,
		"skin": true, "facial": true, "microneedling": true, "tixel": true,
		"chemical": true, "dermal": true, "consultation": true, "treatment": true,
		"prp": true, "b12": true, "vitamin": true, "iv": true, "therapy": true,
		"enhancement": true, "rejuvenation": true, "sculpting": true,
	}
	for _, w := range words {
		if serviceWords[strings.ToLower(w)] {
			return false
		}
	}
	return true
}

// providerPreferenceFromReply checks if the assistant asked about provider preference
// and the user replied with a name or "no preference".
func providerPreferenceFromReply(history []ChatMessage) string {
	providerQuestionPatterns := []string{
		"provider preference", "preferred provider", "specific provider",
		"particular provider", "who would you like", "do you have a preference for a provider",
	}
	for i := len(history) - 1; i >= 1; i-- {
		msg := history[i]
		if msg.Role != ChatRoleUser {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			if history[j].Role == ChatRoleAssistant {
				assistantMsg := strings.ToLower(history[j].Content)
				askedAboutProvider := false
				for _, pat := range providerQuestionPatterns {
					if strings.Contains(assistantMsg, pat) {
						askedAboutProvider = true
						break
					}
				}
				if askedAboutProvider {
					reply := strings.ToLower(strings.TrimSpace(msg.Content))
					if reply == "no" || reply == "nope" || strings.Contains(reply, "no preference") ||
						strings.Contains(reply, "doesn't matter") || strings.Contains(reply, "don't care") ||
						strings.Contains(reply, "either") || strings.Contains(reply, "anyone") ||
						strings.Contains(reply, "whoever") {
						return "no preference"
					}
					if len(reply) > 1 && len(reply) < 50 {
						return msg.Content
					}
				}
				break
			}
		}
	}
	return ""
}
