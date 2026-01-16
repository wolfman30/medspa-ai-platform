package messaging

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// PersistingMessenger wraps a ReplyMessenger to persist outbound messages to the database.
// This enables viewing complete conversations (both inbound and outbound).
type PersistingMessenger struct {
	inner  conversation.ReplyMessenger
	store  *Store
	logger *logging.Logger
}

// WrapWithPersistence wraps a messenger to persist outbound messages.
// If store is nil, returns the original messenger unchanged.
func WrapWithPersistence(messenger conversation.ReplyMessenger, store *Store, logger *logging.Logger) conversation.ReplyMessenger {
	if store == nil {
		return messenger
	}
	if logger == nil {
		logger = logging.Default()
	}
	return &PersistingMessenger{
		inner:  messenger,
		store:  store,
		logger: logger,
	}
}

// SendReply persists the outbound message to the database, then sends it.
func (p *PersistingMessenger) SendReply(ctx context.Context, reply conversation.OutboundReply) error {
	// Try to resolve clinic ID from org ID
	var clinicID uuid.UUID
	if reply.OrgID != "" {
		if parsed, err := uuid.Parse(reply.OrgID); err == nil {
			clinicID = parsed
		}
	}

	// Persist the outbound message before sending
	now := time.Now()
	rec := MessageRecord{
		ClinicID:       clinicID,
		From:           reply.From,
		To:             reply.To,
		Direction:      "outbound",
		Body:           reply.Body,
		Media:          []string{},
		ProviderStatus: "pending",
		SendAttempts:   1,
		LastAttemptAt:  &now,
	}

	msgID, err := p.store.InsertMessage(ctx, nil, rec)
	if err != nil {
		// Log but don't fail - message delivery is more important than persistence
		p.logger.Warn("failed to persist outbound message", "error", err, "org_id", reply.OrgID, "to", reply.To)
	} else {
		p.logger.Debug("persisted outbound message", "msg_id", msgID, "org_id", reply.OrgID, "to", reply.To)
	}

	// Send the message
	sendErr := p.inner.SendReply(ctx, reply)

	// Update message status based on send result
	if msgID != uuid.Nil {
		if sendErr != nil {
			failedAt := time.Now()
			if updateErr := p.store.UpdateMessageStatusByID(ctx, msgID, "failed", nil, &failedAt); updateErr != nil {
				p.logger.Warn("failed to update message status to failed", "error", updateErr, "msg_id", msgID)
			}
		} else {
			deliveredAt := time.Now()
			if updateErr := p.store.UpdateMessageStatusByID(ctx, msgID, "sent", &deliveredAt, nil); updateErr != nil {
				p.logger.Warn("failed to update message status to sent", "error", updateErr, "msg_id", msgID)
			}
		}
	}

	return sendErr
}
