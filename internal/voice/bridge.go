package voice

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
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
	SidecarURL   string // e.g. "ws://localhost:3002/ws/nova-sonic"
	SystemPrompt string
	Voice        string
	OrgID        string
	CallerPhone  string // E.164 caller number
	CalledPhone  string // E.164 clinic number dialed by caller
	ClinicName   string // For ElevenLabs greeting
	Greeting     string // Custom greeting text (optional)
	Tools        []ToolDefinition
	ToolDeps     *ToolDeps
	Redis        *redis.Client // For payment confirmation pub/sub
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

	callControlID string
	orgID         string
	callerPhone   string
	calledPhone   string
	mediaFormat   TelnyxMediaFormat

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
	}

	// Create tool handler
	b.toolHandler = NewToolHandler(cfg.OrgID, cfg.CallerPhone, cfg.CalledPhone, cfg.ToolDeps, logger)

	// Connect to Nova Sonic sidecar
	sidecar, err := DialSidecar(SidecarConfig{URL: cfg.SidecarURL}, logger)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("connect to nova sonic sidecar: %w", err)
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
		return nil, fmt.Errorf("init nova sonic session: %w", err)
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

// SendAudioToNovaSonic forwards audio from Telnyx to Nova Sonic via the sidecar.
func (b *Bridge) SendAudioToNovaSonic(audio []byte) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return fmt.Errorf("bridge closed")
	}
	b.mu.Unlock()

	// Convert audio format if needed (Telnyx mulaw → LPCM)
	converted, err := b.convertInputAudio(audio)
	if err != nil {
		return fmt.Errorf("convert audio: %w", err)
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
				// Convert and forward audio to Telnyx
				output, err := b.convertOutputAudio(event.Audio)
				if err != nil {
					b.logger.Error("bridge: convert output audio", "error", err)
					continue
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

			case "tool_call":
				if event.ToolCall == nil {
					continue
				}
				// Execute tool and send result back via sidecar
				result := b.toolHandler.Handle(ctx, *event.ToolCall)
				if err := b.sidecar.SendToolResult(result.ToolUseID, result.Content); err != nil {
					b.logger.Error("bridge: send tool result", "error", err)
				}

			case "text":
				if !b.shouldProcessAssistantText(event.Text) {
					b.logger.Info("bridge: duplicate transcript suppressed", "text", event.Text)
					continue
				}
				b.logger.Info("bridge: transcript", "text", event.Text)
				b.maybeCaptureSlotSelection(event.Text)
				// Detect when Lauren mentions sending a deposit link — trigger SMS only after
				// an explicit slot selection (date+time) has been captured.
				// Nova Sonic tools are disabled (AWS limitation), so we fire SMS from Go side
				// when the assistant's transcript indicates deposit intent.
				b.maybeFireDepositSMS(ctx, event.Text)

			case "error":
				b.logger.Error("bridge: nova sonic error", "error", event.Text)
			}
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Audio format conversion
// ──────────────────────────────────────────────────────────────────────────────

func (b *Bridge) convertInputAudio(audio []byte) ([]byte, error) {
	switch b.mediaFormat.Encoding {
	case "audio/x-l16", "audio/x-linear16", "audio/lpcm", "L16", "l16":
		return audio, nil
	case "audio/x-mulaw":
		return mulawToLinear16(audio), nil
	default:
		b.logger.Warn("bridge: unknown input encoding, passing through", "encoding", b.mediaFormat.Encoding)
		return audio, nil
	}
}

func (b *Bridge) convertOutputAudio(audio []byte) ([]byte, error) {
	switch b.mediaFormat.Encoding {
	case "audio/x-l16", "audio/x-linear16", "audio/lpcm", "L16", "l16":
		return audio, nil
	case "audio/x-mulaw":
		return linear16ToMulaw(audio), nil
	default:
		return audio, nil
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Mu-law ↔ Linear16 conversion (ITU-T G.711)
// ──────────────────────────────────────────────────────────────────────────────

func mulawToLinear16(mulaw []byte) []byte {
	linear := make([]byte, len(mulaw)*2)
	for i, b := range mulaw {
		sample := mulawDecodeTable[b]
		linear[i*2] = byte(sample & 0xFF)
		linear[i*2+1] = byte((sample >> 8) & 0xFF)
	}
	return linear
}

func linear16ToMulaw(linear []byte) []byte {
	n := len(linear) / 2
	mulaw := make([]byte, n)
	for i := 0; i < n; i++ {
		sample := int16(linear[i*2]) | int16(linear[i*2+1])<<8
		mulaw[i] = linearToMulawSample(sample)
	}
	return mulaw
}

func linearToMulawSample(sample int16) byte {
	const (
		mulawMax  = 0x1FFF
		mulawBias = 33
	)

	sign := byte(0)
	if sample < 0 {
		sign = 0x80
		sample = -sample
	}

	if int(sample) > mulawMax {
		sample = mulawMax
	}
	sample += mulawBias

	exp := byte(7)
	for expMask := int16(0x4000); (sample & expMask) == 0; expMask >>= 1 {
		if exp == 0 {
			break
		}
		exp--
	}

	mantissa := byte((sample >> (exp + 3)) & 0x0F)
	encoded := ^(sign | (exp << 4) | mantissa)
	return encoded
}

var mulawDecodeTable = [256]int16{
	-32124, -31100, -30076, -29052, -28028, -27004, -25980, -24956,
	-23932, -22908, -21884, -20860, -19836, -18812, -17788, -16764,
	-15996, -15484, -14972, -14460, -13948, -13436, -12924, -12412,
	-11900, -11388, -10876, -10364, -9852, -9340, -8828, -8316,
	-7932, -7676, -7420, -7164, -6908, -6652, -6396, -6140,
	-5884, -5628, -5372, -5116, -4860, -4604, -4348, -4092,
	-3900, -3772, -3644, -3516, -3388, -3260, -3132, -3004,
	-2876, -2748, -2620, -2492, -2364, -2236, -2108, -1980,
	-1884, -1820, -1756, -1692, -1628, -1564, -1500, -1436,
	-1372, -1308, -1244, -1180, -1116, -1052, -988, -924,
	-876, -844, -812, -780, -748, -716, -684, -652,
	-620, -588, -556, -524, -492, -460, -428, -396,
	-372, -356, -340, -324, -308, -292, -276, -260,
	-244, -228, -212, -196, -180, -164, -148, -132,
	-120, -112, -104, -96, -88, -80, -72, -64,
	-56, -48, -40, -32, -24, -16, -8, 0,
	32124, 31100, 30076, 29052, 28028, 27004, 25980, 24956,
	23932, 22908, 21884, 20860, 19836, 18812, 17788, 16764,
	15996, 15484, 14972, 14460, 13948, 13436, 12924, 12412,
	11900, 11388, 10876, 10364, 9852, 9340, 8828, 8316,
	7932, 7676, 7420, 7164, 6908, 6652, 6396, 6140,
	5884, 5628, 5372, 5116, 4860, 4604, 4348, 4092,
	3900, 3772, 3644, 3516, 3388, 3260, 3132, 3004,
	2876, 2748, 2620, 2492, 2364, 2236, 2108, 1980,
	1884, 1820, 1756, 1692, 1628, 1564, 1500, 1436,
	1372, 1308, 1244, 1180, 1116, 1052, 988, 924,
	876, 844, 812, 780, 748, 716, 684, 652,
	620, 588, 556, 524, 492, 460, 428, 396,
	372, 356, 340, 324, 308, 292, 276, 260,
	244, 228, 212, 196, 180, 164, 148, 132,
	120, 112, 104, 96, 88, 80, 72, 64,
	56, 48, 40, 32, 24, 16, 8, 0,
}

// PaymentConfirmationChannel returns the Redis pub/sub channel name for a caller's payment.
func PaymentConfirmationChannel(callerPhone string) string {
	return "voice:payment:" + callerPhone
}

// listenForPaymentConfirmation subscribes to Redis for payment events during this call.
// When Stripe webhook processes a successful payment from this caller, it publishes
// to the channel, and we inject a confirmation message into Lauren's conversation.
func (b *Bridge) listenForPaymentConfirmation(ctx context.Context) {
	channel := PaymentConfirmationChannel(b.callerPhone)
	pubsub := b.redisClient.Subscribe(ctx, channel)
	defer pubsub.Close()

	b.logger.Info("bridge: listening for payment confirmation",
		"channel", channel, "caller", b.callerPhone)

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
				b.logger.Info("bridge: duplicate payment confirmation suppressed",
					"caller", b.callerPhone,
					"payload", msg.Payload,
				)
				continue
			}

			b.logger.Info("bridge: payment confirmation received!",
				"caller", b.callerPhone, "payload", msg.Payload)

			// Inject confirmation text into Lauren's conversation
			confirmText := fmt.Sprintf(
				"[SYSTEM: The patient's payment has been confirmed. Their deposit was successfully processed. " +
					"Tell them: 'I just got confirmation that your payment went through! You're all booked. " +
					"You'll receive a confirmation text shortly. Is there anything else I can help with?']")
			if err := b.sidecar.InjectText(confirmText); err != nil {
				b.logger.Error("bridge: failed to inject payment confirmation", "error", err)
			}
		}
	}
}

