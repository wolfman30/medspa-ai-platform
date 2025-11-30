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
		// Unwrap the nested payload structure
		var wrapper struct {
			Payload       events.MessageReceivedV1 `json:"payload"`
			CorrelationID string                   `json:"correlation_id"`
		}
		if err := json.Unmarshal(entry.Payload, &wrapper); err != nil {
			return fmt.Errorf("conversation: decode message event: %w", err)
		}
		evt := wrapper.Payload
		correlationID := wrapper.CorrelationID
		if correlationID == "" {
			correlationID = evt.CorrelationID
		}
		req := MessageRequest{
			OrgID:          evt.ClinicID,
			LeadID:         fmt.Sprintf("%s:%s", evt.ClinicID, evt.FromE164),
			ConversationID: fmt.Sprintf("sms:%s:%s", evt.ClinicID, evt.FromE164),
			ClinicID:       evt.ClinicID,
			From:           evt.FromE164,
			To:             evt.ToE164,
			Message:        evt.Body,
			Channel:        ChannelSMS,
		}
		return d.publisher.EnqueueMessage(ctx, correlationID, req)
	case "payment_succeeded.v1":
		var evt events.PaymentSucceededV1
		if err := json.Unmarshal(entry.Payload, &evt); err != nil {
			return fmt.Errorf("conversation: decode payment event: %w", err)
		}
		return d.publisher.EnqueuePaymentSucceeded(ctx, evt)
	case "payments.deposit.requested.v1":
		// Conversation layer does not consume deposit requests; ignore gracefully.
		return nil
	default:
		return fmt.Errorf("conversation: unhandled outbox type %s", entry.EventType)
	}
}
