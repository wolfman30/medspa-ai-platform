package instagram

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// Publisher enqueues conversation jobs.
type Publisher interface {
	EnqueueMessage(ctx context.Context, jobID string, req conversation.MessageRequest, opts ...conversation.PublishOption) error
}

// LeadResolver finds or creates a lead for the Instagram sender.
type LeadResolver interface {
	FindOrCreateByInstagramID(ctx context.Context, orgID, igSenderID string) (string, bool, error)
}

// OrgResolver resolves an org/clinic ID from an Instagram page ID.
type OrgResolver interface {
	ResolveByInstagramPageID(ctx context.Context, pageID string) (string, error)
}

// Adapter is the Instagram DM channel adapter.
// It handles inbound webhooks from Meta and sends outbound messages
// via the Graph API, wired into the shared conversation engine.
type Adapter struct {
	client       *Client
	webhook      *WebhookHandler
	publisher    Publisher
	leadResolver LeadResolver
	orgResolver  OrgResolver
	logger       *logging.Logger
}

// AdapterConfig holds configuration for creating an Instagram adapter.
type AdapterConfig struct {
	PageAccessToken string
	AppSecret       string
	VerifyToken     string
	Publisher       Publisher
	LeadResolver    LeadResolver
	OrgResolver     OrgResolver
	Logger          *logging.Logger
}

// NewAdapter creates a new Instagram DM adapter wired into the conversation engine.
func NewAdapter(cfg AdapterConfig) *Adapter {
	if cfg.Logger == nil {
		cfg.Logger = logging.Default()
	}

	a := &Adapter{
		client:       NewClient(cfg.PageAccessToken),
		publisher:    cfg.Publisher,
		leadResolver: cfg.LeadResolver,
		orgResolver:  cfg.OrgResolver,
		logger:       cfg.Logger,
	}

	a.webhook = NewWebhookHandler(cfg.VerifyToken, cfg.AppSecret, func(msg ParsedInboundMessage) {
		a.handleInboundMessage(msg)
	})

	return a
}

// handleInboundMessage normalizes an Instagram DM into a conversation MessageRequest
// and enqueues it for the conversation engine.
func (a *Adapter) handleInboundMessage(msg ParsedInboundMessage) {
	ctx := context.Background()

	a.logger.Info("instagram: inbound message",
		"sender_id", msg.SenderID,
		"message_id", msg.MessageID,
		"is_postback", msg.IsPostback,
		"timestamp", msg.Timestamp,
	)

	if a.publisher == nil {
		a.logger.Warn("instagram: publisher not configured, dropping message")
		return
	}

	// Resolve the org from the page that received the message
	orgID := "default"
	if a.orgResolver != nil {
		resolved, err := a.orgResolver.ResolveByInstagramPageID(ctx, msg.RecipientID)
		if err != nil {
			a.logger.Error("instagram: failed to resolve org", "error", err, "recipient_id", msg.RecipientID)
			// Fall through with default - don't drop the message
		} else {
			orgID = resolved
		}
	}

	// Find or create lead
	leadID := ""
	if a.leadResolver != nil {
		lid, _, err := a.leadResolver.FindOrCreateByInstagramID(ctx, orgID, msg.SenderID)
		if err != nil {
			a.logger.Error("instagram: failed to resolve lead", "error", err, "sender_id", msg.SenderID)
		} else {
			leadID = lid
		}
	}

	conversationID := deterministicConversationID(orgID, msg.SenderID)
	text := msg.Text
	if msg.IsPostback {
		text = msg.PostbackPayload
		if text == "" {
			text = msg.Text
		}
	}

	jobID := msg.MessageID
	if jobID == "" {
		jobID = uuid.New().String()
	}

	msgReq := conversation.MessageRequest{
		OrgID:          orgID,
		LeadID:         leadID,
		ConversationID: conversationID,
		Message:        text,
		ClinicID:       orgID,
		Channel:        conversation.ChannelInstagram,
		From:           msg.SenderID,
		To:             msg.RecipientID,
		Metadata: map[string]string{
			"instagram_message_id": msg.MessageID,
			"channel":              "instagram",
		},
	}

	if err := a.publisher.EnqueueMessage(ctx, jobID, msgReq, conversation.WithoutJobTracking()); err != nil {
		a.logger.Error("instagram: failed to enqueue conversation job",
			"error", err,
			"sender_id", msg.SenderID,
			"message_id", msg.MessageID,
		)
	}
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

// SendReply implements conversation.ReplyMessenger for Instagram.
func (a *Adapter) SendReply(ctx context.Context, reply conversation.OutboundReply) error {
	return a.SendMessage(ctx, reply.To, reply.Body)
}

// SetGraphAPIBase overrides the Graph API base URL (useful for testing).
func (a *Adapter) SetGraphAPIBase(base string) {
	a.client.SetGraphAPIBase(base)
}

// deterministicConversationID creates a stable conversation ID for an org + IG sender pair.
func deterministicConversationID(orgID, senderID string) string {
	return fmt.Sprintf("ig_%s_%s", orgID, senderID)
}

// PatientIdentity represents a linked patient identity across channels.
type PatientIdentity struct {
	PatientID   string
	ChannelType string // "sms", "instagram", "voice"
	ChannelID   string // phone number or IG user ID
	OrgID       string
}

// IdentityStore manages cross-channel patient identity linking.
type IdentityStore interface {
	// LinkInstagramToPhone links an Instagram sender ID to a phone number,
	// unifying the patient identity across channels.
	LinkInstagramToPhone(ctx context.Context, orgID, igSenderID, phoneE164 string) error

	// FindPatientByInstagramID looks up a patient by their Instagram sender ID.
	FindPatientByInstagramID(ctx context.Context, orgID, igSenderID string) (*PatientIdentity, error)

	// FindPatientByPhone looks up a patient by phone number.
	FindPatientByPhone(ctx context.Context, orgID, phoneE164 string) (*PatientIdentity, error)
}

// SimpleLeadResolver implements LeadResolver using the existing leads repository.
type SimpleLeadResolver struct {
	repo leads.Repository
}

// NewSimpleLeadResolver creates a lead resolver backed by the leads repository.
func NewSimpleLeadResolver(repo leads.Repository) *SimpleLeadResolver {
	return &SimpleLeadResolver{repo: repo}
}

// FindOrCreateByInstagramID finds or creates a lead for the given Instagram sender.
func (r *SimpleLeadResolver) FindOrCreateByInstagramID(ctx context.Context, orgID, igSenderID string) (string, bool, error) {
	if r.repo == nil {
		return "", false, fmt.Errorf("leads repository not configured")
	}
	// Use the IG sender ID as a phone placeholder with ig: prefix to distinguish
	igIdentifier := "ig:" + igSenderID
	lead, err := r.repo.GetOrCreateByPhone(ctx, orgID, igIdentifier, "instagram_dm", "")
	if err != nil {
		return "", false, err
	}
	return lead.ID, false, nil
}
