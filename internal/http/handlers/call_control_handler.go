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

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// MissedCallTexter triggers the text-back flow for a missed call.
type MissedCallTexter interface {
	// HandleMissedCall triggers text-back for a missed call from `from` to `to`.
	HandleMissedCall(ctx context.Context, from, to string) error
}

// PostCallSMSSender sends a follow-up SMS after a voice call ends.
type PostCallSMSSender interface {
	// SendPostCallSMS sends a deposit/booking link SMS to the caller after hangup.
	SendPostCallSMS(ctx context.Context, callerPhone, clinicPhone string) error
}

// CallControlHandler handles Telnyx Call Control webhook events.
// When a call comes in, it checks if the clinic has voice AI enabled.
// If yes, it answers and starts media streaming to Nova Sonic.
// If no, it rejects the call and triggers the missed-call text-back flow.
type CallControlHandler struct {
	logger           *logging.Logger
	telnyxAPIKey     string
	telnyxBaseURL    string // Override for testing; defaults to "https://api.telnyx.com"
	streamURL        string // e.g. "wss://api-dev.aiwolfsolutions.com/ws/voice"
	orgResolver      messaging.OrgResolver
	clinicStore      *clinic.Store
	missedCallTexter MissedCallTexter
	postCallSMS      PostCallSMSSender
}

// CallControlConfig configures the handler.
type CallControlConfig struct {
	Logger           *logging.Logger
	TelnyxAPIKey     string
	StreamURL        string
	OrgResolver      messaging.OrgResolver
	ClinicStore      *clinic.Store
	MissedCallTexter MissedCallTexter
	PostCallSMS      PostCallSMSSender
}

// NewCallControlHandler creates a new Call Control webhook handler.
func NewCallControlHandler(cfg CallControlConfig) *CallControlHandler {
	if cfg.Logger == nil {
		cfg.Logger = logging.Default()
	}
	return &CallControlHandler{
		logger:           cfg.Logger,
		telnyxAPIKey:     cfg.TelnyxAPIKey,
		streamURL:        cfg.StreamURL,
		orgResolver:      cfg.OrgResolver,
		clinicStore:      cfg.ClinicStore,
		missedCallTexter: cfg.MissedCallTexter,
		postCallSMS:      cfg.PostCallSMS,
	}
}

