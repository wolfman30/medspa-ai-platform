package voice

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// ──────────────────────────────────────────────────────────────────────────────
// Bridge connects a Telnyx WebSocket media stream to Nova Sonic via the
// Node.js sidecar. One Bridge per active phone call.
// ──────────────────────────────────────────────────────────────────────────────

// BridgeConfig holds configuration for creating a bridge.
type BridgeConfig struct {
	SidecarURL     string // e.g. "ws://localhost:3002/ws/nova-sonic"
	SystemPrompt   string
	Voice          string
	OrgID          string
	CallerPhone    string // E.164 caller number
	CalledPhone    string // E.164 clinic number dialed by caller
	ClinicName     string // For ElevenLabs greeting
	Greeting       string // Custom greeting text (optional)
	Tools          []ToolDefinition
	ToolDeps       *ToolDeps
	Redis          *redis.Client // For payment confirmation pub/sub
	OnTranscript   func(role, text string)
	ConversationID string // Optional explicit conversation ID for transcript persistence
}

const (
	// maxCallDuration is the hard limit for voice calls (OWASP LLM10).
	maxCallDuration = 10 * time.Minute
	// callWarningTime is when we inject a courtesy warning.
	callWarningTime = 8 * time.Minute
)

// Bridge manages the bidirectional audio flow between Telnyx and Nova Sonic.
type Bridge struct {
	logger      *slog.Logger
	sidecar     *SidecarClient
	toolHandler *ToolHandler
	redisClient *redis.Client

	// telnyxOutput is the channel for audio going back to Telnyx
	telnyxOutput chan []byte

	callControlID  string
	orgID          string
	callerPhone    string
	calledPhone    string
	mediaFormat    TelnyxMediaFormat
	conversationID string
	onTranscript   func(role, text string)

	mu           sync.Mutex
	closed       bool
	cancelFn     context.CancelFunc
	started      time.Time
	outputChunks int
	warningTimer *time.Timer
	maxTimer     *time.Timer

	// depositSMSSent tracks whether we've already sent the deposit SMS for this call.
	// Set to true after we fire the SMS so we don't send duplicates.
	depositSMSSent bool
	// slotSelectionCaptured is set once Lauren explicitly confirms a date+time slot.
	// Deposit SMS is gated on this to prevent premature payment prompts.
	slotSelectionCaptured bool
	// availabilityFetched tracks whether we've fetched service-specific availability.
	availabilityFetched bool

	// paymentConfirmed tracks whether payment was confirmed during this call.
	paymentConfirmed bool
	// paymentConfirmationAnnounced ensures we only inject payment confirmation once per call.
	paymentConfirmationAnnounced bool
	// recentAssistantText stores normalized transcript snippets to suppress duplicate repeats.
	recentAssistantText map[string]time.Time
}

// NewBridge creates a bridge for a single call, connecting to the Nova Sonic sidecar.
func NewBridge(ctx context.Context, cfg BridgeConfig, callControlID string, mediaFormat TelnyxMediaFormat, logger *slog.Logger) (*Bridge, error) {
	if logger == nil {
		logger = slog.Default()
	}

	ctx, cancel := context.WithCancel(ctx)

	b := &Bridge{
		logger:              logger,
		callControlID:       callControlID,
		orgID:               cfg.OrgID,
		callerPhone:         cfg.CallerPhone,
		calledPhone:         cfg.CalledPhone,
		mediaFormat:         mediaFormat,
		telnyxOutput:        make(chan []byte, 128),
		cancelFn:            cancel,
		started:             time.Now(),
		redisClient:         cfg.Redis,
		recentAssistantText: make(map[string]time.Time),
		conversationID:      cfg.ConversationID,
		onTranscript:        cfg.OnTranscript,
	}

	// Create tool handler
	b.toolHandler = NewToolHandler(cfg.OrgID, cfg.CallerPhone, cfg.CalledPhone, cfg.ToolDeps, logger)
	if b.conversationID == "" {
		b.conversationID = fmt.Sprintf("voice:%s:%s", cfg.OrgID, strings.TrimPrefix(cfg.CallerPhone, "+"))
	}

	// Connect to Nova Sonic sidecar
	sidecar, err := DialSidecar(SidecarConfig{URL: cfg.SidecarURL}, logger)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("NewBridge: connect to nova sonic sidecar: %w", err)
	}
	b.sidecar = sidecar

	// Initialize the Nova Sonic session via the sidecar
	tools := cfg.Tools
	if tools == nil {
		tools = DefaultTools()
	}
	if err := sidecar.Init(cfg.SystemPrompt, tools, cfg.Voice, cfg.OrgID, cfg.CallerPhone, cfg.ClinicName, cfg.Greeting); err != nil {
		cancel()
		sidecar.Close()
		return nil, fmt.Errorf("NewBridge: init nova sonic session: %w", err)
	}

	// Start goroutine to process sidecar output events
	go b.processSidecarOutput(ctx)

	// Start goroutine to listen for payment confirmations via Redis pub/sub
	if b.redisClient != nil {
		go b.listenForPaymentConfirmation(ctx)
	}

	// Voice call duration limits (OWASP LLM10: unbounded consumption).
	b.warningTimer = time.AfterFunc(callWarningTime, func() {
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			return
		}
		b.mu.Unlock()
		b.logger.Warn("bridge: call approaching max duration, injecting warning",
			"call_control_id", callControlID,
			"elapsed", callWarningTime,
		)
		_ = b.sidecar.InjectText("I want to make sure I'm not keeping you too long. Is there anything else I can help with?")
	})
	b.maxTimer = time.AfterFunc(maxCallDuration, func() {
		b.logger.Warn("bridge: max call duration reached, hanging up",
			"call_control_id", callControlID,
			"max_duration", maxCallDuration,
		)
		_ = b.sidecar.InjectText("Thank you for calling! Our team can help with anything else — have a great day!")
		// Give a moment for the farewell to be spoken before closing.
		time.Sleep(3 * time.Second)
		b.Close()
	})

	logger.Info("bridge: created",
		"call_control_id", callControlID,
		"org_id", cfg.OrgID,
		"caller", cfg.CallerPhone,
		"encoding", mediaFormat.Encoding,
		"sample_rate", mediaFormat.SampleRate,
		"sidecar_url", cfg.SidecarURL,
	)

	return b, nil
}

