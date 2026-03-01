package main

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

// buildAdminMessagingHandler creates the admin messaging handler with quiet hours.
func buildAdminMessagingHandler(
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

// twDeps holds inputs for building the Telnyx webhook handler.
type twDeps struct {
	cfg               *appconfig.Config
	logger            *logging.Logger
	msgStore          *messaging.Store
	telnyxClient      *telnyxclient.Client
	processedStore    *events.ProcessedStore
	conversationPub   *conversation.Publisher
	leadsRepo         leads.Repository
	smsTranscript     *conversation.SMSTranscriptStore
	conversationStore *conversation.ConversationStore
	clinicStore       *clinic.Store
	messagingMetrics  *observemetrics.MessagingMetrics
}

// buildTelnyxWebhookHandler creates the Telnyx inbound webhook handler.
func buildTelnyxWebhookHandler(deps twDeps) *handlers.TelnyxWebhookHandler {
	if deps.msgStore == nil || deps.telnyxClient == nil || deps.processedStore == nil {
		deps.logger.Warn("telnyx webhook handler NOT created - missing prerequisites")
		return nil
	}

	h := handlers.NewTelnyxWebhookHandler(handlers.TelnyxWebhookConfig{
		Store:             deps.msgStore,
		Processed:         deps.processedStore,
		Telnyx:            deps.telnyxClient,
		Conversation:      deps.conversationPub,
		Leads:             deps.leadsRepo,
		Logger:            deps.logger,
		Transcript:        deps.smsTranscript,
		ConversationStore: deps.conversationStore,
		ClinicStore:       deps.clinicStore,
		MessagingProfile:  deps.cfg.TelnyxMessagingProfileID,
		StopAck:           deps.cfg.TelnyxStopReply,
		HelpAck:           deps.cfg.TelnyxHelpReply,
		StartAck:          deps.cfg.TelnyxStartReply,
		FirstContactAck:   deps.cfg.TelnyxFirstContactReply,
		VoiceAck:          deps.cfg.TelnyxVoiceAckReply,
		DemoMode:          deps.cfg.DemoMode,
		TrackJobs:         deps.cfg.TelnyxTrackJobs,
		Metrics:           deps.messagingMetrics,
	})
	deps.logger.Info("telnyx webhook handler initialized", "profile_id", deps.cfg.TelnyxMessagingProfileID)
	return h
}
