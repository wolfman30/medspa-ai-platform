package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/emr/boulevard"
	moxieclient "github.com/wolfman30/medspa-ai-platform/internal/emr/moxie"
	"github.com/wolfman30/medspa-ai-platform/internal/http/handlers"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/internal/payments"
	"github.com/wolfman30/medspa-ai-platform/internal/voice"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// VoiceBootstrap holds handlers for voice AI (Telnyx Call Control,
// Nova Sonic WebSocket bridge, and voice webhook processing).
type VoiceBootstrap struct {
	VoiceAIHandler *handlers.VoiceAIHandler
	VoiceWSHandler *voice.TelnyxWSHandler
	CallControl    *handlers.CallControlHandler
}

// VoiceDeps holds dependencies for initializing voice AI handlers.
type VoiceDeps struct {
	Cfg                   *appconfig.Config
	Logger                *logging.Logger
	MsgStore              *messaging.Store
	ClinicStore           *clinic.Store
	ConversationPublisher *conversation.Publisher
	ConversationService   conversation.Service
	ConversationStore     *conversation.ConversationStore
	RedisClient           *redis.Client
	WebhookMessenger      conversation.ReplyMessenger
	LeadsRepo             leads.Repository
	Resolver              *messaging.StaticOrgResolver
}

// BootstrapVoice initializes the voice AI handler, Nova Sonic WebSocket
// bridge, and Telnyx Call Control handler based on config availability.
func BootstrapVoice(deps VoiceDeps) VoiceBootstrap {
	cfg := deps.Cfg
	logger := deps.Logger
	msgStore := deps.MsgStore
	clinicStore := deps.ClinicStore
	conversationPublisher := deps.ConversationPublisher
	conversationService := deps.ConversationService
	conversationStore := deps.ConversationStore
	redisClient := deps.RedisClient
	webhookMessenger := deps.WebhookMessenger
	leadsRepo := deps.LeadsRepo
	resolver := deps.Resolver

	var voiceAIHandler *handlers.VoiceAIHandler
	if msgStore != nil && clinicStore != nil {
		voiceAIHandler = handlers.NewVoiceAIHandler(handlers.VoiceAIHandlerConfig{
			Store:       msgStore,
			Publisher:   conversationPublisher,
			Processor:   conversationService,
			ClinicStore: clinicStore,
			ConvStore:   conversationStore,
			Redis:       redisClient,
			Logger:      logger,
		})
		logger.Info("voice AI handler initialized")
	}

	voiceWSHandler := bootstrapVoiceWebSocket(cfg, logger, clinicStore, conversationStore, redisClient, webhookMessenger, leadsRepo, resolver)

	var callControlHandler *handlers.CallControlHandler
	if cfg.NovaSonicStreamURL != "" && cfg.TelnyxAPIKey != "" {
		postCallSMS := voice.NewPostCallSMSService(voice.PostCallSMSConfig{
			Logger:      slog.Default(),
			Messenger:   webhookMessenger,
			OrgResolver: resolver,
			ClinicStore: clinicStore,
		})

		callControlHandler = handlers.NewCallControlHandler(handlers.CallControlConfig{
			Logger:       logger,
			TelnyxAPIKey: cfg.TelnyxAPIKey,
			StreamURL:    cfg.NovaSonicStreamURL,
			OrgResolver:  resolver,
			ClinicStore:  clinicStore,
			PostCallSMS:  postCallSMS,
		})
		logger.Info("call control handler initialized", "stream_url", cfg.NovaSonicStreamURL)
	}

	return VoiceBootstrap{VoiceAIHandler: voiceAIHandler, VoiceWSHandler: voiceWSHandler, CallControl: callControlHandler}
}

