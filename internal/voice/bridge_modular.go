package voice

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// ──────────────────────────────────────────────────────────────────────────────
// ModularBridge connects a Telnyx WebSocket media stream to the modular
// Deepgram STT → Claude LLM → ElevenLabs TTS pipeline.
// One ModularBridge per active phone call.
// ──────────────────────────────────────────────────────────────────────────────

const (
	modularMaxCallDuration = 10 * time.Minute
	modularCallWarningTime = 8 * time.Minute
)

// ModularBridgeConfig holds configuration for the modular bridge.
type ModularBridgeConfig struct {
	DeepgramAPIKey   string
	ElevenLabsAPIKey string
	SystemPrompt     string
	OrgID            string
	CallerPhone      string
	CalledPhone      string
	ClinicName       string
	Greeting         string
	Tools            []ToolDefinition
	ToolDeps         *ToolDeps
	Redis            *redis.Client
	OnTranscript     func(role, text string)
	ConversationID   string
}

// ModularBridge manages the bidirectional audio flow between Telnyx and the
// Deepgram→Claude→ElevenLabs pipeline.
type ModularBridge struct {
	logger      *slog.Logger
	toolHandler *ToolHandler
	redisClient *redis.Client

	deepgram   *DeepgramSTT
	claude     *ClaudeLLM
	elevenLabs *ElevenLabsClient

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
	ctx          context.Context
	started      time.Time
	outputChunks int
	warningTimer *time.Timer
	maxTimer     *time.Timer

	depositSMSSent               bool
	slotSelectionCaptured        bool
	paymentConfirmed             bool
	paymentConfirmationAnnounced bool
	recentAssistantText          map[string]time.Time
}

// NewModularBridge creates a modular bridge for a single call.
func NewModularBridge(ctx context.Context, cfg ModularBridgeConfig, callControlID string, mediaFormat TelnyxMediaFormat, logger *slog.Logger) (*ModularBridge, error) {
	if logger == nil {
		logger = slog.Default()
	}

	ctx, cancel := context.WithCancel(ctx)

	b := &ModularBridge{
		logger:              logger,
		callControlID:       callControlID,
		orgID:               cfg.OrgID,
		callerPhone:         cfg.CallerPhone,
		calledPhone:         cfg.CalledPhone,
		mediaFormat:         mediaFormat,
		telnyxOutput:        make(chan []byte, 128),
		cancelFn:            cancel,
		ctx:                 ctx,
		started:             time.Now(),
		redisClient:         cfg.Redis,
		recentAssistantText: make(map[string]time.Time),
		conversationID:      cfg.ConversationID,
		onTranscript:        cfg.OnTranscript,
	}

	b.toolHandler = NewToolHandler(cfg.OrgID, cfg.CallerPhone, cfg.CalledPhone, cfg.ToolDeps, logger)
	if b.conversationID == "" {
		b.conversationID = fmt.Sprintf("voice:%s:%s", cfg.OrgID, strings.TrimPrefix(cfg.CallerPhone, "+"))
	}

	tools := cfg.Tools
	if tools == nil {
		tools = DefaultTools()
	}

	// Initialize Deepgram STT
	dgCfg := DeepgramConfig{
		APIKey:     cfg.DeepgramAPIKey,
		Model:      "nova-2-medical",
		SampleRate: mediaFormat.SampleRate,
		Encoding:   deepgramEncoding(mediaFormat.Encoding),
		Channels:   1,
	}
	dg, err := NewDeepgramSTT(ctx, dgCfg, logger)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("NewModularBridge: deepgram: %w", err)
	}
	b.deepgram = dg

	// Initialize Claude LLM
	claudeCfg := ClaudeLLMConfig{
		ModelID:      "us.anthropic.claude-sonnet-4-6-20250514",
		SystemPrompt: cfg.SystemPrompt,
		Tools:        tools,
		MaxTokens:    1024,
	}
	claude, err := NewClaudeLLM(ctx, claudeCfg, logger)
	if err != nil {
		cancel()
		dg.Close()
		return nil, fmt.Errorf("NewModularBridge: claude: %w", err)
	}
	b.claude = claude

	// Initialize ElevenLabs TTS
	elCfg := ElevenLabsConfig{
		APIKey:           cfg.ElevenLabsAPIKey,
		VoiceID:          DefaultVoiceID,
		ModelID:          "eleven_turbo_v2_5",
		Stability:        DefaultStability,
		SimilarityBoost:  DefaultSimilarity,
		Speed:            DefaultSpeed,
		OutputSampleRate: 8000,
	}
	el, err := NewElevenLabsClient(elCfg, logger)
	if err != nil {
		cancel()
		dg.Close()
		return nil, fmt.Errorf("NewModularBridge: elevenlabs: %w", err)
	}
	b.elevenLabs = el

	// Start processing Deepgram transcripts
	go b.processTranscripts(ctx)

	// Payment confirmation listener
	if b.redisClient != nil {
		go b.modularListenForPaymentConfirmation(ctx)
	}

	// Duration limits
	b.warningTimer = time.AfterFunc(modularCallWarningTime, func() {
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			return
		}
		b.mu.Unlock()
		b.logger.Warn("modular-bridge: call approaching max duration")
		b.injectSystemMessage("I want to make sure I'm not keeping you too long. Is there anything else I can help with?")
	})
	b.maxTimer = time.AfterFunc(modularMaxCallDuration, func() {
		b.logger.Warn("modular-bridge: max call duration reached, hanging up")
		b.injectSystemMessage("Thank you for calling! Our team can help with anything else — have a great day!")
		time.Sleep(3 * time.Second)
		b.Close()
	})

	logger.Info("modular-bridge: created",
		"call_control_id", callControlID,
		"org_id", cfg.OrgID,
		"caller", cfg.CallerPhone,
		"encoding", mediaFormat.Encoding,
	)

	return b, nil
}

