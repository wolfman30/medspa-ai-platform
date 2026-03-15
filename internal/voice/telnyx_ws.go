package voice

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// CallContext carries caller metadata decoded from Telnyx client_state.
type CallContext struct {
	From  string `json:"from"`
	To    string `json:"to"`
	OrgID string `json:"org_id"`
}

// ──────────────────────────────────────────────────────────────────────────────
// Telnyx WebSocket media stream handler.
//
// Telnyx forks call audio to a WebSocket endpoint. We receive audio in base64,
// decode it, forward to Nova Sonic, and send Nova Sonic audio back as base64.
//
// Telnyx media stream events:
//   - connected: WebSocket connection established
//   - start:     media stream started (includes metadata)
//   - media:     audio payload (base64-encoded)
//   - stop:      media stream ended (call hangup)
// ──────────────────────────────────────────────────────────────────────────────

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true }, // Telnyx connects from their infra
}

// TelnyxEvent represents a Telnyx WebSocket media streaming event.
type TelnyxEvent struct {
	Event    string            `json:"event"`
	Sequence json.Number       `json:"sequence_number,omitempty"`
	Media    *TelnyxMedia      `json:"media,omitempty"`
	Start    *TelnyxStart      `json:"start,omitempty"`
	Stop     *TelnyxStop       `json:"stop,omitempty"`
	StreamID string            `json:"stream_id,omitempty"`
	Extra    map[string]string `json:"-"` // unused fields
}

// TelnyxMedia carries the audio payload.
type TelnyxMedia struct {
	Track     string      `json:"track"`     // "inbound" or "outbound"
	Chunk     json.Number `json:"chunk"`     // sequential chunk number (string from Telnyx)
	Timestamp json.Number `json:"timestamp"` // relative timestamp (string from Telnyx)
	Payload   string      `json:"payload"`   // base64-encoded audio
}

// TelnyxStart carries stream metadata.
type TelnyxStart struct {
	StreamID      string            `json:"stream_id"`
	CallControlID string            `json:"call_control_id"`
	ClientState   string            `json:"client_state,omitempty"`
	MediaFormat   TelnyxMediaFormat `json:"media_format"`
	CustomHeaders map[string]string `json:"custom_headers,omitempty"`
}

// TelnyxMediaFormat describes the audio encoding.
type TelnyxMediaFormat struct {
	Encoding   string `json:"encoding"`    // "audio/x-mulaw" or "audio/x-l16"
	SampleRate int    `json:"sample_rate"` // typically 8000
	Channels   int    `json:"channels"`    // typically 1
}

// TelnyxStop carries stream termination info.
type TelnyxStop struct {
	CallControlID string `json:"call_control_id"`
	Reason        string `json:"reason,omitempty"`
}

// VoiceBridge is the interface that both Bridge (Nova Sonic) and ModularBridge
// (Deepgram→Claude→ElevenLabs) implement for Telnyx media streaming.
type VoiceBridge interface {
	SendAudio(audio []byte) error
	ReadAudioForTelnyx() ([]byte, bool)
	Close()
}

// TelnyxWSHandler handles the WebSocket connection from Telnyx.
type TelnyxWSHandler struct {
	logger        *slog.Logger
	bridgeFactory BridgeFactory
}

// BridgeFactory creates a new VoiceBridge for each incoming call.
type BridgeFactory func(logger *slog.Logger, callControlID string, mediaFormat TelnyxMediaFormat, callCtx CallContext) (VoiceBridge, error)

// NewTelnyxWSHandler creates the WebSocket handler.
func NewTelnyxWSHandler(logger *slog.Logger, factory BridgeFactory) *TelnyxWSHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &TelnyxWSHandler{
		logger:        logger,
		bridgeFactory: factory,
	}
}

