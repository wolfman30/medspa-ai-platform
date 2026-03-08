package voice

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ──────────────────────────────────────────────────────────────────────────────
// ElevenLabs streaming TTS client via WebSocket.
//
// Accepts text chunks and returns PCM audio suitable for Telnyx (L16, 8kHz).
// Uses the input-streaming WebSocket API for low-latency speech synthesis.
// ──────────────────────────────────────────────────────────────────────────────

const (
	elevenLabsWSURLTemplate = "wss://api.elevenlabs.io/v1/text-to-speech/%s/stream-input"
	elevenLabsDialTimeout   = 5 * time.Second
	elevenLabsWriteTimeout  = 3 * time.Second
	elevenLabsReadTimeout   = 30 * time.Second

	DefaultVoiceID    = "l4Coq6695JDX9xtLqXDE" // Lauren B
	DefaultModelID    = "eleven_v3"
	DefaultStability  = 0.5 // "Natural" — balanced, closest to original voice
	DefaultSimilarity = 0.75
	DefaultSpeed      = 1.0
)

// ElevenLabsConfig configures the ElevenLabs TTS client.
type ElevenLabsConfig struct {
	APIKey           string
	VoiceID          string  // default: Lauren B
	ModelID          string  // default: eleven_flash_v2_5
	Stability        float64 // 0.0–1.0
	SimilarityBoost  float64 // 0.0–1.0
	Speed            float64 // 0.5–2.0
	OutputSampleRate int     // target sample rate (default 8000 for Telnyx)
}

func (c *ElevenLabsConfig) applyDefaults() {
	if c.VoiceID == "" {
		c.VoiceID = DefaultVoiceID
	}
	if c.ModelID == "" {
		c.ModelID = DefaultModelID
	}
	if c.Stability == 0 {
		c.Stability = DefaultStability
	}
	if c.SimilarityBoost == 0 {
		c.SimilarityBoost = DefaultSimilarity
	}
	if c.Speed == 0 {
		c.Speed = DefaultSpeed
	}
	if c.OutputSampleRate == 0 {
		c.OutputSampleRate = 8000
	}
}

// ElevenLabsClient streams text-to-speech via the ElevenLabs WebSocket API.
type ElevenLabsClient struct {
	cfg    ElevenLabsConfig
	logger *slog.Logger
}

