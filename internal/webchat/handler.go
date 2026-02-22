package webchat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
	"golang.org/x/net/websocket"
)

// Publisher enqueues conversation jobs.
type Publisher interface {
	EnqueueMessage(ctx context.Context, jobID string, req conversation.MessageRequest, opts ...conversation.PublishOption) error
	EnqueueStart(ctx context.Context, jobID string, req conversation.StartRequest, opts ...conversation.PublishOption) error
}

// TranscriptStore reads chat history.
type TranscriptStore interface {
	Append(ctx context.Context, conversationID string, msg conversation.SMSTranscriptMessage) error
	List(ctx context.Context, conversationID string, limit int64) ([]conversation.SMSTranscriptMessage, error)
}

// Handler manages web chat connections and messages.
type Handler struct {
	publisher  Publisher
	transcript TranscriptStore
	logger     *logging.Logger
	widgetJS   []byte

	mu       sync.RWMutex
	sessions map[string]*wsConn // conversationID -> active connection
}

type wsConn struct {
	conn *websocket.Conn
	done chan struct{}
}

// InboundMessage is what the widget sends.
type InboundMessage struct {
	Type      string `json:"type"` // "message", "ping"
	OrgID     string `json:"org_id"`
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
}

// OutboundMessage is what we send to the widget.
type OutboundMessage struct {
	Type      string `json:"type"` // "message", "typing", "history", "session", "error"
	Text      string `json:"text,omitempty"`
	Role      string `json:"role,omitempty"` // "assistant" or "user"
	SessionID string `json:"session_id,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	Messages  []HistoryMessage `json:"messages,omitempty"`
}

// HistoryMessage is a simplified message for history responses.
type HistoryMessage struct {
	Role      string `json:"role"`
	Text      string `json:"text"`
	Timestamp string `json:"timestamp"`
}

// NewHandler creates a web chat handler.
func NewHandler(publisher Publisher, transcript TranscriptStore, widgetJS []byte, logger *logging.Logger) *Handler {
	if logger == nil {
		logger = logging.Default()
	}
	return &Handler{
		publisher:  publisher,
		transcript: transcript,
		logger:     logger,
		widgetJS:   widgetJS,
		sessions:   make(map[string]*wsConn),
	}
}

// ConversationID builds the canonical conversation ID for a webchat session.
func ConversationID(orgID, sessionID string) string {
	return fmt.Sprintf("webchat:%s:%s", orgID, sessionID)
}

// generateSessionID creates a random session identifier.
func generateSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return uuid.New().String()
	}
	return hex.EncodeToString(b)
}

// HandleWebSocket upgrades to WebSocket and handles real-time messaging.
func (h *Handler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	websocket.Handler(func(conn *websocket.Conn) {
		h.serveWS(conn, r)
	}).ServeHTTP(w, r)
}

func (h *Handler) serveWS(conn *websocket.Conn, r *http.Request) {
	orgID := r.URL.Query().Get("org")
	if orgID == "" {
		_ = websocket.JSON.Send(conn, OutboundMessage{Type: "error", Text: "missing org parameter"})
		return
	}

	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		sessionID = generateSessionID()
	}

	convID := ConversationID(orgID, sessionID)

	// Send session info
	_ = websocket.JSON.Send(conn, OutboundMessage{
		Type:      "session",
		SessionID: sessionID,
	})

	// Send history if available
	if h.transcript != nil {
		if msgs, err := h.transcript.List(r.Context(), convID, 50); err == nil && len(msgs) > 0 {
			history := make([]HistoryMessage, 0, len(msgs))
			for _, m := range msgs {
				history = append(history, HistoryMessage{
					Role:      m.Role,
					Text:      m.Body,
					Timestamp: m.Timestamp.Format(time.RFC3339),
				})
			}
			_ = websocket.JSON.Send(conn, OutboundMessage{Type: "history", Messages: history})
		}
	}

	// Register connection
	wsc := &wsConn{conn: conn, done: make(chan struct{})}
	h.mu.Lock()
	h.sessions[convID] = wsc
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		if h.sessions[convID] == wsc {
			delete(h.sessions, convID)
		}
		h.mu.Unlock()
		close(wsc.done)
	}()

	h.logger.Info("webchat: connection opened", "org_id", orgID, "session_id", sessionID)

	for {
		var msg InboundMessage
		if err := websocket.JSON.Receive(conn, &msg); err != nil {
			h.logger.Debug("webchat: connection closed", "org_id", orgID, "error", err)
			return
		}

		if msg.Type == "ping" {
			_ = websocket.JSON.Send(conn, OutboundMessage{Type: "pong"})
			continue
		}

		if msg.Type != "message" || strings.TrimSpace(msg.Text) == "" {
			continue
		}

		h.processMessage(r.Context(), orgID, sessionID, msg.Text)
	}
}

func (h *Handler) processMessage(ctx context.Context, orgID, sessionID, text string) {
	convID := ConversationID(orgID, sessionID)
	jobID := uuid.New().String()

	// Store inbound message
	if h.transcript != nil {
		_ = h.transcript.Append(ctx, convID, conversation.SMSTranscriptMessage{
			ID:        uuid.New().String(),
			Role:      "user",
			From:      sessionID,
			To:        orgID,
			Body:      text,
			Timestamp: time.Now().UTC(),
			Kind:      "webchat_inbound",
		})
	}

	// Send typing indicator
	h.SendToSession(convID, OutboundMessage{Type: "typing"})

	req := conversation.MessageRequest{
		OrgID:          orgID,
		ConversationID: convID,
		Message:        text,
		Channel:        conversation.ChannelWebChat,
		From:           sessionID,
		To:             orgID,
	}

	if err := h.publisher.EnqueueMessage(ctx, jobID, req); err != nil {
		h.logger.Error("webchat: failed to enqueue message", "error", err, "org_id", orgID)
		h.SendToSession(convID, OutboundMessage{
			Type: "error",
			Text: "Sorry, something went wrong. Please try again.",
		})
	}
}

// SendToSession sends a message to an active WebSocket session.
func (h *Handler) SendToSession(convID string, msg OutboundMessage) {
	h.mu.RLock()
	wsc, ok := h.sessions[convID]
	h.mu.RUnlock()
	if !ok {
		return
	}
	_ = websocket.JSON.Send(wsc.conn, msg)
}

// HandleMessage is the HTTP fallback for sending messages.
func (h *Handler) HandleMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgID     string `json:"org_id"`
		SessionID string `json:"session_id"`
		Text      string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.OrgID == "" || req.Text == "" {
		http.Error(w, "org_id and text are required", http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		req.SessionID = generateSessionID()
	}

	h.processMessage(r.Context(), req.OrgID, req.SessionID, req.Text)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":     "queued",
		"session_id": req.SessionID,
	})
}

// HandleHistory returns chat history for a session.
func (h *Handler) HandleHistory(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org")
	sessionID := r.URL.Query().Get("session")
	if orgID == "" || sessionID == "" {
		http.Error(w, "org and session parameters required", http.StatusBadRequest)
		return
	}

	convID := ConversationID(orgID, sessionID)

	if h.transcript == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"messages": []HistoryMessage{}})
		return
	}

	msgs, err := h.transcript.List(r.Context(), convID, 100)
	if err != nil {
		h.logger.Error("webchat: failed to load history", "error", err)
		http.Error(w, "failed to load history", http.StatusInternalServerError)
		return
	}

	history := make([]HistoryMessage, 0, len(msgs))
	for _, m := range msgs {
		history = append(history, HistoryMessage{
			Role:      m.Role,
			Text:      m.Body,
			Timestamp: m.Timestamp.Format(time.RFC3339),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"messages": history})
}

// HandleWidgetJS serves the embeddable widget JavaScript.
func (h *Handler) HandleWidgetJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_, _ = w.Write(h.widgetJS)
}
