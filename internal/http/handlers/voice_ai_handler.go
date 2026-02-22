package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// ----- Telnyx Voice AI webhook event types -----

// VoiceAIEvent represents the top-level Telnyx Voice AI webhook payload.
// Telnyx AI Assistants send webhook events when tools are invoked during a
// voice conversation. Our endpoint is registered as a webhook tool on the
// Telnyx AI Assistant; when the assistant's LLM decides it needs to consult
// our brain, it calls this tool with the patient's latest utterance.
type VoiceAIEvent struct {
	// AssistantID is the Telnyx AI Assistant that originated the event.
	AssistantID string `json:"assistant_id,omitempty"`
	// ConversationID groups turns within a single call.
	ConversationID string `json:"conversation_id,omitempty"`
	// EventType identifies the webhook event (e.g. "tool_call").
	EventType string `json:"event_type,omitempty"`
	// From is the caller's phone number (E.164).
	From string `json:"from,omitempty"`
	// To is the Telnyx number that received the call (E.164).
	To string `json:"to,omitempty"`
	// Payload holds the tool-specific data.
	Payload VoiceAIPayload `json:"payload,omitempty"`
}

// VoiceAIPayload carries the tool invocation details.
type VoiceAIPayload struct {
	// ToolName is the name of the webhook tool being invoked.
	ToolName string `json:"tool_name,omitempty"`
	// ToolCallID is the unique identifier for this tool call; must be echoed
	// back in the response so Telnyx can correlate the result.
	ToolCallID string `json:"tool_call_id,omitempty"`
	// Arguments is a map of named arguments supplied by the Telnyx LLM.
	// For our "consult_medspa_brain" tool we expect:
	//   - "transcript": the patient's latest utterance (STT output)
	//   - "summary":    optional conversation summary so far
	Arguments map[string]string `json:"arguments,omitempty"`
}

// VoiceAIResponse is the JSON body we return to Telnyx. The assistant's TTS
// engine converts Response into speech for the caller.
type VoiceAIResponse struct {
	// ToolCallID echoes back the request's ToolCallID.
	ToolCallID string `json:"tool_call_id"`
	// Response is the text that Telnyx TTS will speak to the patient.
	Response string `json:"response"`
}

// VoiceAIErrorResponse is returned when we cannot process the event.
type VoiceAIErrorResponse struct {
	ToolCallID string `json:"tool_call_id,omitempty"`
	Error      string `json:"error"`
}

// ----- Handler -----

// clinicByNumberLookup resolves a clinic UUID from a phone number.
type clinicByNumberLookup interface {
	LookupClinicByNumber(ctx context.Context, number string) (uuid.UUID, error)
}

// VoiceAIHandler handles Telnyx Voice AI webhook events. It acts as a channel
// adapter: it receives transcribed speech from the Telnyx AI Assistant, feeds
// it through the shared conversation engine, and returns text for TTS.
type VoiceAIHandler struct {
	store       clinicByNumberLookup
	publisher   conversationPublisher
	processor   conversation.Service
	clinicStore *clinic.Store
	logger      *logging.Logger

	// voicePromptAddition is injected into voice-channel conversations.
	voicePromptAddition string
}

// VoiceAIHandlerConfig configures the VoiceAIHandler.
type VoiceAIHandlerConfig struct {
	Store       clinicByNumberLookup
	Publisher   conversationPublisher
	Processor   conversation.Service
	ClinicStore *clinic.Store
	Logger      *logging.Logger
}

// NewVoiceAIHandler creates a new VoiceAIHandler.
func NewVoiceAIHandler(cfg VoiceAIHandlerConfig) *VoiceAIHandler {
	if cfg.Logger == nil {
		cfg.Logger = logging.Default()
	}
	return &VoiceAIHandler{
		store:       cfg.Store,
		publisher:   cfg.Publisher,
		processor:   cfg.Processor,
		clinicStore: cfg.ClinicStore,
		logger:      cfg.Logger,
		voicePromptAddition: "Keep responses to 1-2 sentences. " +
			"Use spoken language, not written. " +
			"Say 'I'll text you a link' instead of sharing URLs. " +
			"Be warm and professional.",
	}
}

