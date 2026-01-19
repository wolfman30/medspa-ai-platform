package bootstrap

import (
	auditcompliance "github.com/wolfman30/medspa-ai-platform/internal/compliance"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// BuildOutboundMessenger creates a reply messenger and applies standard wrappers.
func BuildOutboundMessenger(
	cfg *appconfig.Config,
	logger *logging.Logger,
	store *messaging.Store,
	audit *auditcompliance.AuditService,
	conversationStore *conversation.ConversationStore,
	transcriptStore *conversation.SMSTranscriptStore,
) (conversation.ReplyMessenger, string, string) {
	if cfg == nil {
		return nil, "", "missing config"
	}

	messengerCfg := messaging.ProviderSelectionConfig{
		Preference:       cfg.SMSProvider,
		TelnyxAPIKey:     cfg.TelnyxAPIKey,
		TelnyxProfileID:  cfg.TelnyxMessagingProfileID,
		TwilioAccountSID: cfg.TwilioAccountSID,
		TwilioAuthToken:  cfg.TwilioAuthToken,
		TwilioFromNumber: cfg.TwilioFromNumber,
	}
	messenger, provider, reason := messaging.BuildReplyMessenger(messengerCfg, logger)
	if messenger == nil {
		return nil, provider, reason
	}

	messenger = messaging.WrapWithDemoMode(messenger, messaging.DemoModeConfig{
		Enabled: cfg.DemoMode,
		Prefix:  cfg.DemoModePrefix,
		Suffix:  cfg.DemoModeSuffix,
		Logger:  logger,
	})
	messenger = messaging.WrapWithDisclaimers(messenger, messaging.DisclaimerWrapperConfig{
		Enabled:           cfg.DisclaimerEnabled,
		Level:             cfg.DisclaimerLevel,
		FirstMessageOnly:  cfg.DisclaimerFirstOnly,
		Logger:            logger,
		Audit:             audit,
		ConversationStore: conversationStore,
		TranscriptStore:   transcriptStore,
	})
	messenger = messaging.WrapWithPersistence(messenger, store, logger)
	return messenger, provider, reason
}
