package webchat

import (
	"context"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// ReplyMessenger implements conversation.ReplyMessenger for web chat.
// It pushes AI replies back through the WebSocket connection.
type ReplyMessenger struct {
	handler *Handler
	logger  *logging.Logger
}

// NewReplyMessenger creates a webchat reply messenger.
func NewReplyMessenger(handler *Handler, logger *logging.Logger) *ReplyMessenger {
	if logger == nil {
		logger = logging.Default()
	}
	return &ReplyMessenger{handler: handler, logger: logger}
}

// SendReply pushes the AI response to the visitor's WebSocket.
func (m *ReplyMessenger) SendReply(ctx context.Context, reply conversation.OutboundReply) error {
	convID := reply.ConversationID

	// Store the outbound message in transcript
	if m.handler.transcript != nil {
		_ = m.handler.transcript.Append(ctx, convID, conversation.SMSTranscriptMessage{
			Role:      "assistant",
			From:      reply.From,
			To:        reply.To,
			Body:      reply.Body,
			Timestamp: time.Now().UTC(),
			Kind:      "webchat_reply",
		})
	}

	m.handler.SendToSession(convID, OutboundMessage{
		Type:      "message",
		Role:      "assistant",
		Text:      reply.Body,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})

	m.logger.Info("webchat: reply sent",
		"conversation_id", convID,
		"org_id", reply.OrgID,
		"length", len(reply.Body),
	)
	return nil
}
