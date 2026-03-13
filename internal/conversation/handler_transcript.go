package conversation

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// TranscriptResponse is the response for GET /admin/clinics/{orgID}/conversations/{phone}.
type TranscriptResponse struct {
	ConversationID string    `json:"conversation_id"`
	Messages       []Message `json:"messages"`
}

// GetTranscript handles GET /admin/clinics/{orgID}/conversations/{phone}.
// Returns the conversation transcript for a given phone number.
func (h *Handler) GetTranscript(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	phoneParam := chi.URLParam(r, "phone")
	phone, err := url.PathUnescape(phoneParam)
	if err != nil {
		http.Error(w, "invalid phone encoding", http.StatusBadRequest)
		return
	}
	phone = strings.TrimSpace(phone)

	if orgID == "" || phone == "" {
		http.Error(w, "missing org_id or phone", http.StatusBadRequest)
		return
	}

	if h.service == nil {
		http.Error(w, "transcript service not configured", http.StatusInternalServerError)
		return
	}

	digits := sanitizeDigits(phone)
	if digits == "" {
		http.Error(w, "invalid phone", http.StatusBadRequest)
		return
	}
	digits = normalizeUSDigits(digits)
	conversationID := fmt.Sprintf("sms:%s:%s", orgID, digits)

	messages, err := h.service.GetHistory(r.Context(), conversationID)
	if err != nil {
		// Check if it's a "not found" error
		if strings.Contains(err.Error(), "unknown conversation") {
			http.Error(w, "conversation not found", http.StatusNotFound)
			return
		}
		h.logger.Error("failed to get transcript", "error", err, "conversation_id", conversationID)
		http.Error(w, "failed to retrieve transcript", http.StatusInternalServerError)
		return
	}

	resp := TranscriptResponse{
		ConversationID: conversationID,
		Messages:       messages,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// SMSTranscriptResponse is the response for GET /admin/clinics/{orgID}/sms/{phone}.
type SMSTranscriptResponse struct {
	ConversationID string                 `json:"conversation_id"`
	Messages       []SMSTranscriptMessage `json:"messages"`
}

// GetSMSTranscript handles GET /admin/clinics/{orgID}/sms/{phone}.
// Returns a Redis-backed SMS transcript that includes webhook acks, AI replies, deposit links, and confirmations.
func (h *Handler) GetSMSTranscript(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	phoneParam := chi.URLParam(r, "phone")
	phone, err := url.PathUnescape(phoneParam)
	if err != nil {
		http.Error(w, "invalid phone encoding", http.StatusBadRequest)
		return
	}
	phone = strings.TrimSpace(phone)

	if orgID == "" || phone == "" {
		http.Error(w, "missing org_id or phone", http.StatusBadRequest)
		return
	}
	if h.sms == nil {
		http.Error(w, "sms transcript store not configured", http.StatusServiceUnavailable)
		return
	}

	digits := sanitizeDigits(phone)
	if digits == "" {
		http.Error(w, "invalid phone", http.StatusBadRequest)
		return
	}
	digits = normalizeUSDigits(digits)
	conversationID := fmt.Sprintf("sms:%s:%s", orgID, digits)

	var limit int64
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = parsed
	}

	messages, err := h.sms.List(r.Context(), conversationID, limit)
	if err != nil {
		h.logger.Error("failed to load sms transcript", "error", err, "conversation_id", conversationID)
		http.Error(w, "failed to retrieve sms transcript", http.StatusInternalServerError)
		return
	}

	resp := SMSTranscriptResponse{
		ConversationID: conversationID,
		Messages:       messages,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// sanitizeDigits strips all non-digit characters from a phone string.
func sanitizeDigits(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// normalizeUSDigits converts 10-digit US numbers to E.164 digits by prefixing "1".
func normalizeUSDigits(digits string) string {
	if len(digits) == 10 {
		return "1" + digits
	}
	return digits
}