// HandleVoiceAI is the HTTP handler for POST /webhooks/telnyx/voice-ai.
func (h *VoiceAIHandler) HandleVoiceAI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		h.logger.Error("voice-ai: failed to read body", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var event VoiceAIEvent
	if err := json.Unmarshal(body, &event); err != nil {
		h.logger.Error("voice-ai: failed to parse event", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	h.logger.Info("voice-ai: received event",
		"event_type", event.EventType,
		"assistant_id", event.AssistantID,
		"conversation_id", event.ConversationID,
		"from", event.From,
		"to", event.To,
		"tool_name", event.Payload.ToolName,
	)

	// Look up the clinic by the Telnyx number that received the call.
	to := messaging.NormalizeE164(event.To)
	clinicCfg, orgID, err := h.resolveClinic(ctx, to)
	if err != nil {
		h.logger.Warn("voice-ai: clinic lookup failed", "to", to, "error", err)
		h.writeError(w, event.Payload.ToolCallID, "clinic not found", http.StatusNotFound)
		return
	}

	// Check voice AI toggle.
	if !clinicCfg.VoiceAIEnabled {
		h.logger.Info("voice-ai: voice AI disabled for clinic, falling back", "org_id", orgID)
		h.writeResponse(w, event.Payload.ToolCallID,
			"I'm sorry, our voice assistant isn't available right now. "+
				"We'll send you a text message shortly to help you out!")
		return
	}

	// Validate AssistantID matches the clinic's configured Telnyx assistant.
	// This prevents unauthorized callers from enqueuing work into the conversation engine.
	if clinicCfg.TelnyxAssistantID != "" && event.AssistantID != clinicCfg.TelnyxAssistantID {
		h.logger.Warn("voice-ai: assistant ID mismatch",
			"expected", clinicCfg.TelnyxAssistantID,
			"got", event.AssistantID,
			"org_id", orgID,
		)
		h.writeError(w, event.Payload.ToolCallID, "unauthorized", http.StatusForbidden)
		return
	}

	// Extract the patient's transcript from tool arguments.
	transcript := strings.TrimSpace(event.Payload.Arguments["transcript"])
	if transcript == "" {
		h.logger.Warn("voice-ai: empty transcript", "conversation_id", event.ConversationID)
		h.writeError(w, event.Payload.ToolCallID, "no transcript provided", http.StatusBadRequest)
		return
	}

	from := messaging.NormalizeE164(event.From)
	convID := event.ConversationID
	if convID == "" {
		convID = fmt.Sprintf("voice:%s:%s", orgID, strings.TrimPrefix(from, "+"))
	}

	msgReq := conversation.MessageRequest{
		OrgID:          orgID,
		ConversationID: convID,
		Message:        transcript,
		Channel:        conversation.ChannelVoice,
		From:           from,
		To:             to,
		Metadata: map[string]string{
			"voice_prompt_addition": h.voicePromptAddition,
			"telnyx_assistant_id":   event.AssistantID,
			"tool_call_id":          event.Payload.ToolCallID,
		},
	}

	// Synchronous path: process inline and return the response for TTS.
	// Voice calls need sub-second latency; queueing adds unacceptable delay.
	if h.processor != nil {
		resp, err := h.processor.ProcessMessage(ctx, msgReq)
		if err != nil {
			h.logger.Error("voice-ai: processor error",
				"error", err, "conversation_id", convID)
			h.writeResponse(w, event.Payload.ToolCallID,
				"I'm sorry, I'm having a bit of trouble. Could you say that again?")
			return
		}
		responseText := resp.Message
		if responseText == "" {
			responseText = "I'm sorry, could you repeat that?"
		}
		h.writeResponse(w, event.Payload.ToolCallID, responseText)
		return
	}

	// Async fallback: enqueue and return interim response.
	jobID := fmt.Sprintf("voice-%s-%d", convID, time.Now().UnixMilli())
	if err := h.publisher.EnqueueMessage(ctx, jobID, msgReq); err != nil {
		h.logger.Error("voice-ai: failed to enqueue message",
			"error", err, "conversation_id", convID)
		h.writeError(w, event.Payload.ToolCallID, "internal error", http.StatusInternalServerError)
		return
	}

	h.writeResponse(w, event.Payload.ToolCallID,
		"Let me look into that for you, one moment please.")
}

// resolveClinic finds the clinic configuration by the called phone number.
func (h *VoiceAIHandler) resolveClinic(ctx context.Context, toNumber string) (*clinic.Config, string, error) {
	if h.store == nil {
		return nil, "", fmt.Errorf("messaging store not configured")
	}
	if h.clinicStore == nil {
		return nil, "", fmt.Errorf("clinic store not configured")
	}

	clinicID, err := h.store.LookupClinicByNumber(ctx, toNumber)
	if err != nil {
		return nil, "", fmt.Errorf("lookup clinic by number %s: %w", toNumber, err)
	}

	orgID := clinicID.String()
	cfg, err := h.clinicStore.Get(ctx, orgID)
	if err != nil {
		return nil, "", fmt.Errorf("get clinic config for %s: %w", orgID, err)
	}
	return cfg, orgID, nil
}

// writeResponse sends a successful VoiceAIResponse.
func (h *VoiceAIHandler) writeResponse(w http.ResponseWriter, toolCallID, text string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(VoiceAIResponse{
		ToolCallID: toolCallID,
		Response:   text,
	})
}

// writeError sends an error response.
func (h *VoiceAIHandler) writeError(w http.ResponseWriter, toolCallID, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(VoiceAIErrorResponse{
		ToolCallID: toolCallID,
		Error:      msg,
	})
}
