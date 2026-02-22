package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
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
	redis       *redis.Client
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
	Redis       *redis.Client
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
		redis:       cfg.Redis,
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

	// Log raw body and headers for debugging Telnyx payload format.
	hdrs := make(map[string]string)
	for k, v := range r.Header {
		hdrs[k] = strings.Join(v, ", ")
	}
	hdrJSON, _ := json.Marshal(hdrs)
	h.logger.Info("voice-ai: request debug", "body", string(body), "headers", string(hdrJSON), "query", r.URL.RawQuery)

	// Telnyx AI Assistant webhook tools send the body parameters directly as JSON.
	// We also accept the legacy VoiceAIEvent format for flexibility.
	var toolArgs map[string]interface{}
	if err := json.Unmarshal(body, &toolArgs); err != nil {
		h.logger.Error("voice-ai: failed to parse body", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Extract fields — Telnyx sends our defined body parameters directly.
	transcript := strings.TrimSpace(fmt.Sprintf("%v", toolArgs["transcript"]))
	if transcript == "<nil>" || transcript == "" {
		h.logger.Warn("voice-ai: empty transcript")
		h.respondJSON(w, map[string]string{"response": "Could you say that again?"})
		return
	}

	// Get summary from Telnyx (cumulative conversation context).
	summary := strings.TrimSpace(fmt.Sprintf("%v", toolArgs["summary"]))
	if summary == "<nil>" {
		summary = ""
	}

	// Get caller/called info from body params or query params.
	from := strings.TrimSpace(fmt.Sprintf("%v", toolArgs["caller_number"]))
	to := strings.TrimSpace(fmt.Sprintf("%v", toolArgs["called_number"]))
	if from == "<nil>" {
		from = r.URL.Query().Get("from")
	}
	if to == "<nil>" {
		to = r.URL.Query().Get("to")
	}
	from = messaging.NormalizeE164(from)
	to = messaging.NormalizeE164(to)

	// If Telnyx didn't provide caller number, try to extract from transcript/summary.
	if from == "" {
		if extracted := extractPhoneFromText(transcript); extracted != "" {
			from = extracted
		} else if extracted := extractPhoneFromText(summary); extracted != "" {
			from = extracted
		}
	}

	h.logger.Info("voice-ai: parsed tool call",
		"transcript", transcript,
		"summary", summary,
		"from", from,
		"to", to,
	)

	// Look up the clinic by the Telnyx number that received the call.
	_, orgID, err := h.resolveClinic(ctx, to)
	if err != nil {
		h.logger.Warn("voice-ai: clinic lookup failed, trying all clinics", "to", to, "error", err)
		// Fallback: if no 'to' number, use a default org for testing
		if to == "" {
			// Use Forever 22 as default for now
			orgID = "d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599"
			_, err = h.clinicStore.Get(ctx, orgID)
			if err != nil {
				h.logger.Error("voice-ai: fallback clinic lookup failed", "error", err)
				h.respondJSON(w, map[string]string{"response": "I'm sorry, I'm having technical difficulties. Can I help you via text instead?"})
				return
			}
		} else {
			h.respondJSON(w, map[string]string{"response": "I'm sorry, I'm having technical difficulties. Can I help you via text instead?"})
			return
		}
	}

	// Build a stable conversation ID. If we have a phone number, use it.
	// Otherwise, use Redis to maintain a session: the first webhook hit for
	// an org creates a session ID with a 15-min TTL; subsequent hits reuse it.
	// This keeps all turns of a single call in one conversation.
	var convID string
	if from != "" {
		convID = fmt.Sprintf("voice:%s:%s", orgID, strings.TrimPrefix(from, "+"))
		// Also update the active session to use this phone-based ID
		if h.redis != nil {
			sessionKey := fmt.Sprintf("voice_session:%s", orgID)
			h.redis.Set(ctx, sessionKey, convID, 15*time.Minute)
		}
	} else {
		convID = h.getOrCreateVoiceSession(ctx, orgID)
	}

	// Build the message. If we have a summary from Telnyx, include it as
	// context so the brain knows the full conversation even if this is a
	// "new" conversation from our perspective (no stable call ID).
	message := transcript
	if summary != "" {
		message = fmt.Sprintf("[Conversation so far: %s]\n\nPatient just said: %s", summary, transcript)
	}

	msgReq := conversation.MessageRequest{
		OrgID:          orgID,
		ConversationID: convID,
		Message:        message,
		Channel:        conversation.ChannelVoice,
		From:           from,
		To:             to,
		Metadata: map[string]string{
			"voice_prompt_addition": h.voicePromptAddition,
		},
	}

	// Synchronous path: process inline and return the response for TTS.
	// Voice calls need sub-second latency; queueing adds unacceptable delay.
	if h.processor != nil {
		resp, err := h.processor.ProcessMessage(ctx, msgReq)
		if err != nil {
			h.logger.Error("voice-ai: processor error",
				"error", err, "conversation_id", convID)
			h.respondJSON(w, map[string]string{"response": "I'm sorry, I'm having a bit of trouble. Could you say that again?"})
			return
		}
		responseText := resp.Message
		if responseText == "" {
			responseText = "I'm sorry, could you repeat that?"
		}
		h.logger.Info("voice-ai: sending response", "response", responseText, "conversation_id", convID)
		h.respondJSON(w, map[string]string{"response": responseText})
		return
	}

	// Async fallback: enqueue and return interim response.
	jobID := fmt.Sprintf("voice-%s-%d", convID, time.Now().UnixMilli())
	if err := h.publisher.EnqueueMessage(ctx, jobID, msgReq); err != nil {
		h.logger.Error("voice-ai: failed to enqueue message",
			"error", err, "conversation_id", convID)
		h.respondJSON(w, map[string]string{"response": "I'm sorry, I'm having technical difficulties."})
		return
	}

	h.respondJSON(w, map[string]string{"response": "Let me look into that for you, one moment please."})
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

// getOrCreateVoiceSession returns a stable conversation ID for voice calls
// without a caller phone number. Uses Redis with a 15-minute TTL so all
// webhook hits during a single call share the same conversation.
func (h *VoiceAIHandler) getOrCreateVoiceSession(ctx context.Context, orgID string) string {
	sessionKey := fmt.Sprintf("voice_session:%s", orgID)

	if h.redis != nil {
		// Try to get existing session
		existing, err := h.redis.Get(ctx, sessionKey).Result()
		if err == nil && existing != "" {
			// Refresh TTL on each turn
			h.redis.Expire(ctx, sessionKey, 15*time.Minute)
			return existing
		}

		// Create new session
		convID := fmt.Sprintf("voice:%s:call-%d", orgID, time.Now().UnixMilli())
		h.redis.Set(ctx, sessionKey, convID, 15*time.Minute)
		return convID
	}

	// No Redis — fall back to timestamp (no continuity)
	return fmt.Sprintf("voice:%s:%d", orgID, time.Now().UnixMilli())
}

// phoneFromTextRe matches US phone numbers in various formats.
var phoneFromTextRe = regexp.MustCompile(`(?:\+?1[-.\s]?)?(?:\(?(\d{3})\)?[-.\s]?)(\d{3})[-.\s]?(\d{4})`)

// extractPhoneFromText tries to find a US phone number in free text and
// returns it in E.164 format, or "" if none found.
func extractPhoneFromText(text string) string {
	m := phoneFromTextRe.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	return messaging.NormalizeE164(fmt.Sprintf("+1%s%s%s", m[1], m[2], m[3]))
}

// respondJSON writes a generic JSON response (for Telnyx tool webhook format).
func (h *VoiceAIHandler) respondJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(data)
}
