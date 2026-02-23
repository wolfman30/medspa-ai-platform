package voice

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// ──────────────────────────────────────────────────────────────────────────────
// Bridge connects a Telnyx WebSocket media stream to a Nova Sonic session.
// One Bridge per active phone call.
// ──────────────────────────────────────────────────────────────────────────────

// BridgeConfig holds configuration for creating a bridge.
type BridgeConfig struct {
	AWSConfig    aws.Config
	SystemPrompt string
	Voice        string
	OrgID        string
	CallerPhone  string // E.164
}

// Bridge manages the bidirectional audio flow between Telnyx and Nova Sonic.
type Bridge struct {
	logger      *slog.Logger
	novaSonic   *NovaSonicSession
	toolHandler *ToolHandler

	// telnyxOutput is the channel for audio going back to Telnyx
	telnyxOutput chan []byte

	callControlID string
	orgID         string
	callerPhone   string
	mediaFormat   TelnyxMediaFormat

	mu       sync.Mutex
	closed   bool
	cancelFn context.CancelFunc

	// Session renewal
	sessionCount int
	started      time.Time
}

// NewBridge creates a bridge for a single call.
func NewBridge(ctx context.Context, cfg BridgeConfig, callControlID string, mediaFormat TelnyxMediaFormat, logger *slog.Logger) (*Bridge, error) {
	if logger == nil {
		logger = slog.Default()
	}

	ctx, cancel := context.WithCancel(ctx)

	b := &Bridge{
		logger:        logger,
		callControlID: callControlID,
		orgID:         cfg.OrgID,
		callerPhone:   cfg.CallerPhone,
		mediaFormat:   mediaFormat,
		telnyxOutput:  make(chan []byte, 128),
		cancelFn:      cancel,
		sessionCount:  1,
		started:       time.Now(),
	}

	// Create tool handler
	b.toolHandler = NewToolHandler(cfg.OrgID, cfg.CallerPhone, logger)

	// Create Nova Sonic session
	novaSonicCfg := NovaSonicConfig{
		SystemPrompt:     cfg.SystemPrompt,
		Voice:            cfg.Voice,
		InputSampleRate:  mediaFormat.SampleRate,
		OutputSampleRate: mediaFormat.SampleRate,
		Tools:            DefaultTools(),
	}

	b.novaSonic = NewNovaSonicSession(cfg.AWSConfig, novaSonicCfg, logger)

	// Start the Nova Sonic session
	if err := b.novaSonic.Start(ctx); err != nil {
		cancel()
		return nil, fmt.Errorf("start nova sonic: %w", err)
	}

	// Start goroutine to process Nova Sonic output events
	go b.processNovaSonicOutput(ctx)

	logger.Info("bridge: created",
		"call_control_id", callControlID,
		"org_id", cfg.OrgID,
		"caller", cfg.CallerPhone,
		"encoding", mediaFormat.Encoding,
		"sample_rate", mediaFormat.SampleRate,
	)

	return b, nil
}

// SendAudioToNovaSonic forwards audio from Telnyx to Nova Sonic.
// Handles audio format conversion if needed.
func (b *Bridge) SendAudioToNovaSonic(audio []byte) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return fmt.Errorf("bridge closed")
	}
	b.mu.Unlock()

	// Convert audio format if needed
	converted, err := b.convertInputAudio(audio)
	if err != nil {
		return fmt.Errorf("convert audio: %w", err)
	}

	return b.novaSonic.SendAudio(converted)
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

	b.cancelFn()
	b.novaSonic.Close()
	close(b.telnyxOutput)

	b.logger.Info("bridge: closed",
		"call_control_id", b.callControlID,
		"duration", time.Since(b.started).String(),
		"sessions", b.sessionCount,
	)
}

