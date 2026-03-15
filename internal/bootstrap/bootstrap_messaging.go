package bootstrap

import (
	"encoding/json"
	"strings"

	appbootstrap "github.com/wolfman30/medspa-ai-platform/internal/app/bootstrap"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	auditcompliance "github.com/wolfman30/medspa-ai-platform/internal/compliance"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// MessagingDeps holds dependencies for initializing SMS webhook handling
// and outbound messaging.
type MessagingDeps struct {
	Cfg                   *appconfig.Config
	Logger                *logging.Logger
	ConversationPublisher *conversation.Publisher
	LeadsRepo             leads.Repository
	MessageStore          *messaging.Store
	AuditService          *auditcompliance.AuditService
	ConversationStore     *conversation.ConversationStore
	SMSTranscriptStore    *conversation.SMSTranscriptStore
	ClinicStore           *clinic.Store
}

// MessagingBootstrap holds the assembled messaging handler, org resolver,
// and outbound messenger for SMS processing.
type MessagingBootstrap struct {
	MessagingHandler *messaging.Handler
	Resolver         *messaging.StaticOrgResolver
	WebhookMessenger conversation.ReplyMessenger
	MessengerReason  string
}

// BootstrapMessaging wires up the SMS webhook handler, org-to-number routing,
// and outbound messenger (Telnyx or Twilio).
func BootstrapMessaging(deps MessagingDeps) MessagingBootstrap {
	cfg := deps.Cfg
	logger := deps.Logger
	conversationPublisher := deps.ConversationPublisher
	leadsRepo := deps.LeadsRepo
	msgStore := deps.MessageStore
	auditSvc := deps.AuditService
	conversationStore := deps.ConversationStore
	smsTranscript := deps.SMSTranscriptStore
	clinicStore := deps.ClinicStore
	orgRouting := map[string]string{}
	if raw := strings.TrimSpace(cfg.TwilioOrgMapJSON); raw != "" {
		if err := json.Unmarshal([]byte(raw), &orgRouting); err != nil {
			logger.Warn("failed to parse TWILIO_ORG_MAP_JSON", "error", err)
		}
	}
	if len(orgRouting) == 0 {
		logger.Warn("TWILIO_ORG_MAP_JSON empty; SMS webhooks will be rejected unless numbers are configured")
	}
	resolver := messaging.NewStaticOrgResolver(orgRouting)
	twilioWebhookSecret := cfg.TwilioWebhookSecret
	if twilioWebhookSecret == "" {
		twilioWebhookSecret = cfg.TwilioAuthToken
	}
	webhookMessenger, webhookMessengerProvider, webhookMessengerReason := appbootstrap.BuildOutboundMessenger(
		cfg,
		logger,
		msgStore,
		auditSvc,
		conversationStore,
		smsTranscript,
	)
	if webhookMessenger != nil {
		logger.Info("sms messenger initialized for webhooks",
			"provider", webhookMessengerProvider,
			"preference", cfg.SMSProvider,
		)
	} else {
		logger.Warn("sms replies disabled for webhooks",
			"preference", cfg.SMSProvider,
			"reason", webhookMessengerReason,
		)
	}
	messagingHandler := messaging.NewHandler(twilioWebhookSecret, conversationPublisher, resolver, webhookMessenger, leadsRepo, logger)
	messagingHandler.SetConversationStore(conversationStore)
	messagingHandler.SetClinicStore(clinicStore)
	messagingHandler.SetPublicBaseURL(cfg.PublicBaseURL)
	messagingHandler.SetSkipSignature(cfg.TwilioSkipSignature)

	if cfg.TwilioSkipSignature && (cfg.Env == "production" || cfg.Env == "staging") {
		logger.Error("SECURITY WARNING: TWILIO_SKIP_SIGNATURE is enabled in production/staging - this is a security risk!")
	}

	return MessagingBootstrap{MessagingHandler: messagingHandler, Resolver: resolver, WebhookMessenger: webhookMessenger, MessengerReason: webhookMessengerReason}
}
