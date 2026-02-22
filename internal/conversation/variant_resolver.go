package conversation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// VariantResolver resolves service delivery variants (e.g. in-person vs virtual)
// using an LLM classifier for natural language understanding.
type VariantResolver struct {
	llm    LLMClient
	model  string
	logger *logging.Logger
}

// NewVariantResolver creates a resolver that uses the given LLM for classification.
// If llm is nil, falls back to keyword matching.
func NewVariantResolver(llm LLMClient, model string, logger *logging.Logger) *VariantResolver {
	return &VariantResolver{llm: llm, model: model, logger: logger}
}

// Resolve checks if a service has delivery variants and tries to determine
// which variant the patient wants from their messages.
//
// Returns:
//   - resolvedService, "" — if no variants configured or variant resolved
//   - "", clarifyQuestion — if variants exist but patient hasn't indicated preference
func (vr *VariantResolver) Resolve(ctx context.Context, cfg *clinic.Config, service string, messages []string) (string, string) {
	if cfg == nil {
		return service, ""
	}
	variants := cfg.GetServiceVariants(service)
	if len(variants) == 0 {
		return service, ""
	}

	// Try LLM classification first, fall back to keywords
	if vr.llm != nil {
		resolved, err := vr.classifyWithLLM(ctx, service, variants, messages)
		if err != nil {
			if vr.logger != nil {
				vr.logger.Warn("variant LLM classification failed, falling back to keywords",
					"service", service, "error", err)
			}
		} else if resolved != "" {
			return resolved, ""
		} else {
			// LLM said "unclear" — ask the patient
			return "", buildClarificationQuestion(service, variants)
		}
	}

	// Fallback: keyword matching
	resolved := keywordMatch(variants, messages)
	if resolved != "" {
		return resolved, ""
	}

	return "", buildClarificationQuestion(service, variants)
}

// classifyWithLLM asks the LLM to determine which variant the patient chose.
// Returns the matched variant name, or "" if unclear.
func (vr *VariantResolver) classifyWithLLM(ctx context.Context, service string, variants []string, messages []string) (string, error) {
	// Build option labels
	labels := make([]string, len(variants))
	for i, v := range variants {
		labels[i] = fmt.Sprintf("%d. %s", i+1, v)
	}

	// Combine recent messages into context
	patientText := strings.Join(messages, "\n")

	prompt := fmt.Sprintf(`A patient is booking a "%s" appointment. This service has delivery options:

%s

The patient said:
"""%s"""

Which option did the patient choose? Reply with ONLY the exact option text (e.g. "%s") or the word "unclear" if they haven't indicated a preference.
Do not explain. Do not add punctuation.`, service, strings.Join(labels, "\n"), patientText, variants[0])

	classifyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := vr.llm.Complete(classifyCtx, LLMRequest{
		Model:       vr.model,
		Messages:    []ChatMessage{{Role: ChatRoleUser, Content: prompt}},
		MaxTokens:   50,
		Temperature: 0,
	})
	if err != nil {
		return "", fmt.Errorf("LLM classify: %w", err)
	}

	answer := strings.TrimSpace(resp.Text)

	// Check if the answer matches one of the variants
	for _, v := range variants {
		if strings.EqualFold(answer, v) {
			return v, nil
		}
	}

	// Fuzzy: check if the answer contains a key part of a variant name
	answerLower := strings.ToLower(answer)
	for _, v := range variants {
		vLower := strings.ToLower(v)
		// Check for the distinguishing part after " - "
		if parts := strings.SplitN(vLower, " - ", 2); len(parts) == 2 {
			if strings.Contains(answerLower, parts[1]) {
				return v, nil
			}
		}
		if strings.Contains(answerLower, vLower) {
			return v, nil
		}
	}

	// "unclear" or unrecognized
	if strings.Contains(answerLower, "unclear") {
		return "", nil
	}

	// LLM gave something unexpected — treat as unclear
	if vr.logger != nil {
		vr.logger.Warn("variant classifier returned unexpected answer",
			"answer", answer, "variants", variants)
	}
	return "", nil
}

// keywordMatch is the fallback when no LLM is available.
func keywordMatch(variants []string, messages []string) string {
	inPersonKeywords := []string{"in person", "in-person", "come in", "office", "clinic", "walk in", "walk-in", "face to face", "on-site", "on site"}
	virtualKeywords := []string{"virtual", "telehealth", "online", "video", "zoom", "remote", "from home", "phone call", "telemedicine"}

	for _, msg := range messages {
		msg = strings.ToLower(msg)
		for _, v := range variants {
			vLower := strings.ToLower(v)
			if strings.Contains(vLower, "in person") {
				for _, kw := range inPersonKeywords {
					if strings.Contains(msg, kw) {
						return v
					}
				}
			}
			if strings.Contains(vLower, "virtual") {
				for _, kw := range virtualKeywords {
					if strings.Contains(msg, kw) {
						return v
					}
				}
			}
		}
	}
	return ""
}

func buildClarificationQuestion(service string, variants []string) string {
	names := make([]string, len(variants))
	for i, v := range variants {
		if parts := strings.SplitN(v, " - ", 2); len(parts) == 2 {
			names[i] = parts[1]
		} else {
			names[i] = v
		}
	}
	if len(names) == 2 {
		return fmt.Sprintf("We offer %s and %s. Which are you interested in?", names[0], names[1])
	}
	// 3+ variants: comma-separated list with "or" before last
	last := names[len(names)-1]
	rest := strings.Join(names[:len(names)-1], ", ")
	return fmt.Sprintf("We offer %s, or %s. Which are you interested in?", rest, last)
}

// ResolveServiceVariant is the stateless convenience function (keyword-only, no LLM).
// Used in tests and as a fallback.
func ResolveServiceVariant(cfg *clinic.Config, service string, messages []string) (string, string) {
	if cfg == nil {
		return service, ""
	}
	variants := cfg.GetServiceVariants(service)
	if len(variants) == 0 {
		return service, ""
	}
	resolved := keywordMatch(variants, messages)
	if resolved != "" {
		return resolved, ""
	}
	return "", buildClarificationQuestion(service, variants)
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
