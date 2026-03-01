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

type AdminMessagingDeps struct {
	Cfg              *appconfig.Config
	Logger           *logging.Logger
	MessageStore     *messaging.Store
	TelnyxClient     *telnyxclient.Client
	MessagingMetrics *observemetrics.MessagingMetrics
}

// BuildAdminMessagingHandler creates the admin messaging handler with quiet hours.
func BuildAdminMessagingHandler(deps AdminMessagingDeps) *handlers.AdminMessagingHandler {
	if deps.MessageStore == nil || deps.TelnyxClient == nil {
		return nil
	}

	var quietHours compliance.QuietHours
	quietHoursEnabled := false
	if deps.Cfg.QuietHoursStart != "" && deps.Cfg.QuietHoursEnd != "" {
		if parsed, err := compliance.ParseQuietHours(deps.Cfg.QuietHoursStart, deps.Cfg.QuietHoursEnd, deps.Cfg.QuietHoursTimezone); err != nil {
			deps.Logger.Warn("invalid quiet hours configuration", "error", err)
		} else {
			quietHours = parsed
			quietHoursEnabled = true
		}
	}

	return handlers.NewAdminMessagingHandler(handlers.AdminMessagingConfig{
		Store:             deps.MessageStore,
		Logger:            deps.Logger,
		Telnyx:            deps.TelnyxClient,
		QuietHours:        quietHours,
		QuietHoursEnabled: quietHoursEnabled,
		MessagingProfile:  deps.Cfg.TelnyxMessagingProfileID,
		StopAck:           deps.Cfg.TelnyxStopReply,
		HelpAck:           deps.Cfg.TelnyxHelpReply,
		RetryBaseDelay:    deps.Cfg.TelnyxRetryBaseDelay,
		Metrics:           deps.MessagingMetrics,
	})
}

// TelnyxWebhookDeps holds inputs for building the Telnyx webhook handler.
type TelnyxWebhookDeps struct {
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
func BuildTelnyxWebhookHandler(deps TelnyxWebhookDeps) *handlers.TelnyxWebhookHandler {
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