// NewElevenLabsClient creates a new ElevenLabs TTS client.
func NewElevenLabsClient(cfg ElevenLabsConfig, logger *slog.Logger) (*ElevenLabsClient, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("elevenlabs: API key is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	cfg.applyDefaults()
	return &ElevenLabsClient{cfg: cfg, logger: logger}, nil
}

// ElevenLabsSession represents a single streaming TTS session (one utterance).
type ElevenLabsSession struct {
	client *ElevenLabsClient
	conn   *websocket.Conn
	logger *slog.Logger

	// AudioOutput receives PCM audio chunks (L16, target sample rate).
	AudioOutput chan []byte

	mu       sync.Mutex
	closed   bool
	cancelFn context.CancelFunc
	done     chan struct{}
}

// elevenLabsBOSMessage is the Beginning-of-Stream message.
type elevenLabsBOSMessage struct {
	Text             string                   `json:"text"`
	VoiceSettings    *elevenLabsVoiceSettings `json:"voice_settings"`
	GenerationConfig *elevenLabsGenerationCfg `json:"generation_config,omitempty"`
	XIAPIKey         string                   `json:"xi_api_key"`
}

type elevenLabsVoiceSettings struct {
	Stability       float64 `json:"stability"`
	SimilarityBoost float64 `json:"similarity_boost"`
	Speed           float64 `json:"speed,omitempty"`
}

type elevenLabsGenerationCfg struct {
	ChunkLengthSchedule []int `json:"chunk_length_schedule,omitempty"`
}

// elevenLabsTextMessage sends a text chunk for synthesis.
type elevenLabsTextMessage struct {
	Text string `json:"text"`
}

// elevenLabsEOSMessage is the End-of-Stream flush signal.
type elevenLabsEOSMessage struct {
	Text string `json:"text"`
}

// elevenLabsResponse is a response from the ElevenLabs WebSocket.
type elevenLabsResponse struct {
	Audio               *string          `json:"audio"` // base64 MP3 or PCM
	IsFinal             bool             `json:"isFinal"`
	NormalizedAlignment *json.RawMessage `json:"normalizedAlignment,omitempty"`
	Error               *struct {
		Message string `json:"message"`
		Code    string `json:"code,omitempty"`
	} `json:"error,omitempty"`
}

// StreamTTS opens a WebSocket session, ready to accept text chunks.
// Call SendText() to feed text, then Flush() when done. Audio arrives on AudioOutput.
func (c *ElevenLabsClient) StreamTTS(ctx context.Context) (*ElevenLabsSession, error) {
	ctx, cancel := context.WithCancel(ctx)

	wsURL := fmt.Sprintf(elevenLabsWSURLTemplate, c.cfg.VoiceID)
	// Add query params for output format: pcm at desired sample rate
	wsURL += fmt.Sprintf("?model_id=%s&output_format=pcm_%d", c.cfg.ModelID, c.cfg.OutputSampleRate)

	dialer := websocket.Dialer{HandshakeTimeout: elevenLabsDialTimeout}
	headers := http.Header{}
	headers.Set("xi-api-key", c.cfg.APIKey)
	conn, _, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("elevenlabs: dial %s: %w", wsURL, err)
	}

	s := &ElevenLabsSession{
		client:      c,
		conn:        conn,
		logger:      c.logger,
		AudioOutput: make(chan []byte, 128),
		cancelFn:    cancel,
		done:        make(chan struct{}),
	}

	// Send BOS (beginning of stream) with voice settings
	bos := elevenLabsBOSMessage{
		Text:     " ", // space signals BOS
		XIAPIKey: c.cfg.APIKey,
		VoiceSettings: &elevenLabsVoiceSettings{
			Stability:       c.cfg.Stability,
			SimilarityBoost: c.cfg.SimilarityBoost,
			Speed:           c.cfg.Speed,
		},
		GenerationConfig: &elevenLabsGenerationCfg{
			ChunkLengthSchedule: []int{120, 160, 250, 290},
		},
	}

	if err := s.writeJSON(bos); err != nil {
		conn.Close()
		cancel()
		return nil, fmt.Errorf("elevenlabs: send BOS: %w", err)
	}

	// Start reading audio responses
	go s.readLoop(ctx)

	c.logger.Info("elevenlabs: session started",
		"voice_id", c.cfg.VoiceID,
		"model_id", c.cfg.ModelID,
		"sample_rate", c.cfg.OutputSampleRate,
	)

	return s, nil
}

// SendText sends a text chunk for synthesis. Text is streamed incrementally.
// The client accumulates text and generates audio as it arrives.
func (s *ElevenLabsSession) SendText(text string) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("elevenlabs: session closed")
	}
	s.mu.Unlock()

	// Normalize text for better TTS quality
	text = NormalizeTextForTTS(text)

	msg := elevenLabsTextMessage{Text: text}
	return s.writeJSON(msg)
}

// Flush signals end-of-input and waits for all audio to be generated.
// After calling Flush, no more text can be sent. AudioOutput will be closed
// when all audio has been received.
func (s *ElevenLabsSession) Flush() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("elevenlabs: session closed")
	}
	s.mu.Unlock()

	// Send EOS (empty string signals flush)
	eos := elevenLabsEOSMessage{Text: ""}
	return s.writeJSON(eos)
}

// Close terminates the session immediately.
func (s *ElevenLabsSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	s.cancelFn()
	s.conn.Close()
	s.logger.Info("elevenlabs: session closed")
}

// Done returns a channel that's closed when the session finishes (all audio received).
func (s *ElevenLabsSession) Done() <-chan struct{} {
	return s.done
}

func (s *ElevenLabsSession) writeJSON(v interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("session closed")
	}
	_ = s.conn.SetWriteDeadline(time.Now().Add(elevenLabsWriteTimeout))
	return s.conn.WriteJSON(v)
}

