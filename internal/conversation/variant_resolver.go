package conversation

import (
	"fmt"
	"strings"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
)

// ResolveServiceVariant checks if a service has delivery variants (e.g. in-person vs virtual).
// Returns:
//   - resolvedService, "" — if no variants or variant resolved from messages
//   - "", clarifyQuestion — if variants exist but patient hasn't indicated a preference
func ResolveServiceVariant(cfg *clinic.Config, service string, messages []string) (string, string) {
	if cfg == nil {
		return service, ""
	}
	variants := cfg.GetServiceVariants(service)
	if len(variants) == 0 {
		return service, ""
	}

	// Try to resolve from message content using keyword groups
	inPersonKeywords := []string{"in person", "in-person", "come in", "office", "clinic", "walk in", "walk-in", "face to face", "on-site", "on site"}
	virtualKeywords := []string{"virtual", "telehealth", "online", "video", "zoom", "remote", "from home", "phone call", "telemedicine"}

	for _, msg := range messages {
		msg = strings.ToLower(msg)
		for _, v := range variants {
			vLower := strings.ToLower(v)
			if strings.Contains(vLower, "in person") {
				for _, kw := range inPersonKeywords {
					if strings.Contains(msg, kw) {
						return v, ""
					}
				}
			}
			if strings.Contains(vLower, "virtual") {
				for _, kw := range virtualKeywords {
					if strings.Contains(msg, kw) {
						return v, ""
					}
				}
			}
		}
	}

	// No match — build a clarification question
	names := make([]string, len(variants))
	for i, v := range variants {
		if parts := strings.SplitN(v, " - ", 2); len(parts) == 2 {
			names[i] = strings.ToLower(parts[1])
		} else {
			names[i] = strings.ToLower(v)
		}
	}
	question := fmt.Sprintf("Would you prefer an %s or %s %s consultation?",
		names[0], names[1], strings.ToLower(service))

	return "", question
}

// recentUserMessages collects the current message + recent user messages from history
// for variant resolution. Returns lowercased messages.
func recentUserMessages(history []ChatMessage, currentMsg string, lookback int) []string {
	msgs := []string{strings.ToLower(currentMsg)}
	for i := len(history) - 1; i >= 0 && i >= len(history)-lookback; i-- {
		if history[i].Role == ChatRoleUser {
			msgs = append(msgs, strings.ToLower(history[i].Content))
		}
	}
	return msgs
}