// SendAudio forwards audio from Telnyx to Deepgram STT.
func (b *ModularBridge) SendAudio(audio []byte) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return fmt.Errorf("bridge closed")
	}
	b.mu.Unlock()

	return b.deepgram.SendAudio(audio)
}

// ReadAudioForTelnyx returns the next audio chunk to send to Telnyx.
func (b *ModularBridge) ReadAudioForTelnyx() ([]byte, bool) {
	audio, ok := <-b.telnyxOutput
	return audio, ok
}

// Close terminates the bridge and all resources.
func (b *ModularBridge) Close() {
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
	b.deepgram.Close()
	close(b.telnyxOutput)

	b.logger.Info("modular-bridge: closed",
		"call_control_id", b.callControlID,
		"duration", time.Since(b.started).String(),
	)
}

// processTranscripts reads Deepgram transcripts and routes them through Claude → ElevenLabs.
func (b *ModularBridge) processTranscripts(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case transcript, ok := <-b.deepgram.Transcripts:
			if !ok {
				return
			}
			if transcript.IsFinal && strings.TrimSpace(transcript.Text) != "" {
				b.logger.Info("modular-bridge: user transcript",
					"text", transcript.Text,
					"confidence", transcript.Confidence,
				)
				if b.onTranscript != nil {
					b.onTranscript("user", transcript.Text)
				}
				go b.handleUserInput(ctx, transcript.Text)
			}
		}
	}
}

// handleUserInput sends user text to Claude and streams the response via ElevenLabs.
func (b *ModularBridge) handleUserInput(ctx context.Context, text string) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()

	// Send to Claude and get response (may include tool calls)
	response, err := b.claude.SendMessage(ctx, "user", text)
	if err != nil {
		b.logger.Error("modular-bridge: claude error", "error", err)
		return
	}

	// Process response — handle tool calls in a loop
	for {
		var textParts []string
		var toolUses []ClaudeToolUse

		for _, block := range response.Content {
			switch block.Type {
			case "text":
				textParts = append(textParts, block.Text)
			case "tool_use":
				toolUses = append(toolUses, ClaudeToolUse{
					ID:    block.ID,
					Name:  block.Name,
					Input: block.Input,
				})
			}
		}

		// Synthesize any text response
		fullText := strings.Join(textParts, " ")
		if fullText != "" {
			b.logger.Info("modular-bridge: assistant response", "text", fullText)
			if b.onTranscript != nil {
				b.onTranscript("assistant", fullText)
			}
			b.modularMaybeCaptureSlotSelection(fullText)
			b.modularMaybeFireDepositSMS(ctx, fullText)

			if err := b.synthesizeAndStream(ctx, fullText); err != nil {
				b.logger.Error("modular-bridge: tts error", "error", err)
			}
		}

		if len(toolUses) == 0 {
			break
		}

		// Execute tool calls and feed results back to Claude
		var toolResults []ClaudeToolResultBlock
		for _, tu := range toolUses {
			b.logger.Info("modular-bridge: tool call", "tool", tu.Name, "id", tu.ID)
			result := b.toolHandler.Handle(ctx, ToolCall{
				ToolUseID: tu.ID,
				Name:      tu.Name,
				Input:     tu.Input,
			})
			toolResults = append(toolResults, ClaudeToolResultBlock{
				ToolUseID: tu.ID,
				Content:   result.Content,
				IsError:   result.IsError,
			})
		}

		response, err = b.claude.SendToolResults(ctx, response.Content, toolResults)
		if err != nil {
			b.logger.Error("modular-bridge: claude tool follow-up error", "error", err)
			return
		}
	}
}