func (s *ElevenLabsSession) readLoop(ctx context.Context) {
	defer func() {
		close(s.AudioOutput)
		close(s.done)
		s.mu.Lock()
		if !s.closed {
			s.closed = true
			s.conn.Close()
		}
		s.mu.Unlock()
	}()

	chunks := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, msg, err := s.conn.ReadMessage()
		if err != nil {
			if !s.isClosed() {
				s.logger.Error("elevenlabs: read error", "error", err)
			}
			return
		}

		var resp elevenLabsResponse
		if err := json.Unmarshal(msg, &resp); err != nil {
			s.logger.Error("elevenlabs: parse response", "error", err)
			continue
		}

		if resp.Error != nil {
			s.logger.Error("elevenlabs: API error",
				"message", resp.Error.Message,
				"code", resp.Error.Code,
			)
			return
		}

		if resp.Audio != nil && *resp.Audio != "" {
			audioBytes, err := base64.StdEncoding.DecodeString(*resp.Audio)
			if err != nil {
				s.logger.Error("elevenlabs: decode audio", "error", err)
				continue
			}

			// ElevenLabs pcm_8000 output is 16-bit signed LE mono — exactly L16.
			// No resampling needed when output_format=pcm_8000.
			chunks++
			if chunks <= 3 || chunks%50 == 0 {
				s.logger.Info("elevenlabs: audio chunk",
					"chunk", chunks,
					"bytes", len(audioBytes),
				)
			}

			select {
			case s.AudioOutput <- audioBytes:
			case <-ctx.Done():
				return
			}
		}

		if resp.IsFinal {
			s.logger.Info("elevenlabs: stream complete", "total_chunks", chunks)
			return
		}
	}
}

func (s *ElevenLabsSession) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// ──────────────────────────────────────────────────────────────────────────────
// Text normalization for TTS
// ──────────────────────────────────────────────────────────────────────────────

var (
	dollarPattern  = regexp.MustCompile(`\$(\d+(?:,\d{3})*(?:\.\d{2})?)`)
	timePattern    = regexp.MustCompile(`(\d{1,2}):(\d{2})\s*(AM|PM|am|pm)`)
	percentPattern = regexp.MustCompile(`(\d+(?:\.\d+)?)%`)
)

// NormalizeTextForTTS converts symbols and abbreviations to speakable text.
func NormalizeTextForTTS(text string) string {
	// $50 → "50 dollars", $1 → "1 dollar"
	text = dollarPattern.ReplaceAllStringFunc(text, func(match string) string {
		amount := match[1:] // strip $
		amount = strings.ReplaceAll(amount, ",", "")
		if amount == "1" || amount == "1.00" {
			return amount + " dollar"
		}
		return amount + " dollars"
	})

	// 3:00 PM → "3 PM", 3:30 PM → "3 30 PM"
	text = timePattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := timePattern.FindStringSubmatch(match)
		if parts == nil {
			return match
		}
		hour, min, ampm := parts[1], parts[2], strings.ToUpper(parts[3])
		if min == "00" {
			return hour + " " + ampm
		}
		return hour + " " + min + " " + ampm
	})

	// 50% → "50 percent"
	text = percentPattern.ReplaceAllString(text, "${1} percent")

	return text
}

// ──────────────────────────────────────────────────────────────────────────────
// Audio resampling utilities (for non-8kHz ElevenLabs output)
// ──────────────────────────────────────────────────────────────────────────────

// ResampleLinear16 resamples 16-bit signed LE PCM from srcRate to dstRate
// using linear interpolation.
func ResampleLinear16(pcm []byte, srcRate, dstRate int) []byte {
	if srcRate == dstRate {
		return pcm
	}

	numSamples := len(pcm) / 2
	if numSamples == 0 {
		return pcm
	}

	ratio := float64(srcRate) / float64(dstRate)
	outLen := int(math.Ceil(float64(numSamples) / ratio))
	out := make([]byte, outLen*2)

	for i := 0; i < outLen; i++ {
		srcPos := float64(i) * ratio
		idx := int(srcPos)
		frac := srcPos - float64(idx)

		s0 := int16(binary.LittleEndian.Uint16(pcm[idx*2:]))
		var s1 int16
		if idx+1 < numSamples {
			s1 = int16(binary.LittleEndian.Uint16(pcm[(idx+1)*2:]))
		} else {
			s1 = s0
		}

		sample := int16(float64(s0)*(1-frac) + float64(s1)*frac)
		binary.LittleEndian.PutUint16(out[i*2:], uint16(sample))
	}

	return out
}
