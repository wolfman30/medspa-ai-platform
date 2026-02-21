package instagram

import (
	"context"
	"net/http"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// Adapter is the Instagram DM channel adapter.
// It handles inbound webhooks from Meta and sends outbound messages
// via the Graph API. It does NOT connect to the conversation engine;
// that wiring will be done in a separate PR.
type Adapter struct {
	client  *Client
	webhook *WebhookHandler
	logger  *logging.Logger
}

// NewAdapter creates a new Instagram DM adapter.
func NewAdapter(pageAccessToken, appSecret, verifyToken string, logger *logging.Logger) *Adapter {
	a := &Adapter{
		client: NewClient(pageAccessToken),
		logger: logger,
	}

	a.webhook = NewWebhookHandler(verifyToken, appSecret, func(msg ParsedInboundMessage) {
		logger.Info("instagram: inbound message",
			"sender_id", msg.SenderID,
			"is_postback", msg.IsPostback,
			"timestamp", msg.Timestamp,
		)
		// TODO: feed into conversation engine (separate PR)
	})

	return a
}

// HandleVerification handles GET /webhooks/instagram (Meta challenge).
func (a *Adapter) HandleVerification(w http.ResponseWriter, r *http.Request) {
	a.webhook.HandleVerification(w, r)
}

// HandleWebhook handles POST /webhooks/instagram (inbound messages).
func (a *Adapter) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	a.webhook.HandleInbound(w, r)
}

// SendMessage sends a text DM to the given Instagram user.
func (a *Adapter) SendMessage(ctx context.Context, recipientID, text string) error {
	_, err := a.client.SendTextMessage(ctx, recipientID, text)
	if err != nil {
		a.logger.Error("instagram: failed to send message",
			"recipient_id", recipientID,
			"error", err,
		)
	}
	return err
}

// SendButtonMessage sends a button template DM.
func (a *Adapter) SendButtonMessage(ctx context.Context, recipientID, text string, buttons []Button) error {
	_, err := a.client.SendButtonMessage(ctx, recipientID, text, buttons)
	if err != nil {
		a.logger.Error("instagram: failed to send button message",
			"recipient_id", recipientID,
			"error", err,
		)
	}
	return err
}