// synthesizeAndStream sends text to ElevenLabs and streams audio back to Telnyx.
func (b *ModularBridge) synthesizeAndStream(ctx context.Context, text string) error {
	session, err := b.elevenLabs.StreamTTS(ctx)
	if err != nil {
		return fmt.Errorf("synthesizeAndStream: open session: %w", err)
	}

	if err := session.SendText(text); err != nil {
		session.Close()
		return fmt.Errorf("synthesizeAndStream: send text: %w", err)
	}
	if err := session.Flush(); err != nil {
		session.Close()
		return fmt.Errorf("synthesizeAndStream: flush: %w", err)
	}

	for audioChunk := range session.AudioOutput {
		output, err := b.convertModularOutputAudio(audioChunk)
		if err != nil {
			b.logger.Error("modular-bridge: convert output audio", "error", err)
			continue
		}
		b.outputChunks++
		select {
		case b.telnyxOutput <- output:
		default:
			b.logger.Warn("modular-bridge: telnyx output buffer full, dropping audio")
		}
	}

	return nil
}

// injectSystemMessage sends a system-initiated message through Claude → TTS.
func (b *ModularBridge) injectSystemMessage(text string) {
	ctx := b.ctx
	response, err := b.claude.SendMessage(ctx, "user", fmt.Sprintf("[SYSTEM: Say this to the caller: %s]", text))
	if err != nil {
		b.logger.Error("modular-bridge: inject message error", "error", err)
		return
	}
	for _, block := range response.Content {
		if block.Type == "text" && block.Text != "" {
			if err := b.synthesizeAndStream(ctx, block.Text); err != nil {
				b.logger.Error("modular-bridge: inject tts error", "error", err)
			}
		}
	}
}

// Audio conversion helpers (reuses same mu-law logic as Bridge).
func (b *ModularBridge) convertModularOutputAudio(audio []byte) ([]byte, error) {
	switch b.mediaFormat.Encoding {
	case "audio/x-l16", "audio/x-linear16", "audio/lpcm", "L16", "l16":
		return audio, nil
	case "audio/x-mulaw":
		return linear16ToMulaw(audio), nil
	default:
		return audio, nil
	}
}

// modularListenForPaymentConfirmation subscribes to Redis for payment events.
func (b *ModularBridge) modularListenForPaymentConfirmation(ctx context.Context) {
	channel := PaymentConfirmationChannel(b.callerPhone)
	pubsub := b.redisClient.Subscribe(ctx, channel)
	defer pubsub.Close()

	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}

			b.mu.Lock()
			alreadyAnnounced := b.paymentConfirmationAnnounced
			if !alreadyAnnounced {
				b.paymentConfirmed = true
				b.paymentConfirmationAnnounced = true
			}
			b.mu.Unlock()

			if alreadyAnnounced {
				continue
			}

			b.logger.Info("modular-bridge: payment confirmation received",
				"caller", b.callerPhone, "payload", msg.Payload)

			b.injectSystemMessage("I just got confirmation that your payment went through! You're all booked. You'll receive a confirmation text shortly. Is there anything else I can help with?")
		}
	}
}

func (b *ModularBridge) modularMaybeCaptureSlotSelection(text string) {
	if !looksLikeExplicitSlotSelection(text) {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.slotSelectionCaptured = true
}

func (b *ModularBridge) modularMaybeFireDepositSMS(ctx context.Context, text string) {
	b.mu.Lock()
	if b.depositSMSSent || !b.slotSelectionCaptured {
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()

	lower := strings.ToLower(text)
	hasDeposit := strings.Contains(lower, "deposit") || strings.Contains(lower, "payment")
	hasSend := strings.Contains(lower, "text you") || strings.Contains(lower, "send you") || strings.Contains(lower, "sending")
	hasLink := strings.Contains(lower, "link") || strings.Contains(lower, "secure link")

	if !(hasDeposit && hasSend) && !(hasDeposit && hasLink) {
		return
	}

	b.mu.Lock()
	if b.depositSMSSent {
		b.mu.Unlock()
		return
	}
	b.depositSMSSent = true
	b.mu.Unlock()

	go func() {
		if err := b.toolHandler.SendDepositSMS(ctx, b.orgID, b.callerPhone); err != nil {
			b.logger.Error("modular-bridge: deposit SMS failed", "error", err)
		}
	}()
}

// deepgramEncoding maps Telnyx encoding to Deepgram encoding string.
func deepgramEncoding(telnyxEncoding string) string {
	switch telnyxEncoding {
	case "audio/x-mulaw":
		return "mulaw"
	case "audio/x-l16", "audio/x-linear16", "audio/lpcm", "L16", "l16":
		return "linear16"
	default:
		return "linear16"
	}
}

// GetVoiceEngine returns the configured voice engine from VOICE_ENGINE env var.
func GetVoiceEngine() string {
	engine := os.Getenv("VOICE_ENGINE")
	if engine == "" {
		return "nova-sonic"
	}
	return engine
}