// processNovaSonicOutput reads events from Nova Sonic and routes them.
func (b *Bridge) processNovaSonicOutput(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-b.novaSonic.OutputEvents:
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
				select {
				case b.telnyxOutput <- output:
				default:
					b.logger.Warn("bridge: telnyx output buffer full, dropping audio")
				}

			case "tool_call":
				if event.ToolCall == nil {
					continue
				}
				// Execute tool and send result back to Nova Sonic
				result := b.toolHandler.Handle(ctx, *event.ToolCall)
				if err := b.novaSonic.SendToolResult(result); err != nil {
					b.logger.Error("bridge: send tool result", "error", err)
				}

			case "text":
				b.logger.Info("bridge: transcript", "text", event.Text)

			case "session_end":
				if event.Text == "renewal_needed" {
					b.handleSessionRenewal(ctx)
				}

			case "error":
				b.logger.Error("bridge: nova sonic error", "error", event.Text)
			}
		}
	}
}

// handleSessionRenewal creates a new Nova Sonic session to handle the
// 8-minute connection limit. The old session is closed and a new one
// is started seamlessly.
func (b *Bridge) handleSessionRenewal(ctx context.Context) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	b.logger.Info("bridge: renewing nova sonic session",
		"session_number", b.sessionCount,
		"elapsed", time.Since(b.started).String(),
	)

	// Close old session
	b.novaSonic.Close()

	// Create new session with same config
	newCfg := NovaSonicConfig{
		SystemPrompt:     b.novaSonic.cfg.SystemPrompt,
		Voice:            b.novaSonic.cfg.Voice,
		InputSampleRate:  b.novaSonic.cfg.InputSampleRate,
		OutputSampleRate: b.novaSonic.cfg.OutputSampleRate,
		Tools:            b.novaSonic.cfg.Tools,
	}

	b.novaSonic = NewNovaSonicSession(aws.Config{}, newCfg, b.logger)
	b.sessionCount++

	if err := b.novaSonic.Start(ctx); err != nil {
		b.logger.Error("bridge: session renewal failed", "error", err)
		return
	}

	// Restart the output processor for the new session
	go b.processNovaSonicOutput(ctx)

	b.logger.Info("bridge: session renewed successfully",
		"session_number", b.sessionCount,
	)
}

// ──────────────────────────────────────────────────────────────────────────────
// Audio format conversion
// ──────────────────────────────────────────────────────────────────────────────

// convertInputAudio converts Telnyx audio to Nova Sonic format.
// Telnyx can send mulaw or linear16; Nova Sonic wants LPCM.
func (b *Bridge) convertInputAudio(audio []byte) ([]byte, error) {
	switch b.mediaFormat.Encoding {
	case "audio/x-l16", "audio/x-linear16", "audio/lpcm":
		// Already LPCM — pass through
		return audio, nil
	case "audio/x-mulaw":
		// Convert mu-law to 16-bit linear PCM
		return mulawToLinear16(audio), nil
	default:
		// Unknown format — pass through and hope for the best
		b.logger.Warn("bridge: unknown input encoding, passing through",
			"encoding", b.mediaFormat.Encoding,
		)
		return audio, nil
	}
}

// convertOutputAudio converts Nova Sonic audio to Telnyx format.
func (b *Bridge) convertOutputAudio(audio []byte) ([]byte, error) {
	switch b.mediaFormat.Encoding {
	case "audio/x-l16", "audio/x-linear16", "audio/lpcm":
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

// mulawToLinear16 converts mu-law encoded bytes to 16-bit signed LE PCM.
func mulawToLinear16(mulaw []byte) []byte {
	linear := make([]byte, len(mulaw)*2)
	for i, b := range mulaw {
		sample := mulawDecodeTable[b]
		linear[i*2] = byte(sample & 0xFF)
		linear[i*2+1] = byte((sample >> 8) & 0xFF)
	}
	return linear
}

// linear16ToMulaw converts 16-bit signed LE PCM to mu-law.
func linear16ToMulaw(linear []byte) []byte {
	n := len(linear) / 2
	mulaw := make([]byte, n)
	for i := 0; i < n; i++ {
		sample := int16(linear[i*2]) | int16(linear[i*2+1])<<8
		mulaw[i] = linearToMulawSample(sample)
	}
	return mulaw
}

// linearToMulawSample encodes a single 16-bit sample as mu-law.
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

// mulawDecodeTable is the standard mu-law to linear PCM decode table.
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
