package conversation

import (
	"encoding/json"
	"net/http"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// Handler wires HTTP requests to the conversation service.
type Handler struct {
	service Service
	logger  *logging.Logger
}

// NewHandler creates a conversation handler.
func NewHandler(service Service, logger *logging.Logger) *Handler {
	return &Handler{
		service: service,
		logger:  logger,
	}
}

// Start handles POST /conversations/start.
func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	var req StartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("failed to decode start request", "error", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	resp, err := h.service.StartConversation(r.Context(), req)
	if err != nil {
		h.logger.Error("failed to start conversation", "error", err)
		http.Error(w, "Failed to start conversation", http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, http.StatusCreated, resp)
}

// Message handles POST /conversations/message.
func (h *Handler) Message(w http.ResponseWriter, r *http.Request) {
	var req MessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("failed to decode message request", "error", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	resp, err := h.service.ProcessMessage(r.Context(), req)
	if err != nil {
		h.logger.Error("failed to process message", "error", err)
		http.Error(w, "Failed to process message", http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		h.logger.Error("failed to write JSON response", "error", err)
	}
}
