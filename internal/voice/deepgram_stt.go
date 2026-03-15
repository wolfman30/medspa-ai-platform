package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ──────────────────────────────────────────────────────────────────────────────
// Deepgram STT — real-time streaming speech-to-text via WebSocket.
// ──────────────────────────────────────────────────────────────────────────────

const (
	deepgramWSURL        = "wss://api.deepgram.com/v1/listen"
	deepgramDialTimeout  = 5 * time.Second
	deepgramWriteTimeout = 2 * time.Second
)

// DeepgramConfig configures the Deepgram STT client.
type DeepgramConfig struct {
	APIKey     string
	Model      string // e.g. "nova-2-medical"
	SampleRate int
	Encoding   string // "linear16" or "mulaw"
	Channels   int
}

// DeepgramTranscript represents a transcription result.
type DeepgramTranscript struct {
	Text       string
	Confidence float64
	IsFinal    bool
}

// DeepgramSTT streams audio to Deepgram and emits transcripts.
type DeepgramSTT struct {
	conn   *websocket.Conn
	logger *slog.Logger

	// Transcripts delivers transcription results.
	Transcripts chan DeepgramTranscript

	mu     sync.Mutex
	closed bool
}

// deepgramResponse represents a Deepgram WebSocket response.
type deepgramResponse struct {
	Type    string `json:"type"`
	Channel struct {
		Alternatives []struct {
			Transcript string  `json:"transcript"`
			Confidence float64 `json:"confidence"`
		} `json:"alternatives"`
	} `json:"channel"`
	IsFinal     bool `json:"is_final"`
	SpeechFinal bool `json:"speech_final"`
}

// NewDeepgramSTT connects to Deepgram's streaming API.
func NewDeepgramSTT(ctx context.Context, cfg DeepgramConfig, logger *slog.Logger) (*DeepgramSTT, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("deepgram: API key required")
	}
	if logger == nil {
		logger = slog.Default()
	}

	url := fmt.Sprintf(
		"%s?model=%s&sample_rate=%d&encoding=%s&channels=%d&punctuate=true&interim_results=true&endpointing=300&vad_events=true&smart_format=true",
		deepgramWSURL, cfg.Model, cfg.SampleRate, cfg.Encoding, cfg.Channels,
	)

	dialer := websocket.Dialer{HandshakeTimeout: deepgramDialTimeout}
	headers := make(map[string][]string)
	headers["Authorization"] = []string{"Token " + cfg.APIKey}

	conn, _, err := dialer.DialContext(ctx, url, headers)
	if err != nil {
		return nil, fmt.Errorf("deepgram: dial: %w", err)
	}

	d := &DeepgramSTT{
		conn:        conn,
		logger:      logger,
		Transcripts: make(chan DeepgramTranscript, 32),
	}

	go d.readLoop()

	logger.Info("deepgram-stt: connected", "model", cfg.Model, "sample_rate", cfg.SampleRate)
	return d, nil
}

// SendAudio sends raw audio bytes to Deepgram for transcription.
func (d *DeepgramSTT) SendAudio(audio []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return fmt.Errorf("deepgram: closed")
	}
	_ = d.conn.SetWriteDeadline(time.Now().Add(deepgramWriteTimeout))
	return d.conn.WriteMessage(websocket.BinaryMessage, audio)
}

// Close terminates the Deepgram connection.
func (d *DeepgramSTT) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	d.closed = true
	_ = d.conn.WriteJSON(map[string]string{"type": "CloseStream"})
	d.conn.Close()
}

func (d *DeepgramSTT) readLoop() {
	defer func() {
		close(d.Transcripts)
		d.mu.Lock()
		if !d.closed {
			d.closed = true
			d.conn.Close()
		}
		d.mu.Unlock()
	}()

	for {
		_, msg, err := d.conn.ReadMessage()
		if err != nil {
			d.mu.Lock()
			closed := d.closed
			d.mu.Unlock()
			if !closed {
				d.logger.Error("deepgram-stt: read error", "error", err)
			}
			return
		}

		var resp deepgramResponse
		if err := json.Unmarshal(msg, &resp); err != nil {
			d.logger.Error("deepgram-stt: parse error", "error", err)
			continue
		}

		if resp.Type != "Results" {
			continue
		}

		if len(resp.Channel.Alternatives) == 0 {
			continue
		}

		alt := resp.Channel.Alternatives[0]
		if alt.Transcript == "" {
			continue
		}

		transcript := DeepgramTranscript{
			Text:       alt.Transcript,
			Confidence: alt.Confidence,
			IsFinal:    resp.IsFinal || resp.SpeechFinal,
		}

		select {
		case d.Transcripts <- transcript:
		default:
			d.logger.Warn("deepgram-stt: transcript buffer full")
		}
	}
}
