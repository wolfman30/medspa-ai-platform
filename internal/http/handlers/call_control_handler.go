package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// CallControlHandler handles Telnyx Call Control webhook events.
// When a call comes in, it answers and starts media streaming to the
// Nova Sonic sidecar via WebSocket.
type CallControlHandler struct {
	logger       *logging.Logger
	telnyxAPIKey string
	streamURL    string // e.g. "wss://api-dev.aiwolfsolutions.com/ws/voice"
	orgResolver  messaging.OrgResolver
}

// CallControlConfig configures the handler.
type CallControlConfig struct {
	Logger       *logging.Logger
	TelnyxAPIKey string
	StreamURL    string
	OrgResolver  messaging.OrgResolver
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
		orgResolver:  cfg.OrgResolver,
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
			ClientState   string `json:"client_state,omitempty"`
		} `json:"payload"`
	} `json:"data"`
}

// extractCallerContext decodes from/to from the client_state passed through speak commands.
func (h *CallControlHandler) extractCallerContext(event callControlEvent) (string, string) {
	cs := event.Data.Payload.ClientState
	if cs == "" {
		return "", ""
	}
	decoded, err := base64.StdEncoding.DecodeString(cs)
	if err != nil {
		h.logger.Warn("call-control: decode client_state failed", "error", err)
		return "", ""
	}
	var ctx map[string]string
	if err := json.Unmarshal(decoded, &ctx); err != nil {
		return "", ""
	}
	return ctx["from"], ctx["to"]
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
		// Start BOTH greeting and streaming in parallel.
		// Previously we waited for speak.ended before starting streaming,
		// which created a 2-5 second dead zone where Nova Sonic wasn't
		// listening. Now streaming starts immediately so Nova Sonic is
		// ready to hear the caller as soon as the greeting finishes.
		h.speakGreeting(callControlID, from, to)
		h.startStreaming(callControlID, from, to)

	case "speak.ended":
		// Greeting finished. Streaming is already active (started on call.answered).
		// Nova Sonic is listening and ready to process caller's response.
		h.logger.Info("call-control: greeting finished, streaming already active",
			"call_control_id", callControlID,
		)

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

	payload := map[string]interface{}{}
	h.sendCallControlCommand(callControlID, "answer", payload)
}

// startStreaming tells Telnyx to start media streaming to our WebSocket.
// It encodes caller context (from, to, orgID) into client_state so the
// WebSocket handler can associate the stream with the right clinic.
func (h *CallControlHandler) startStreaming(callControlID, from, to string) {
	// Resolve orgID from the "to" number (the clinic's phone)
	orgID := ""
	if h.orgResolver != nil {
		if resolved, err := h.orgResolver.ResolveOrgID(context.Background(), to); err == nil {
			orgID = resolved
		} else {
			h.logger.Warn("call-control: could not resolve org for number", "to", to, "error", err)
		}
	}

	h.logger.Info("call-control: starting media stream",
		"call_control_id", callControlID,
		"stream_url", h.streamURL,
		"from", from,
		"to", to,
		"org_id", orgID,
	)

	// Encode caller context as base64 JSON in client_state
	cs, _ := json.Marshal(map[string]string{
		"from":   from,
		"to":     to,
		"org_id": orgID,
	})
	clientState := base64.StdEncoding.EncodeToString(cs)

	payload := map[string]interface{}{
		"stream_url":                 h.streamURL,
		"stream_track":               "both_tracks",
		"stream_bidirectional_mode":  "rtp",
		"stream_bidirectional_codec": "L16",
		"enable_dialogflow":          false,
		"client_state":               clientState,
	}
	h.sendCallControlCommand(callControlID, "streaming_start", payload)
}

// speakGreeting uses Telnyx TTS to speak a greeting, then passes caller
// context via client_state so startStreaming can pick it up on speak.ended.
func (h *CallControlHandler) speakGreeting(callControlID, from, to string) {
	// Resolve clinic name for a personalized greeting
	clinicName := "our office"
	orgID := ""
	if h.orgResolver != nil {
		if resolved, err := h.orgResolver.ResolveOrgID(context.Background(), to); err == nil {
			orgID = resolved
		}
	}
	// Try to get clinic name from org ID
	if orgID != "" {
		// Use a simple mapping for now — clinic names from known configs
		clinicName = orgIDToClinicName(orgID)
	}

	greeting := fmt.Sprintf(
		"Hi there! Thanks so much for calling %s. How can I help you today?",
		clinicName,
	)

	h.logger.Info("call-control: speaking greeting",
		"call_control_id", callControlID,
		"clinic", clinicName,
		"org_id", orgID,
	)

	// Encode caller context in client_state so speak.ended can pass it to startStreaming
	cs, _ := json.Marshal(map[string]string{
		"from":   from,
		"to":     to,
		"org_id": orgID,
	})
	clientState := base64.StdEncoding.EncodeToString(cs)

	// Use Telnyx premium voice for natural-sounding greeting.
	// "Polly.Joanna" = AWS Polly neural voice via Telnyx (warm, professional female).
	// Other options: "Polly.Salli", "Polly.Kendra", "Polly.Ruth" (neural).
	payload := map[string]interface{}{
		"payload":      greeting,
		"voice":        "Polly.Ruth",
		"language":     "en-US",
		"client_state": clientState,
	}
	h.sendCallControlCommand(callControlID, "speak", payload)
}

// orgIDToClinicName maps org IDs to display names for voice greetings.
func orgIDToClinicName(orgID string) string {
	names := map[string]string{
		"d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599": "Forever 22 Med Spa",
		"brilliant-aesthetics":                 "Brilliant Aesthetics",
		"lucys-laser-medspa":                   "Lucy's Laser and Med Spa",
		"adela-medical-spa":                    "Adela Medical Spa",
	}
	if name, ok := names[orgID]; ok {
		return name
	}
	return "our office"
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
