package conversation

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/wolfman30/medspa-ai-platform/internal/events"
)

// OutboxDispatcher delivers stored events to the conversation queue.
type OutboxDispatcher struct {
	publisher *Publisher
}

func NewOutboxDispatcher(publisher *Publisher) *OutboxDispatcher {
	return &OutboxDispatcher{publisher: publisher}
}

func (d *OutboxDispatcher) Handle(ctx context.Context, entry events.OutboxEntry) error {
	switch entry.EventType {
	case "messaging.message.received.v1":
		// IMPORTANT: Telnyx webhooks already enqueue conversation jobs directly via dispatchConversation.
		// Processing this event here would cause DUPLICATE AI responses.
		// This event is written to the outbox for audit/observability purposes only.
		// If we ever need outbox-driven message processing (e.g., for a different provider),
		// we should add a separate event type or use a flag to distinguish.
		return nil
	case "payment_succeeded.v1":
		var evt events.PaymentSucceededV1
		if err := json.Unmarshal(entry.Payload, &evt); err != nil {
			return fmt.Errorf("conversation: decode payment event: %w", err)
		}
		return d.publisher.EnqueuePaymentSucceeded(ctx, evt)
	case "payment_failed.v1":
		var evt events.PaymentFailedV1
		if err := json.Unmarshal(entry.Payload, &evt); err != nil {
			return fmt.Errorf("conversation: decode payment failed event: %w", err)
		}
		return d.publisher.EnqueuePaymentFailed(ctx, evt)
	case "payments.deposit.requested.v1":
		// Conversation layer does not consume deposit requests; ignore gracefully.
		return nil
	default:
		return fmt.Errorf("conversation: unhandled outbox type %s", entry.EventType)
	}
}
