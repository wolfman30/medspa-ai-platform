package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

var (
	depositAffirmativeRE = regexp.MustCompile(`(?i)(?:\b(?:yes|yeah|yea|sure|ok|okay|absolutely|definitely|proceed)\b|let'?s do it|i'?ll pay|i will pay)`)
	depositNegativeRE    = regexp.MustCompile(`(?i)(?:no deposit|don'?t want|do not want|not paying|not now|maybe(?: later)?|later|skip|no thanks|nope)`)
	depositKeywordRE     = regexp.MustCompile(`(?i)(?:\b(?:deposit|payment)\b|\bpay\b|secure (?:my|your) spot|hold (?:my|your) spot)`)
	depositAskRE         = regexp.MustCompile(`(?i)(?:\bdeposit\b|refundable deposit|payment link|secure (?:my|your) spot|hold (?:my|your) spot|pay a deposit)`)

	// sanitizeSMSResponse regexes
	smsItalicRE     = regexp.MustCompile(`\*([^\s*][^*]*[^\s*])\*`)
	smsBulletRE     = regexp.MustCompile(`(?m)^[\s]*[-•]\s+`)
	smsNumberedRE   = regexp.MustCompile(`(?m)^[\s]*\d+\.\s+`)
	smsMultiSpaceRE = regexp.MustCompile(`\s{2,}`)
)

func shouldAttemptDepositClassification(history []ChatMessage) bool {
	checked := 0
	for i := len(history) - 1; i >= 0 && checked < 8; i-- {
		if history[i].Role == ChatRoleSystem {
			continue
		}
		msg := strings.TrimSpace(history[i].Content)
		if msg == "" {
			continue
		}
		if depositKeywordRE.MatchString(msg) || depositAskRE.MatchString(msg) {
			return true
		}
		checked++
	}
	return false
}

func (s *LLMService) extractDepositIntent(ctx context.Context, history []ChatMessage) (*DepositIntent, error) {
	ctx, span := llmTracer.Start(ctx, "conversation.deposit_intent")
	defer span.End()

	outcome := "skip"
	var raw string
	defer func() {
		depositDecisionTotal.WithLabelValues(s.model, outcome).Inc()
	}()

	// Focus on the most recent turns to keep the prompt small.
	transcript := summarizeHistory(history, 8)
	systemPrompt := fmt.Sprintf(`You are a decision agent for MedSpa AI. Analyze a conversation and decide if we should send a payment link to collect a deposit.

CRITICAL: Return ONLY a JSON object, nothing else. No markdown, no code fences, no explanation.

Return this exact format:
{"collect": true, "amount_cents": 5000, "description": "Refundable deposit", "success_url": "", "cancel_url": ""}

Rules:
- ONLY set collect=true if the customer EXPLICITLY agreed to the deposit with words like "yes", "sure", "ok", "proceed", "let's do it", "I'll pay", etc.
- Set collect=false if:
  - Customer hasn't been asked about the deposit yet
  - Customer was just offered the deposit but hasn't responded yet
  - Customer declined or said "no", "not now", "maybe later", etc.
  - The assistant just asked "Would you like to proceed?" - WAIT for their response
- Default amount: %d cents
- For success_url and cancel_url: use empty strings
`, s.deposit.DefaultAmountCents)

	callCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := s.client.Complete(callCtx, LLMRequest{
		Model:  s.model,
		System: []string{systemPrompt},
		Messages: []ChatMessage{
			{Role: ChatRoleUser, Content: "Conversation:\n" + transcript},
		},
		MaxTokens:   256,
		Temperature: 0,
	})
	latency := time.Since(start)
	status := "ok"
	if err != nil {
		status = "error"
	}
	llmLatency.WithLabelValues(s.model, status).Observe(latency.Seconds())
	if resp.Usage.InputTokens > 0 {
		llmTokensTotal.WithLabelValues(s.model, "input").Add(float64(resp.Usage.InputTokens))
	}
	if resp.Usage.OutputTokens > 0 {
		llmTokensTotal.WithLabelValues(s.model, "output").Add(float64(resp.Usage.OutputTokens))
	}
	if resp.Usage.TotalTokens > 0 {
		llmTokensTotal.WithLabelValues(s.model, "total").Add(float64(resp.Usage.TotalTokens))
	}
	if span.IsRecording() {
		span.SetAttributes(
			attribute.String("medspa.llm.purpose", "deposit_classifier"),
			attribute.Float64("medspa.llm.latency_ms", float64(latency.Milliseconds())),
			attribute.Int("medspa.llm.input_tokens", int(resp.Usage.InputTokens)),
			attribute.Int("medspa.llm.output_tokens", int(resp.Usage.OutputTokens)),
			attribute.Int("medspa.llm.total_tokens", int(resp.Usage.TotalTokens)),
			attribute.String("medspa.llm.stop_reason", resp.StopReason),
		)
	}
	if err != nil {
		outcome = "error"
		s.maybeLogDepositClassifierError(raw, err)
		return nil, fmt.Errorf("conversation: deposit classification failed: %w", err)
	}

	raw = strings.TrimSpace(resp.Text)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	var decision struct {
		Collect     bool   `json:"collect"`
		AmountCents int32  `json:"amount_cents"`
		SuccessURL  string `json:"success_url"`
		CancelURL   string `json:"cancel_url"`
		Description string `json:"description"`
	}
	jsonText := raw
	if !strings.HasPrefix(jsonText, "{") {
		start := strings.Index(jsonText, "{")
		end := strings.LastIndex(jsonText, "}")
		if start >= 0 && end > start {
			jsonText = jsonText[start : end+1]
		}
	}
	if err := json.Unmarshal([]byte(jsonText), &decision); err != nil {
		outcome = "error"
		s.maybeLogDepositClassifierError(raw, err)
		return nil, fmt.Errorf("conversation: deposit classification parse: %w", err)
	}
	if !decision.Collect {
		span.SetAttributes(attribute.Bool("medspa.deposit.collect", false))
		s.logger.Debug("deposit: classifier skipped", "model", s.model)
		return nil, nil
	}

	amount := decision.AmountCents
	if amount <= 0 {
		amount = s.deposit.DefaultAmountCents
	}
	outcome = "collect"

	intent := &DepositIntent{
		AmountCents: amount,
		Description: defaultString(decision.Description, s.deposit.Description),
		SuccessURL:  defaultString(decision.SuccessURL, s.deposit.SuccessURL),
		CancelURL:   defaultString(decision.CancelURL, s.deposit.CancelURL),
	}
	span.SetAttributes(
		attribute.Bool("medspa.deposit.collect", true),
		attribute.Int("medspa.deposit.amount_cents", int(amount)),
	)
	s.logger.Info("deposit: classifier collected",
		"model", s.model,
		"amount_cents", amount,
		"success_url_set", intent.SuccessURL != "",
		"cancel_url_set", intent.CancelURL != "",
		"description", intent.Description,
	)
	return intent, nil
}