// bootstrapVoiceWebSocket sets up the Nova Sonic WebSocket handler with
// Boulevard availability pre-fetching and voice bridge factory.
func bootstrapVoiceWebSocket(
	cfg *appconfig.Config,
	logger *logging.Logger,
	clinicStore *clinic.Store,
	conversationStore *conversation.ConversationStore,
	redisClient *redis.Client,
	webhookMessenger conversation.ReplyMessenger,
	leadsRepo leads.Repository,
	resolver *messaging.StaticOrgResolver,
) *voice.TelnyxWSHandler {
	if cfg.NovaSonicSidecarURL == "" {
		return nil
	}

	sidecarURL := cfg.NovaSonicSidecarURL
	novaSonicVoice := cfg.NovaSonicVoice

	var voiceCheckoutSvc voice.CheckoutLinkCreator
	if cfg.StripeSecretKey != "" {
		stripeSvc := payments.NewStripeCheckoutService(cfg.StripeSecretKey, cfg.StripeSuccessURL, cfg.StripeCancelURL, logger)
		if clinicStore != nil {
			voiceCheckoutSvc = payments.NewMultiCheckoutService(nil, stripeSvc, clinicStore, logger)
		} else {
			voiceCheckoutSvc = stripeSvc
		}
		logger.Info("voice: Stripe checkout service configured for deposits")
	} else {
		logger.Warn("voice: no STRIPE_SECRET_KEY — deposits will use fallback test link")
	}

	voiceToolDeps := &voice.ToolDeps{
		MoxieClient: func() *moxieclient.Client {
			moxieDryRun := os.Getenv("MOXIE_DRY_RUN") == "true"
			return moxieclient.NewClient(logger, moxieclient.WithDryRun(moxieDryRun))
		}(),
		Messenger:       webhookMessenger,
		ClinicStore:     clinicStore,
		LeadsRepo:       leadsRepo,
		CheckoutService: voiceCheckoutSvc,
	}

	voiceWSHandler := voice.NewTelnyxWSHandler(slog.Default(), func(l *slog.Logger, callControlID string, mediaFormat voice.TelnyxMediaFormat, callCtx voice.CallContext) (voice.VoiceBridge, error) {
		systemPrompt := voice.BuildVoiceSystemPrompt(l, voiceToolDeps.ClinicStore, callCtx.OrgID)
		systemPrompt = appendBoulevardAvailability(systemPrompt, voiceToolDeps.ClinicStore, callCtx.OrgID, logger)

		clinicName := resolveClinicName(voiceToolDeps.ClinicStore, callCtx.OrgID)
		voiceGreeting := "__SKIP__"
		conversationID := "voice:" + callCtx.OrgID + ":" + strings.TrimPrefix(callCtx.From, "+")
		onTranscript := buildTranscriptCallback(conversationStore, conversationID, callCtx, logger)

		if voice.GetVoiceEngine() == "modular" {
			return voice.NewModularBridge(
				context.Background(),
				voice.ModularBridgeConfig{
					DeepgramAPIKey:   os.Getenv("DEEPGRAM_API_KEY"),
					ElevenLabsAPIKey: os.Getenv("ELEVENLABS_API_KEY"),
					SystemPrompt:     systemPrompt,
					OrgID:            callCtx.OrgID,
					CallerPhone:      callCtx.From,
					CalledPhone:      callCtx.To,
					ClinicName:       clinicName,
					ToolDeps:         voiceToolDeps,
					Redis:            redisClient,
					OnTranscript:     onTranscript,
					ConversationID:   conversationID,
				},
				callControlID,
				mediaFormat,
				l,
			)
		}

		return voice.NewBridge(
			context.Background(),
			voice.BridgeConfig{
				SidecarURL:     sidecarURL,
				SystemPrompt:   systemPrompt,
				Voice:          novaSonicVoice,
				OrgID:          callCtx.OrgID,
				CallerPhone:    callCtx.From,
				CalledPhone:    callCtx.To,
				ClinicName:     clinicName,
				Greeting:       voiceGreeting,
				ToolDeps:       voiceToolDeps,
				Redis:          redisClient,
				OnTranscript:   onTranscript,
				ConversationID: conversationID,
			},
			callControlID,
			mediaFormat,
			l,
		)
	})
	logger.Info("nova sonic voice WebSocket handler initialized", "sidecar_url", sidecarURL)
	return voiceWSHandler
}

