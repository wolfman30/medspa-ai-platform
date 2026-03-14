package conversation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"go.opentelemetry.io/otel/attribute"
)

const llmCompletionTimeout = 60 * time.Second

// generateResponse sends conversation history to the configured LLM and returns assistant text.
func (s *LLMService) generateResponse(ctx context.Context, history []ChatMessage) (string, error) {
	ctx, span := llmTracer.Start(ctx, "conversation.llm")
	defer span.End()

	trimmed := trimHistory(history, maxHistoryMessages)
	system, messages := splitSystemAndMessages(trimmed)

	model := s.model
	if m, ok := ctx.Value(ctxKeyVoiceModel).(string); ok && m != "" {
		model = m
	}
	req := LLMRequest{
		Model:       model,
		System:      system,
		Messages:    messages,
		MaxTokens:   llmMaxTokens,
		Temperature: llmTemperature,
	}
	callCtx, cancel := context.WithTimeout(ctx, llmCompletionTimeout)
	defer cancel()

	start := time.Now()
	resp, err := s.client.Complete(callCtx, req)
	latency := time.Since(start)
	status := "ok"
	if err != nil {
		status = "error"
	}
	llmLatency.WithLabelValues(s.model, status).Observe(latency.Seconds())
	if span.IsRecording() {
		span.SetAttributes(
			attribute.Float64("medspa.llm.latency_ms", float64(latency.Milliseconds())),
			attribute.String("medspa.llm.model", s.model),
			attribute.Int("medspa.llm.input_tokens", int(resp.Usage.InputTokens)),
			attribute.Int("medspa.llm.output_tokens", int(resp.Usage.OutputTokens)),
			attribute.Int("medspa.llm.total_tokens", int(resp.Usage.TotalTokens)),
			attribute.String("medspa.llm.stop_reason", resp.StopReason),
		)
	}
	if err != nil {
		span.RecordError(err)
		s.logger.Warn("llm completion failed", "model", s.model, "latency_ms", latency.Milliseconds(), "error", err)
		return "", fmt.Errorf("conversation: llm completion failed: %w", err)
	}
	if resp.Usage.InputTokens > 0 {
		llmTokensTotal.WithLabelValues(s.model, "input").Add(float64(resp.Usage.InputTokens))
	}
	if resp.Usage.OutputTokens > 0 {
		llmTokensTotal.WithLabelValues(s.model, "output").Add(float64(resp.Usage.OutputTokens))
	}
	if resp.Usage.TotalTokens > 0 {
		llmTokensTotal.WithLabelValues(s.model, "total").Add(float64(resp.Usage.TotalTokens))
	}

	text := strings.TrimSpace(resp.Text)
	s.logger.Info("llm completion finished",
		"model", s.model,
		"latency_ms", latency.Milliseconds(),
		"input_tokens", resp.Usage.InputTokens,
		"output_tokens", resp.Usage.OutputTokens,
		"total_tokens", resp.Usage.TotalTokens,
		"stop_reason", resp.StopReason,
	)
	if text == "" {
		err := errors.New("conversation: llm returned empty response")
		span.RecordError(err)
		return "", err
	}
	return text, nil
}

// AppendAssistantMessage appends an assistant message to conversation history.
func (s *LLMService) AppendAssistantMessage(ctx context.Context, conversationID, message string) error {
	history, err := s.history.Load(ctx, conversationID)
	if err != nil {
		return fmt.Errorf("load history: %w", err)
	}
	history = append(history, ChatMessage{Role: ChatRoleAssistant, Content: message})
	return s.history.Save(ctx, conversationID, history)
}

// ClearLeadPreferences resets scheduling preferences for a lead identified by org + phone.
func (s *LLMService) ClearLeadPreferences(ctx context.Context, orgID, phone string) error {
	if s.leadsRepo == nil {
		return nil
	}
	lead, err := s.leadsRepo.GetOrCreateByPhone(ctx, orgID, phone, "voice", "")
	if err != nil {
		return fmt.Errorf("get lead: %w", err)
	}
	return s.leadsRepo.UpdateSchedulingPreferences(ctx, lead.ID, leads.SchedulingPreferences{})
}
