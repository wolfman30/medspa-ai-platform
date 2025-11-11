package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// Enqueuer defines how conversation requests are dispatched.
type Enqueuer interface {
	EnqueueStart(ctx context.Context, jobID string, req StartRequest) error
	EnqueueMessage(ctx context.Context, jobID string, req MessageRequest) error
}

// Handler wires HTTP requests to the conversation queue.
type Handler struct {
	enqueuer Enqueuer
	jobs     JobRecorder
	logger   *logging.Logger
}

// NewHandler creates a conversation handler.
func NewHandler(enqueuer Enqueuer, jobs JobRecorder, logger *logging.Logger) *Handler {
	return &Handler{
		enqueuer: enqueuer,
		jobs:     jobs,
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

	jobID := uuid.NewString()

	if err := h.recordPendingJob(r.Context(), jobID, jobTypeStart, &req, nil); err != nil {
		h.logger.Error("failed to persist job record", "error", err)
		http.Error(w, "Failed to persist job record", http.StatusInternalServerError)
		return
	}

	if err := h.enqueuer.EnqueueStart(r.Context(), jobID, req); err != nil {
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

	jobID := uuid.NewString()

	if err := h.recordPendingJob(r.Context(), jobID, jobTypeMessage, nil, &req); err != nil {
		h.logger.Error("failed to persist job record", "error", err)
		http.Error(w, "Failed to persist job record", http.StatusInternalServerError)
		return
	}

	if err := h.enqueuer.EnqueueMessage(r.Context(), jobID, req); err != nil {
		h.logger.Error("failed to enqueue message", "error", err)
		http.Error(w, "Failed to schedule message", http.StatusInternalServerError)
		return
	}

	h.writeAccepted(w, jobID)
}

// JobStatus handles GET /conversations/jobs/{jobID}.
func (h *Handler) JobStatus(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	if jobID == "" {
		http.Error(w, "jobID is required", http.StatusBadRequest)
		return
	}

	job, err := h.jobs.GetJob(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, ErrJobNotFound) {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		h.logger.Error("failed to load job", "error", err, "job_id", jobID)
		http.Error(w, "Failed to load job", http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, http.StatusOK, job)
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

func (h *Handler) recordPendingJob(ctx context.Context, jobID string, kind jobType, start *StartRequest, message *MessageRequest) error {
	if jobID == "" {
		return errors.New("missing job ID")
	}

	job := &JobRecord{
		JobID:          jobID,
		RequestType:    kind,
		StartRequest:   start,
		MessageRequest: message,
	}
	if message != nil {
		job.ConversationID = message.ConversationID
	}
	return h.jobs.PutPending(ctx, job)
}