// appendBoulevardAvailability pre-fetches Boulevard slots and appends them
// to the system prompt so the voice agent has real availability data.
func appendBoulevardAvailability(systemPrompt string, clinicStore *clinic.Store, orgID string, logger *logging.Logger) string {
	if clinicStore == nil {
		return systemPrompt
	}
	clinicCfg, err := clinicStore.Get(context.Background(), orgID)
	if err != nil || clinicCfg == nil || !clinicCfg.UsesBoulevardBooking() {
		return systemPrompt
	}
	blvdClient := boulevard.NewBoulevardClient(clinicCfg.BoulevardBusinessID, clinicCfg.BoulevardLocationID, logger)
	if blvdClient == nil {
		return systemPrompt
	}
	adapter := boulevard.NewBoulevardAdapter(blvdClient, os.Getenv("MOXIE_DRY_RUN") == "true", logger)
	fetchCtx, fetchCancel := context.WithTimeout(context.Background(), 8*time.Second)
	slots, _, blvdErr := adapter.ResolveAvailabilityWithCart(fetchCtx, "Botox", "", time.Now())
	fetchCancel()
	if blvdErr != nil {
		logger.Warn("voice: Boulevard prefetch failed", "error", blvdErr, "org_id", orgID)
		return systemPrompt
	}
	if len(slots) == 0 {
		return systemPrompt
	}

	validSlots := filterSlotsByBusinessHours(slots, clinicCfg.BusinessHours)
	var slotLines []string
	for i, s := range validSlots {
		if i >= 20 {
			break
		}
		slotLines = append(slotLines, fmt.Sprintf("- %s", s.StartAt.Format("Mon Jan 2 at 3:04 PM")))
	}
	if len(slotLines) == 0 {
		return systemPrompt
	}
	logger.Info("voice: pre-fetched Boulevard availability", "raw_slots", len(slots), "valid_slots", len(validSlots), "org_id", orgID, "times", strings.Join(slotLines, "; "))
	return systemPrompt + "\n\n=== AVAILABLE APPOINTMENT TIMES (FROM BOOKING SYSTEM) ===\n" +
		"THESE ARE THE ONLY TIMES THAT EXIST. There are NO other times.\n" +
		"You MUST pick from this list. Any time not on this list DOES NOT EXIST.\n\n" +
		strings.Join(slotLines, "\n") +
		"\n\n=== END OF AVAILABLE TIMES ===\n" +
		"STRICT RULE: If the caller wants times not on this list, say: " +
		"\"I don't have openings matching that right now. Would you like to hear what I do have available?\" " +
		"Then read times from the list above. NEVER say a time that is not on the list."
}

// filterSlotsByBusinessHours removes slots that fall on days the clinic
// is closed according to its configured business hours.
func filterSlotsByBusinessHours(slots []boulevard.TimeSlot, bh clinic.BusinessHours) []boulevard.TimeSlot {
	getDayHours := func(day time.Weekday) *clinic.DayHours {
		switch day {
		case time.Monday:
			return bh.Monday
		case time.Tuesday:
			return bh.Tuesday
		case time.Wednesday:
			return bh.Wednesday
		case time.Thursday:
			return bh.Thursday
		case time.Friday:
			return bh.Friday
		case time.Saturday:
			return bh.Saturday
		case time.Sunday:
			return bh.Sunday
		}
		return nil
	}
	var valid []boulevard.TimeSlot
	for _, s := range slots {
		if getDayHours(s.StartAt.Weekday()) != nil {
			valid = append(valid, s)
		}
	}
	return valid
}

// resolveClinicName returns the clinic display name for the given org,
// falling back to "our office" if unavailable.
func resolveClinicName(clinicStore *clinic.Store, orgID string) string {
	if clinicStore != nil {
		if cfg, err := clinicStore.Get(context.Background(), orgID); err == nil && cfg != nil && cfg.Name != "" {
			return cfg.Name
		}
	}
	return "our office"
}

// buildTranscriptCallback returns a function that persists voice transcript
// messages to the conversation store.
func buildTranscriptCallback(
	conversationStore *conversation.ConversationStore,
	conversationID string,
	callCtx voice.CallContext,
	logger *logging.Logger,
) func(role, text string) {
	return func(role, text string) {
		if conversationStore == nil || strings.TrimSpace(text) == "" {
			return
		}
		now := time.Now()
		from := callCtx.From
		to := callCtx.To
		status := "delivered"
		if role == "assistant" {
			from = callCtx.To
			to = callCtx.From
			status = "sent"
		}
		if err := conversationStore.AppendMessage(context.Background(), conversationID, conversation.SMSTranscriptMessage{
			Role:      role,
			From:      from,
			To:        to,
			Body:      text,
			Timestamp: now,
			Status:    status,
			Metadata:  map[string]string{"channel": "voice", "source": "nova-bridge"},
		}); err != nil {
			logger.Error("voice bridge: failed to persist transcript", "error", err, "conversation_id", conversationID)
		}
	}
}
