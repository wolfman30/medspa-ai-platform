// Package voice implements the Nova Sonic voice AI bridge.
package voice

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ──────────────────────────────────────────────────────────────────────────────
// SidecarClient connects to the Node.js Nova Sonic sidecar via WebSocket.
// It replaces the Go NovaSonicSession stub by proxying audio and events
// through the sidecar, which has the real Bedrock bidirectional stream.
// ──────────────────────────────────────────────────────────────────────────────

const (
	sidecarDialTimeout  = 5 * time.Second
	sidecarWriteTimeout = 2 * time.Second
	sidecarPongWait     = 30 * time.Second
	sidecarPingInterval = 15 * time.Second
)

// SidecarConfig configures the connection to the Node.js sidecar.
type SidecarConfig struct {
	// URL is the WebSocket endpoint, e.g. "ws://localhost:3002/ws/nova-sonic"
	URL string
}

// sidecarInbound is a message FROM the sidecar (Nova Sonic → Go).
type sidecarInbound struct {
	Type       string                 `json:"type"` // audio, tool_call, transcript, error, session_renewed
	Data       string                 `json:"data,omitempty"`
	ToolCallID string                 `json:"toolCallId,omitempty"`
	ToolName   string                 `json:"toolName,omitempty"`
	Input      map[string]interface{} `json:"input,omitempty"`
	Role       string                 `json:"role,omitempty"`
	Text       string                 `json:"text,omitempty"`
	Message    string                 `json:"message,omitempty"`
}

// sidecarOutbound is a message TO the sidecar (Go → Nova Sonic).
type sidecarOutbound struct {
	Type       string             `json:"type"` // init, audio, tool_result, close
	Data       string             `json:"data,omitempty"`
	ToolCallID string             `json:"toolCallId,omitempty"`
	Result     string             `json:"result,omitempty"`
	Config     *sidecarInitConfig `json:"config,omitempty"`
}

type sidecarInitConfig struct {
	SystemPrompt string           `json:"systemPrompt"`
	Tools        []ToolDefinition `json:"tools,omitempty"`
	Voice        string           `json:"voice,omitempty"`
	OrgID        string           `json:"orgId,omitempty"`
	CallerPhone  string           `json:"callerPhone,omitempty"`
}

// SidecarClient manages a WebSocket connection to the Nova Sonic sidecar.
type SidecarClient struct {
	conn   *websocket.Conn
	logger *slog.Logger

	// OutputEvents delivers events from Nova Sonic to the bridge.
	OutputEvents chan NovaSonicEvent

	mu     sync.Mutex
	closed bool
}

// DialSidecar connects to the Nova Sonic sidecar.
func DialSidecar(cfg SidecarConfig, logger *slog.Logger) (*SidecarClient, error) {
	if logger == nil {
		logger = slog.Default()
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: sidecarDialTimeout,
	}

	conn, _, err := dialer.Dial(cfg.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("dial sidecar at %s: %w", cfg.URL, err)
	}

	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(sidecarPongWait))
	})

	sc := &SidecarClient{
		conn:         conn,
		logger:       logger,
		OutputEvents: make(chan NovaSonicEvent, 64),
	}

	// Start reading from sidecar
	go sc.readLoop()
	// Start ping keepalive
	go sc.pingLoop()

	logger.Info("sidecar-client: connected", "url", cfg.URL)
	return sc, nil
}

// Init sends the session initialization message to the sidecar.
func (sc *SidecarClient) Init(systemPrompt string, tools []ToolDefinition, voice, orgID, callerPhone string) error {
	return sc.send(sidecarOutbound{
		Type: "init",
		Config: &sidecarInitConfig{
			SystemPrompt: systemPrompt,
			Tools:        tools,
			Voice:        voice,
			OrgID:        orgID,
			CallerPhone:  callerPhone,
		},
	})
}