// SetMissedCallTexter sets the missed call handler after construction (for circular dependency resolution).
func (h *CallControlHandler) SetMissedCallTexter(t MissedCallTexter) {
	h.missedCallTexter = t
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
			// Check if clinic has voice AI enabled before answering.
			// If not, reject the call so it falls through to missed-call text-back.
			if !h.isVoiceAIEnabled(to) {
				h.logger.Info("call-control: voice AI not enabled for clinic, rejecting call for text-back flow",
					"to", to, "from", from)
				h.rejectCall(callControlID)
				// Trigger text-back flow directly since the voice webhook
				// won't receive events for Call Control-routed numbers.
				if h.missedCallTexter != nil {
					go func() {
						if err := h.missedCallTexter.HandleMissedCall(context.Background(), from, to); err != nil {
							h.logger.Error("call-control: failed to trigger text-back", "error", err, "from", from, "to", to)
						}
					}()
				}
				return
			}
			h.answerCall(callControlID)
		}

	case "call.answered":
		// Play pre-recorded Lauren greeting via Telnyx play command (instant, <1s).
		// Audio is pre-generated with ElevenLabs and hosted on S3.
		// Simultaneously start streaming so Nova Sonic boots in the background.
		h.playPreRecordedGreeting(callControlID, from, to)
		h.startStreaming(callControlID, from, to)
		// Record the call server-side for demo videos and QA review
		h.startRecording(callControlID)

	case "streaming.started":
		// Stream is live — Nova Sonic is ready. Pre-recorded greeting may still
		// be playing. Nova Sonic will wait for caller's first words after greeting.
		h.logger.Info("call-control: streaming started, pre-recorded greeting playing",
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
		// Deposit SMS is now sent DURING the call (bridge.go detects Lauren's
		// deposit intent in her transcript and fires SMS immediately).
		// No post-hangup SMS needed.

	default:
		h.logger.Debug("call-control: unhandled event", "event_type", eventType)
	}

	// Always return 200 to acknowledge the webhook
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"ok"}`)
}

// isVoiceAIEnabled checks if the clinic associated with the called number has voice AI enabled.
func (h *CallControlHandler) isVoiceAIEnabled(toNumber string) bool {
	if h.clinicStore == nil || h.orgResolver == nil {
		// No clinic store — default to answering (backward compatible)
		return true
	}
	orgID, err := h.orgResolver.ResolveOrgID(context.Background(), toNumber)
	if err != nil || orgID == "" {
		h.logger.Warn("call-control: could not resolve org for voice AI check, defaulting to answer",
			"to", toNumber, "error", err)
		return true
	}
	cfg, err := h.clinicStore.Get(context.Background(), orgID)
	if err != nil || cfg == nil {
		h.logger.Warn("call-control: could not load clinic config for voice AI check, defaulting to answer",
			"org_id", orgID, "error", err)
		return true
	}
	return cfg.VoiceAIEnabled
}

// rejectCall sends the reject command to Telnyx so the call goes to missed-call flow.
func (h *CallControlHandler) rejectCall(callControlID string) {
	h.logger.Info("call-control: rejecting call for text-back flow", "call_control_id", callControlID)
	payload := map[string]interface{}{
		"cause": "CALL_REJECTED",
	}
	h.sendCallControlCommand(callControlID, "reject", payload)
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
		"stream_track":               "inbound_track",
		"stream_bidirectional_mode":  "rtp",
		"stream_bidirectional_codec": "L16",
		"enable_dialogflow":          false,
		"client_state":               clientState,
	}
	h.sendCallControlCommand(callControlID, "streaming_start", payload)
}

// startRecording tells Telnyx to record the call (both channels) for demo/QA purposes.
func (h *CallControlHandler) startRecording(callControlID string) {
	payload := map[string]interface{}{
		"format":   "mp3",
		"channels": "dual",
	}
	h.logger.Info("call-control: starting call recording", "call_control_id", callControlID)
	h.sendCallControlCommand(callControlID, "record_start", payload)
}

// playPreRecordedGreeting plays a pre-recorded Lauren ElevenLabs greeting via Telnyx play command.
// The audio is pre-generated and hosted on S3 for instant playback (<1s).
func (h *CallControlHandler) playPreRecordedGreeting(callControlID, from, to string) {
	orgID := ""
	if h.orgResolver != nil {
		if resolved, err := h.orgResolver.ResolveOrgID(context.Background(), to); err == nil {
			orgID = resolved
		}
	}

	// Map org IDs to pre-recorded greeting audio.
	// Primary: Telnyx media_name (pre-uploaded, zero download latency).
	// Fallback: audio_url from our API (in case media expires).
	type greetingConfig struct {
		mediaName string
		audioURL  string
	}
	greetings := map[string]greetingConfig{
		"d9558a2d-2110-4e26-8224-1b36cd526e14": {mediaName: "greeting-bodytonic", audioURL: "https://api-dev.aiwolfsolutions.com/static/greetings/bodytonic.mp3"},
		"d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599": {mediaName: "greeting-forever22", audioURL: "https://api-dev.aiwolfsolutions.com/static/greetings/forever22.mp3"},
	}

	cfg, ok := greetings[orgID]
	if !ok {
		// Fallback: let sidecar handle greeting via ElevenLabs TTS
		h.logger.Info("call-control: no pre-recorded greeting, sidecar will handle",
			"call_control_id", callControlID, "org_id", orgID)
		return
	}

	h.logger.Info("call-control: playing pre-recorded Lauren greeting",
		"call_control_id", callControlID, "org_id", orgID, "media_name", cfg.mediaName)

	// Use audio_url (media_name requires periodic re-upload and expires)
	payload := map[string]interface{}{
		"audio_url": cfg.audioURL,
	}
	h.sendCallControlCommand(callControlID, "playback_start", payload)
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
		"voice":        "Polly.Joanna",
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
		"d9558a2d-2110-4e26-8224-1b36cd526e14": "BodyTonic Medspa",
	}
	if name, ok := names[orgID]; ok {
		return name
	}
	return "our office"
}

// sendCallControlCommand sends a command to the Telnyx Call Control API.
func (h *CallControlHandler) sendCallControlCommand(callControlID, command string, payload map[string]interface{}) {
	baseURL := h.telnyxBaseURL
	if baseURL == "" {
		baseURL = "https://api.telnyx.com"
	}
	url := fmt.Sprintf("%s/v2/calls/%s/actions/%s",
		baseURL, strings.TrimSpace(callControlID), command)

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
