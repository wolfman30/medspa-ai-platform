package conversation

import (
	"context"
	"encoding/json"
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// ConversationEvent represents a structured event in the conversation lifecycle.
// All events share the same base fields for easy filtering/grep.
type ConversationEvent struct {
	Time           string         `json:"time"`
	Event          string         `json:"event"`
	ConversationID string         `json:"conversation_id"`
	OrgID          string         `json:"org_id"`
	LeadID         string         `json:"lead_id,omitempty"`
	Data           map[string]any `json:"data,omitempty"`
}

// EventLogger emits structured JSON events at each decision point in the
// conversation flow. Designed for fast grep/filter debugging:
//
//	grep '"event":"service_extracted"' /var/log/app.log
//	grep '"conversation_id":"conv_abc"' /var/log/app.log
type EventLogger struct {
	logger *logging.Logger
}

// NewEventLogger creates a new conversation event logger.
func NewEventLogger(logger *logging.Logger) *EventLogger {
	return &EventLogger{logger: logger}
}

// Log emits a structured conversation event.
func (e *EventLogger) Log(_ context.Context, event string, convID, orgID, leadID string, data map[string]any) {
	if e == nil || e.logger == nil {
		return
	}
	evt := ConversationEvent{
		Time:           time.Now().UTC().Format(time.RFC3339Nano),
		Event:          event,
		ConversationID: convID,
		OrgID:          orgID,
		LeadID:         leadID,
		Data:           data,
	}
	b, _ := json.Marshal(evt)
	e.logger.Info(string(b))
}

// Convenience methods for common events:

func (e *EventLogger) ConversationStarted(ctx context.Context, convID, orgID, leadID, from, source string) {
	e.Log(ctx, "conversation_started", convID, orgID, leadID, map[string]any{
		"from":   from,
		"source": source,
	})
}

func (e *EventLogger) MessageReceived(ctx context.Context, convID, orgID, leadID, message string) {
	// Truncate message for logging
	msg := message
	if len(msg) > 200 {
		msg = msg[:200] + "..."
	}
	e.Log(ctx, "message_received", convID, orgID, leadID, map[string]any{
		"message": msg,
	})
}

func (e *EventLogger) PromptInjectionDetected(ctx context.Context, convID, orgID string, blocked bool, score float64, reasons []string) {
	e.Log(ctx, "prompt_injection_detected", convID, orgID, "", map[string]any{
		"blocked": blocked,
		"score":   score,
		"reasons": reasons,
	})
}

func (e *EventLogger) ServiceExtracted(ctx context.Context, convID, orgID, service, resolvedTo string) {
	e.Log(ctx, "service_extracted", convID, orgID, "", map[string]any{
		"service":     service,
		"resolved_to": resolvedTo,
	})
}

func (e *EventLogger) VariantAsked(ctx context.Context, convID, orgID, service string, variants []string) {
	e.Log(ctx, "variant_asked", convID, orgID, "", map[string]any{
		"service":  service,
		"variants": variants,
	})
}

func (e *EventLogger) VariantResolved(ctx context.Context, convID, orgID, service, variant, method string) {
	e.Log(ctx, "variant_resolved", convID, orgID, "", map[string]any{
		"service": service,
		"variant": variant,
		"method":  method, // "llm" or "keyword"
	})
}

func (e *EventLogger) QualificationStep(ctx context.Context, convID, orgID, step string, data map[string]any) {
	d := map[string]any{"step": step}
	for k, v := range data {
		d[k] = v
	}
	e.Log(ctx, "qualification_step", convID, orgID, "", d)
}

func (e *EventLogger) AvailabilityFetched(ctx context.Context, convID, orgID, service string, slotCount int, durationMs int64) {
	e.Log(ctx, "availability_fetched", convID, orgID, "", map[string]any{
		"service":     service,
		"slot_count":  slotCount,
		"duration_ms": durationMs,
	})
}

func (e *EventLogger) TimeSlotSelected(ctx context.Context, convID, orgID, slot string, slotIndex int) {
	e.Log(ctx, "time_slot_selected", convID, orgID, "", map[string]any{
		"slot":       slot,
		"slot_index": slotIndex,
	})
}

func (e *EventLogger) DepositLinkSent(ctx context.Context, convID, orgID string, amountCents int, provider string) {
	e.Log(ctx, "deposit_link_sent", convID, orgID, "", map[string]any{
		"amount_cents": amountCents,
		"provider":     provider,
	})
}

func (e *EventLogger) PaymentReceived(ctx context.Context, convID, orgID string, amountCents int, provider string) {
	e.Log(ctx, "payment_received", convID, orgID, "", map[string]any{
		"amount_cents": amountCents,
		"provider":     provider,
	})
}

func (e *EventLogger) BookingCreated(ctx context.Context, convID, orgID, service, dateTime string, dryRun bool) {
	e.Log(ctx, "booking_created", convID, orgID, "", map[string]any{
		"service":  service,
		"datetime": dateTime,
		"dry_run":  dryRun,
	})
}

func (e *EventLogger) BookingPoliciesSent(ctx context.Context, convID, orgID string, policyCount int) {
	e.Log(ctx, "booking_policies_sent", convID, orgID, "", map[string]any{
		"policy_count": policyCount,
	})
}

func (e *EventLogger) OutputGuardTriggered(ctx context.Context, convID, orgID, patternID string) {
	e.Log(ctx, "output_guard_triggered", convID, orgID, "", map[string]any{
		"pattern_id": patternID,
	})
}

func (e *EventLogger) ProviderPreferenceAsked(ctx context.Context, convID, orgID, service string, providerCount int) {
	e.Log(ctx, "provider_preference_asked", convID, orgID, "", map[string]any{
		"service":        service,
		"provider_count": providerCount,
	})
}

func (e *EventLogger) LLMResponseGenerated(ctx context.Context, convID, orgID string, durationMs int64, tokenCount int) {
	e.Log(ctx, "llm_response_generated", convID, orgID, "", map[string]any{
		"duration_ms": durationMs,
		"tokens":      tokenCount,
	})
}

func (e *EventLogger) SMSSent(ctx context.Context, convID, orgID, to string, bodyLen int) {
	e.Log(ctx, "sms_sent", convID, orgID, "", map[string]any{
		"to":       to,
		"body_len": bodyLen,
	})
}

func (e *EventLogger) ErrorOccurred(ctx context.Context, convID, orgID, step string, err error) {
	e.Log(ctx, "error", convID, orgID, "", map[string]any{
		"step":  step,
		"error": err.Error(),
	})
}

func (e *EventLogger) SecondServiceRequested(ctx context.Context, convID, orgID, newService string) {
	e.Log(ctx, "second_service_requested", convID, orgID, "", map[string]any{
		"service": newService,
	})
}

func (e *EventLogger) TCPAAction(ctx context.Context, convID, orgID, action, from string) {
	e.Log(ctx, "tcpa_action", convID, orgID, "", map[string]any{
		"action": action, // "stop", "help", "start"
		"from":   from,
	})
}