// SendAudio sends base64-encoded PCM audio to the sidecar.
func (sc *SidecarClient) SendAudio(pcmAudio []byte) error {
	b64 := base64.StdEncoding.EncodeToString(pcmAudio)
	return sc.send(sidecarOutbound{
		Type: "audio",
		Data: b64,
	})
}

// SendToolResult sends a tool execution result back through the sidecar.
func (sc *SidecarClient) SendToolResult(toolCallID, result string) error {
	return sc.send(sidecarOutbound{
		Type:       "tool_result",
		ToolCallID: toolCallID,
		Result:     result,
	})
}

// Close gracefully shuts down the sidecar connection.
func (sc *SidecarClient) Close() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if sc.closed {
		return
	}
	sc.closed = true

	// Send close message
	_ = sc.conn.WriteJSON(sidecarOutbound{Type: "close"})
	sc.conn.Close()
	close(sc.OutputEvents)
	sc.logger.Info("sidecar-client: closed")
}

func (sc *SidecarClient) send(msg sidecarOutbound) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if sc.closed {
		return fmt.Errorf("sidecar client closed")
	}

	_ = sc.conn.SetWriteDeadline(time.Now().Add(sidecarWriteTimeout))
	return sc.conn.WriteJSON(msg)
}

func (sc *SidecarClient) readLoop() {
	defer func() {
		sc.mu.Lock()
		if !sc.closed {
			sc.closed = true
			close(sc.OutputEvents)
		}
		sc.mu.Unlock()
	}()

	for {
		_, msg, err := sc.conn.ReadMessage()
		if err != nil {
			if !sc.isClosed() {
				sc.logger.Error("sidecar-client: read error", "error", err)
			}
			return
		}

		var inbound sidecarInbound
		if err := json.Unmarshal(msg, &inbound); err != nil {
			sc.logger.Error("sidecar-client: parse error", "error", err, "raw", string(msg))
			continue
		}

		event := sc.toNovaSonicEvent(inbound)
		if event != nil {
			select {
			case sc.OutputEvents <- *event:
			default:
				sc.logger.Warn("sidecar-client: output buffer full, dropping event", "type", event.Type)
			}
		}
	}
}

func (sc *SidecarClient) toNovaSonicEvent(in sidecarInbound) *NovaSonicEvent {
	switch in.Type {
	case "audio":
		audio, err := base64.StdEncoding.DecodeString(in.Data)
		if err != nil {
			sc.logger.Error("sidecar-client: decode audio", "error", err)
			return nil
		}
		return &NovaSonicEvent{Type: "audio", Audio: audio}

	case "tool_call":
		inputJSON, _ := json.Marshal(in.Input)
		return &NovaSonicEvent{
			Type: "tool_call",
			ToolCall: &ToolCall{
				ToolUseID: in.ToolCallID,
				Name:      in.ToolName,
				Input:     inputJSON,
			},
		}

	case "transcript":
		return &NovaSonicEvent{Type: "text", Text: fmt.Sprintf("[%s] %s", in.Role, in.Text)}

	case "error":
		return &NovaSonicEvent{Type: "error", Text: in.Message}

	case "session_renewed":
		return &NovaSonicEvent{Type: "session_end", Text: "renewed"}

	default:
		sc.logger.Debug("sidecar-client: unknown event type", "type", in.Type)
		return nil
	}
}

func (sc *SidecarClient) pingLoop() {
	ticker := time.NewTicker(sidecarPingInterval)
	defer ticker.Stop()

	for range ticker.C {
		sc.mu.Lock()
		if sc.closed {
			sc.mu.Unlock()
			return
		}
		_ = sc.conn.SetWriteDeadline(time.Now().Add(sidecarWriteTimeout))
		err := sc.conn.WriteMessage(websocket.PingMessage, nil)
		sc.mu.Unlock()

		if err != nil {
			return
		}
	}
}

func (sc *SidecarClient) isClosed() bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.closed
}