func summarizeHistory(history []ChatMessage, limit int) string {
	if limit > 0 && len(history) > limit {
		history = history[len(history)-limit:]
	}
	var builder strings.Builder
	for _, msg := range history {
		builder.WriteString(msg.Role)
		builder.WriteString(": ")
		builder.WriteString(msg.Content)
		builder.WriteString("\n")
	}
	return builder.String()
}

func (s *LLMService) maybeLogDepositClassifierError(raw string, err error) {
	if s == nil || s.logger == nil || err == nil {
		return
	}
	if !s.shouldSampleDepositLog() {
		return
	}
	masked := strings.TrimSpace(raw)
	if len(masked) > 512 {
		masked = masked[:512] + "...(truncated)"
	}
	s.logger.Warn("deposit: classifier error",
		"model", s.model,
		"error", err,
		"raw", masked,
	)
}

func (s *LLMService) shouldSampleDepositLog() bool {
	// 10% sampling to avoid noisy logs.
	return time.Now().UnixNano()%10 == 0
}

// latestTurnAgreedToDeposit returns true when the most recent user message clearly indicates they want to pay a deposit.
// This is used as a deterministic fallback to avoid missing deposits due to LLM classifier variance.
func latestTurnAgreedToDeposit(history []ChatMessage) bool {
	userIndex := -1
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == ChatRoleUser {
			userIndex = i
			break
		}
	}
	if userIndex == -1 {
		return false
	}

	msg := strings.TrimSpace(history[userIndex].Content)
	if msg == "" {
		return false
	}
	if depositNegativeRE.MatchString(msg) {
		return false
	}
	if !depositAffirmativeRE.MatchString(msg) {
		return false
	}
	if depositKeywordRE.MatchString(msg) {
		return true
	}

	// Generic affirmative only counts if the assistant just asked about a deposit.
	for i := userIndex - 1; i >= 0; i-- {
		switch history[i].Role {
		case ChatRoleSystem:
			continue
		case ChatRoleAssistant:
			return depositAskRE.MatchString(history[i].Content)
		default:
			return false
		}
	}
	return false
}

func conversationHasDepositAgreement(history []ChatMessage) bool {
	for i := 0; i < len(history); i++ {
		if history[i].Role != ChatRoleAssistant {
			continue
		}
		if !depositAskRE.MatchString(history[i].Content) {
			continue
		}

		// Look ahead to the next user message (skipping system messages). If they affirm, we treat the
		// deposit as agreed even if the payment record hasn't persisted yet.
		for j := i + 1; j < len(history); j++ {
			switch history[j].Role {
			case ChatRoleSystem:
				continue
			case ChatRoleUser:
				msg := strings.TrimSpace(history[j].Content)
				if msg == "" {
					break
				}
				if depositNegativeRE.MatchString(msg) {
					break
				}
				if depositAffirmativeRE.MatchString(msg) {
					return true
				}
				break
			default:
				// Another assistant turn occurred before a user reply.
				break
			}
			break
		}
	}
	return false
}