var (
	weekdayDateTimePattern = regexp.MustCompile(`(?i)\b(monday|tuesday|wednesday|thursday|friday|saturday|sunday)\b[^\n]{0,80}\b(\d{1,2}:\d{2}|\d{1,2})\s*(am|pm)\b`)
	monthDateTimePattern   = regexp.MustCompile(`(?i)\b(january|february|march|april|may|june|july|august|september|october|november|december)\b[^\n]{0,40}\b\d{1,2}(st|nd|rd|th)?\b[^\n]{0,40}\b(\d{1,2}:\d{2}|\d{1,2})\s*(am|pm)\b`)
)

// maybeCaptureSlotSelection records when Lauren explicitly confirms a date+time slot.
func (b *Bridge) maybeCaptureSlotSelection(text string) {
	if !looksLikeExplicitSlotSelection(text) {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.slotSelectionCaptured = true
}

func looksLikeExplicitSlotSelection(text string) bool {
	normalized := strings.ToLower(text)
	if !(strings.Contains(normalized, "works") || strings.Contains(normalized, "perfect") || strings.Contains(normalized, "great") || strings.Contains(normalized, "awesome") || strings.Contains(normalized, "book")) {
		return false
	}
	return weekdayDateTimePattern.MatchString(text) || monthDateTimePattern.MatchString(text)
}

// maybeFireDepositSMS checks if Lauren's transcript indicates she's sending a deposit link,
// and fires the actual SMS. This is the workaround for Nova Sonic tools being disabled.
func (b *Bridge) maybeFireDepositSMS(ctx context.Context, text string) {
	b.mu.Lock()
	if b.depositSMSSent {
		b.mu.Unlock()
		return
	}
	slotSelected := b.slotSelectionCaptured
	b.mu.Unlock()

	if !slotSelected {
		b.logger.Info("bridge: deposit intent ignored until slot is explicitly selected",
			"caller", b.callerPhone,
			"text", text,
		)
		return
	}

	lower := strings.ToLower(text)
	// Detect deposit link intent: Lauren says she'll text/send a deposit/payment link
	hasDeposit := strings.Contains(lower, "deposit") || strings.Contains(lower, "payment")
	hasSend := strings.Contains(lower, "text you") || strings.Contains(lower, "send you") || strings.Contains(lower, "sending")
	hasLink := strings.Contains(lower, "link") || strings.Contains(lower, "secure link") || strings.Contains(lower, "secure deposit")

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

	b.logger.Info("bridge: detected deposit SMS intent in transcript, firing SMS",
		"caller", b.callerPhone, "org_id", b.orgID, "text", text)

	// Fire SMS async so we don't block audio
	go func() {
		if err := b.toolHandler.SendDepositSMS(ctx, b.orgID, b.callerPhone); err != nil {
			b.logger.Error("bridge: deposit SMS failed", "error", err, "caller", b.callerPhone)
		} else {
			b.logger.Info("bridge: deposit SMS sent successfully", "caller", b.callerPhone)
		}
	}()
}

// shouldProcessAssistantText suppresses duplicate assistant transcripts that can arrive
// from sidecar retries/replays. It deduplicates normalized text within a short time window.
func (b *Bridge) shouldProcessAssistantText(text string) bool {
	normalized := strings.TrimSpace(strings.ToLower(text))
	if normalized == "" {
		return false
	}

	const dedupWindow = 30 * time.Second
	now := time.Now()

	b.mu.Lock()
	defer b.mu.Unlock()

	if prev, ok := b.recentAssistantText[normalized]; ok && now.Sub(prev) <= dedupWindow {
		return false
	}

	// opportunistic cleanup
	for k, ts := range b.recentAssistantText {
		if now.Sub(ts) > dedupWindow {
			delete(b.recentAssistantText, k)
		}
	}

	b.recentAssistantText[normalized] = now
	return true
}
