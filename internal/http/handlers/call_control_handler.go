package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// CallControlHandler handles Telnyx Call Control webhook events.
// When a call comes in, it answers and starts media streaming to the
// Nova Sonic sidecar via WebSocket.
type CallControlHandler struct {
	logger       *logging.Logger
	telnyxAPIKey string
	streamURL    string // e.g. "wss://api-dev.aiwolfsolutions.com/ws/voice"
}

// CallControlConfig configures the handler.
type CallControlConfig struct {
	Logger       *logging.Logger
	TelnyxAPIKey string
	StreamURL    string
}

// NewCallControlHandler creates a new Call Control webhook handler.
func NewCallControlHandler(cfg CallControlConfig) *CallControlHandler {
	if cfg.Logger == nil {
		cfg.Logger = logging.Default()
	}
	return &CallControlHandler{
		logger:       cfg.Logger,
		telnyxAPIKey: cfg.TelnyxAPIKey,
		streamURL:    cfg.StreamURL,
	}
}

// callControlEvent represents a Telnyx Call Control webhook event.
type callControlEvent struct {
	Data struct {
		ID        string `json:"id"`
		EventType string `json:"event_type"`
		Payload   struct {
			CallControlID string `json:"call_control_id"`
			CallLegID     string `json:"call_leg_id"`
			CallSessionID string `json:"call_session_id"`
			ConnectionID  string `json:"connection_id"`
			From          string `json:"from"`
			To            string `json:"to"`
			Direction     string `json:"direction"`
			State         string `json:"state"`
			StreamURL     string `json:"stream_url,omitempty"`
		} `json:"payload"`
	} `json:"data"`
}

// HandleCallControl processes Call Control webhook events.
func (h *CallControlHandler) HandleCallControl(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		h.logger.Error("call-control: read body failed", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var event callControlEvent
	if err := json.Unmarshal(body, &event); err != nil {
		h.logger.Error("call-control: parse failed", "error", err, "body", string(body))
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	eventType := event.Data.EventType
	callControlID := event.Data.Payload.CallControlID
	from := event.Data.Payload.From
	to := event.Data.Payload.To

	h.logger.Info("call-control: event received",
		"event_type", eventType,
		"call_control_id", callControlID,
		"from", from,
		"to", to,
	)

	switch eventType {
	case "call.initiated":
		if event.Data.Payload.Direction == "incoming" {
			h.answerCall(callControlID)
		}

	case "call.answered":
		// Call answered — start media streaming
		h.startStreaming(callControlID)

	case "streaming.started":
		h.logger.Info("call-control: media streaming started",
			"call_control_id", callControlID,
		)

	case "streaming.stopped":
		h.logger.Info("call-control: media streaming stopped",
			"call_control_id", callControlID,
		)

	case "call.hangup":
		h.logger.Info("call-control: call ended",
			"call_control_id", callControlID,
			"from", from,
			"to", to,
		)

	default:
		h.logger.Debug("call-control: unhandled event", "event_type", eventType)
	}

	// Always return 200 to acknowledge the webhook
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"ok"}`)
}

// answerCall sends the answer command to Telnyx.
func (h *CallControlHandler) answerCall(callControlID string) {
	h.logger.Info("call-control: answering call", "call_control_id", callControlID)

	payload := map[string]interface{}{
		"client_state": "nova_sonic",
	}
	h.sendCallControlCommand(callControlID, "answer", payload)
}

// startStreaming tells Telnyx to start media streaming to our WebSocket.
func (h *CallControlHandler) startStreaming(callControlID string) {
	h.logger.Info("call-control: starting media stream",
		"call_control_id", callControlID,
		"stream_url", h.streamURL,
	)

	payload := map[string]interface{}{
		"stream_url":        h.streamURL,
		"stream_track":      "both_tracks",
		"enable_dialogflow": false,
	}
	h.sendCallControlCommand(callControlID, "streaming_start", payload)
}

// sendCallControlCommand sends a command to the Telnyx Call Control API.
func (h *CallControlHandler) sendCallControlCommand(callControlID, command string, payload map[string]interface{}) {
	url := fmt.Sprintf("https://api.telnyx.com/v2/calls/%s/actions/%s",
		strings.TrimSpace(callControlID), command)

	body, err := json.Marshal(payload)
	if err != nil {
		h.logger.Error("call-control: marshal failed", "error", err, "command", command)
		return
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		h.logger.Error("call-control: request creation failed", "error", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+h.telnyxAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.logger.Error("call-control: request failed", "error", err, "command", command)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 300 {
		h.logger.Error("call-control: command failed",
			"command", command,
			"status", resp.StatusCode,
			"response", string(respBody),
			"call_control_id", callControlID,
		)
		return
	}

	h.logger.Info("call-control: command sent",
		"command", command,
		"status", resp.StatusCode,
		"call_control_id", callControlID,
	)
}