// SendAudio forwards audio from Telnyx to Nova Sonic via the sidecar.
// Implements VoiceBridge.
func (b *Bridge) SendAudio(audio []byte) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return fmt.Errorf("bridge closed")
	}
	b.mu.Unlock()

	// Convert audio format if needed (Telnyx mulaw → LPCM)
	converted, err := b.convertInputAudio(audio)
	if err != nil {
		return fmt.Errorf("SendAudioToNovaSonic: convert audio: %w", err)
	}

	return b.sidecar.SendAudio(converted)
}

// ReadAudioForTelnyx returns the next audio chunk to send to Telnyx.
// Returns (nil, false) when the bridge is closed.
func (b *Bridge) ReadAudioForTelnyx() ([]byte, bool) {
	audio, ok := <-b.telnyxOutput
	return audio, ok
}

// Close terminates the bridge and all associated resources.
func (b *Bridge) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true

	if b.warningTimer != nil {
		b.warningTimer.Stop()
	}
	if b.maxTimer != nil {
		b.maxTimer.Stop()
	}
	b.cancelFn()
	b.sidecar.Close()
	close(b.telnyxOutput)

	b.logger.Info("bridge: closed",
		"call_control_id", b.callControlID,
		"duration", time.Since(b.started).String(),
	)
}

// processSidecarOutput reads events from the sidecar and routes them.
func (b *Bridge) processSidecarOutput(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-b.sidecar.OutputEvents:
			if !ok {
				return
			}

			switch event.Type {
			case "audio":
				b.handleAudioEvent(event)
			case "tool_call":
				b.handleToolCallEvent(ctx, event)
			case "text":
				b.handleTextEvent(ctx, event)
			case "error":
				b.logger.Error("bridge: nova sonic error", "error", event.Text)
			}
		}
	}
}

// handleAudioEvent converts and forwards audio to Telnyx.
func (b *Bridge) handleAudioEvent(event NovaSonicEvent) {
	output, err := b.convertOutputAudio(event.Audio)
	if err != nil {
		b.logger.Error("bridge: convert output audio", "error", err)
		return
	}
	b.outputChunks++
	if b.outputChunks <= 3 || b.outputChunks%50 == 0 {
		b.logger.Info("bridge: forwarding audio to Telnyx",
			"chunk", b.outputChunks,
			"input_bytes", len(event.Audio),
			"output_bytes", len(output),
		)
	}
	select {
	case b.telnyxOutput <- output:
	default:
		b.logger.Warn("bridge: telnyx output buffer full, dropping audio")
	}
}

// handleToolCallEvent executes a tool call and sends the result back via sidecar.
func (b *Bridge) handleToolCallEvent(ctx context.Context, event NovaSonicEvent) {
	if event.ToolCall == nil {
		return
	}
	result := b.toolHandler.Handle(ctx, *event.ToolCall)
	if err := b.sidecar.SendToolResult(result.ToolUseID, result.Content); err != nil {
		b.logger.Error("bridge: send tool result", "error", err)
	}
}

// handleTextEvent processes transcript text, fires deposit SMS if needed, and
// captures slot selections.
func (b *Bridge) handleTextEvent(ctx context.Context, event NovaSonicEvent) {
	if !b.shouldProcessAssistantText(event.Text) {
		b.logger.Info("bridge: duplicate transcript suppressed", "text", event.Text)
		return
	}
	b.logger.Info("bridge: transcript", "text", event.Text)
	role, cleanText := parseTranscriptRoleAndText(event.Text)
	if b.onTranscript != nil && cleanText != "" {
		b.onTranscript(role, cleanText)
	}
	b.maybeCaptureSlotSelection(event.Text)
	b.maybeFetchAvailability(ctx, event.Text)
	// Detect when Lauren mentions sending a deposit link — trigger SMS only after
	// an explicit slot selection (date+time) has been captured.
	// Nova Sonic tools are disabled (AWS limitation), so we fire SMS from Go side
	// when the assistant's transcript indicates deposit intent.
	b.maybeFireDepositSMS(ctx, event.Text)
}