// ServeHTTP upgrades the HTTP connection to WebSocket and handles the media stream.
func (h *TelnyxWSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("telnyx-ws: upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	h.logger.Info("telnyx-ws: connection established", "remote", r.RemoteAddr)

	session := &telnyxSession{
		conn:    conn,
		logger:  h.logger,
		factory: h.bridgeFactory,
	}
	session.run()
}

// telnyxSession manages a single Telnyx WebSocket connection.
type telnyxSession struct {
	conn       *websocket.Conn
	logger     *slog.Logger
	factory    BridgeFactory
	bridge     VoiceBridge
	mu         sync.Mutex
	mediaCount int
}

func (s *telnyxSession) run() {
	defer func() {
		s.mu.Lock()
		if s.bridge != nil {
			s.bridge.Close()
		}
		s.mu.Unlock()
	}()

	// Start goroutine to send audio back to Telnyx from the bridge
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		s.sendOutputAudio()
	}()

	// Read loop: receive events from Telnyx
	for {
		_, msg, err := s.conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				s.logger.Info("telnyx-ws: connection closed normally")
			} else {
				s.logger.Error("telnyx-ws: read error", "error", err)
			}
			return
		}

		var event TelnyxEvent
		if err := json.Unmarshal(msg, &event); err != nil {
			s.logger.Error("telnyx-ws: parse error", "error", err, "raw", string(msg))
			continue
		}

		// Debug: log all non-media events and first few media events
		if event.Event != "media" {
			s.logger.Info("telnyx-ws: event received", "event", event.Event, "stream_id", event.StreamID)
		} else {
			s.mediaCount++
			if s.mediaCount <= 3 || s.mediaCount%100 == 0 {
				payloadLen := 0
				if event.Media != nil {
					payloadLen = len(event.Media.Payload)
				}
				s.logger.Info("telnyx-ws: media event", "count", s.mediaCount, "payload_len", payloadLen, "track", event.Media.Track)
			}
		}

		switch event.Event {
		case "connected":
			s.logger.Info("telnyx-ws: connected event", "stream_id", event.StreamID)

		case "start":
			if err := s.handleStart(event); err != nil {
				s.logger.Error("telnyx-ws: start error", "error", err)
				return
			}

		case "media":
			if err := s.handleMedia(event); err != nil {
				s.logger.Error("telnyx-ws: media error", "error", err)
			}

		case "stop":
			s.logger.Info("telnyx-ws: stop event",
				"call_control_id", event.Stop.GetCallControlID(),
				"reason", event.Stop.GetReason(),
			)
			return

		default:
			s.logger.Debug("telnyx-ws: unknown event", "event", event.Event)
		}
	}
}

func (s *telnyxSession) handleStart(event TelnyxEvent) error {
	if event.Start == nil {
		return fmt.Errorf("start event missing start payload")
	}

	// Decode caller context from client_state (base64 JSON)
	var callCtx CallContext
	if event.Start.ClientState != "" {
		if decoded, err := base64.StdEncoding.DecodeString(event.Start.ClientState); err == nil {
			_ = json.Unmarshal(decoded, &callCtx)
		}
	}

	s.logger.Info("telnyx-ws: stream started",
		"stream_id", event.Start.StreamID,
		"call_control_id", event.Start.CallControlID,
		"encoding", event.Start.MediaFormat.Encoding,
		"sample_rate", event.Start.MediaFormat.SampleRate,
		"caller_from", callCtx.From,
		"caller_to", callCtx.To,
		"org_id", callCtx.OrgID,
	)

	bridge, err := s.factory(s.logger, event.Start.CallControlID, event.Start.MediaFormat, callCtx)
	if err != nil {
		return fmt.Errorf("create bridge: %w", err)
	}

	s.mu.Lock()
	s.bridge = bridge
	s.mu.Unlock()

	return nil
}

func (s *telnyxSession) handleMedia(event TelnyxEvent) error {
	s.mu.Lock()
	bridge := s.bridge
	s.mu.Unlock()

	if bridge == nil {
		return fmt.Errorf("media received before start")
	}

	if event.Media == nil {
		return nil
	}

	// Decode base64 audio
	audio, err := base64.StdEncoding.DecodeString(event.Media.Payload)
	if err != nil {
		return fmt.Errorf("decode audio: %w", err)
	}

	// Forward to bridge
	return bridge.SendAudio(audio)
}

// sendOutputAudio reads audio from the bridge and sends it back to Telnyx.
func (s *telnyxSession) sendOutputAudio() {
	// Wait for bridge to be established
	for {
		s.mu.Lock()
		bridge := s.bridge
		s.mu.Unlock()
		if bridge != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	s.mu.Lock()
	bridge := s.bridge
	s.mu.Unlock()

	outputChunks := 0
	for {
		audioData, ok := bridge.ReadAudioForTelnyx()
		if !ok {
			s.logger.Info("telnyx-ws: output audio channel closed", "total_chunks_sent", outputChunks)
			return
		}

		outputChunks++
		if outputChunks <= 3 || outputChunks%50 == 0 {
			s.logger.Info("telnyx-ws: sending output audio to Telnyx",
				"chunk", outputChunks,
				"bytes", len(audioData),
			)
		}

		// Encode to base64 and send as Telnyx media event
		payload := base64.StdEncoding.EncodeToString(audioData)
		outEvent := map[string]interface{}{
			"event": "media",
			"media": map[string]interface{}{
				"payload": payload,
			},
		}

		s.mu.Lock()
		err := s.conn.WriteJSON(outEvent)
		s.mu.Unlock()

		if err != nil {
			s.logger.Error("telnyx-ws: write error", "error", err, "chunks_sent", outputChunks)
			return
		}
	}
}

// Helper methods for nil-safe access on TelnyxStop
func (ts *TelnyxStop) GetCallControlID() string {
	if ts == nil {
		return ""
	}
	return ts.CallControlID
}

func (ts *TelnyxStop) GetReason() string {
	if ts == nil {
		return ""
	}
	return ts.Reason
}
