package conversation

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// Enqueuer defines how conversation requests are dispatched.
type Enqueuer interface {
	EnqueueStart(ctx context.Context, req StartRequest) (string, error)
	EnqueueMessage(ctx context.Context, req MessageRequest) (string, error)
}

// Handler wires HTTP requests to the conversation queue.
type Handler struct {
	enqueuer Enqueuer
	logger   *logging.Logger
}

// NewHandler creates a conversation handler.
func NewHandler(enqueuer Enqueuer, logger *logging.Logger) *Handler {
	return &Handler{
		enqueuer: enqueuer,
		logger:   logger,
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

	jobID, err := h.enqueuer.EnqueueStart(r.Context(), req)
	if err != nil {
		h.logger.Error("failed to enqueue start conversation", "error", err)
		http.Error(w, "Failed to schedule conversation start", http.StatusInternalServerError)
		return
	}

	h.writeAccepted(w, jobID)
}

// Message handles POST /conversations/message.
func (h *Handler) Message(w http.ResponseWriter, r *http.Request) {
	var req MessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("failed to decode message request", "error", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	jobID, err := h.enqueuer.EnqueueMessage(r.Context(), req)
	if err != nil {
		h.logger.Error("failed to enqueue message", "error", err)
		http.Error(w, "Failed to schedule message", http.StatusInternalServerError)
		return
	}

	h.writeAccepted(w, jobID)
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		h.logger.Error("failed to write JSON response", "error", err)
	}
}

func (h *Handler) writeAccepted(w http.ResponseWriter, jobID string) {
	h.writeJSON(w, http.StatusAccepted, struct {
		JobID  string `json:"jobId"`
		Status string `json:"status"`
	}{
		JobID:  jobID,
		Status: "accepted",
	})
}
