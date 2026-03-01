package bootstrap

import (
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/http/handlers"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/compliance"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/telnyxclient"
	observemetrics "github.com/wolfman30/medspa-ai-platform/internal/observability/metrics"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// BuildAdminMessagingHandler creates the admin messaging handler with quiet hours.
func BuildAdminMessagingHandler(
	cfg *appconfig.Config,
	logger *logging.Logger,
	msgStore *messaging.Store,
	telnyxClient *telnyxclient.Client,
	messagingMetrics *observemetrics.MessagingMetrics,
) *handlers.AdminMessagingHandler {
	if msgStore == nil || telnyxClient == nil {
		return nil
	}

	var quietHours compliance.QuietHours
	quietHoursEnabled := false
	if cfg.QuietHoursStart != "" && cfg.QuietHoursEnd != "" {
		if parsed, err := compliance.ParseQuietHours(cfg.QuietHoursStart, cfg.QuietHoursEnd, cfg.QuietHoursTimezone); err != nil {
			logger.Warn("invalid quiet hours configuration", "error", err)
		} else {
			quietHours = parsed
			quietHoursEnabled = true
		}
	}

	return handlers.NewAdminMessagingHandler(handlers.AdminMessagingConfig{
		Store:             msgStore,
		Logger:            logger,
		Telnyx:            telnyxClient,
		QuietHours:        quietHours,
		QuietHoursEnabled: quietHoursEnabled,
		MessagingProfile:  cfg.TelnyxMessagingProfileID,
		StopAck:           cfg.TelnyxStopReply,
		HelpAck:           cfg.TelnyxHelpReply,
		RetryBaseDelay:    cfg.TelnyxRetryBaseDelay,
		Metrics:           messagingMetrics,
	})
}

// TwDeps holds inputs for building the Telnyx webhook handler.
type TwDeps struct {
	Cfg               *appconfig.Config
	Logger            *logging.Logger
	MsgStore          *messaging.Store
	TelnyxClient      *telnyxclient.Client
	ProcessedStore    *events.ProcessedStore
	ConversationPub   *conversation.Publisher
	LeadsRepo         leads.Repository
	SMSTranscript     *conversation.SMSTranscriptStore
	ConversationStore *conversation.ConversationStore
	ClinicStore       *clinic.Store
	MessagingMetrics  *observemetrics.MessagingMetrics
}

// BuildTelnyxWebhookHandler creates the Telnyx inbound webhook handler.
func BuildTelnyxWebhookHandler(deps TwDeps) *handlers.TelnyxWebhookHandler {
	if deps.MsgStore == nil || deps.TelnyxClient == nil || deps.ProcessedStore == nil {
		deps.Logger.Warn("telnyx webhook handler NOT created - missing prerequisites")
		return nil
	}

	h := handlers.NewTelnyxWebhookHandler(handlers.TelnyxWebhookConfig{
		Store:             deps.MsgStore,
		Processed:         deps.ProcessedStore,
		Telnyx:            deps.TelnyxClient,
		Conversation:      deps.ConversationPub,
		Leads:             deps.LeadsRepo,
		Logger:            deps.Logger,
		Transcript:        deps.SMSTranscript,
		ConversationStore: deps.ConversationStore,
		ClinicStore:       deps.ClinicStore,
		MessagingProfile:  deps.Cfg.TelnyxMessagingProfileID,
		StopAck:           deps.Cfg.TelnyxStopReply,
		HelpAck:           deps.Cfg.TelnyxHelpReply,
		StartAck:          deps.Cfg.TelnyxStartReply,
		FirstContactAck:   deps.Cfg.TelnyxFirstContactReply,
		VoiceAck:          deps.Cfg.TelnyxVoiceAckReply,
		DemoMode:          deps.Cfg.DemoMode,
		TrackJobs:         deps.Cfg.TelnyxTrackJobs,
		Metrics:           deps.MessagingMetrics,
	})
	deps.Logger.Info("telnyx webhook handler initialized", "profile_id", deps.Cfg.TelnyxMessagingProfileID)
	return h
}
