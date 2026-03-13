package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// Enqueuer defines how conversation requests are dispatched.
type Enqueuer interface {
	EnqueueStart(ctx context.Context, jobID string, req StartRequest, opts ...PublishOption) error
	EnqueueMessage(ctx context.Context, jobID string, req MessageRequest, opts ...PublishOption) error
}

// Handler wires HTTP requests to the conversation queue.
type Handler struct {
	enqueuer  Enqueuer
	jobs      JobRecorder
	knowledge KnowledgeRepository
	rag       RAGIngestor
	service   Service
	sms       *SMSTranscriptStore
	logger    *logging.Logger
}

// NewHandler creates a conversation handler.
func NewHandler(enqueuer Enqueuer, jobs JobRecorder, knowledge KnowledgeRepository, rag RAGIngestor, logger *logging.Logger) *Handler {
	return &Handler{
		enqueuer:  enqueuer,
		jobs:      jobs,
		knowledge: knowledge,
		rag:       rag,
		logger:    logger,
	}
}

// SetService attaches the conversation service for transcript lookups.
func (h *Handler) SetService(s Service) {
	h.service = s
}

// SetSMSTranscriptStore attaches the Redis-backed SMS transcript store for phone-view / E2E.
func (h *Handler) SetSMSTranscriptStore(store *SMSTranscriptStore) {
	h.sms = store
}

// Start handles POST /conversations/start.
func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		StartRequest
		ScheduledFor string `json:"scheduled_for,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.logger.Error("failed to decode start request", "error", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	req := payload.StartRequest
	if strings.TrimSpace(payload.ScheduledFor) != "" {
		when, err := time.Parse(time.RFC3339, payload.ScheduledFor)
		if err != nil {
			http.Error(w, "invalid scheduled_for format", http.StatusBadRequest)
			return
		}
		if req.Metadata == nil {
			req.Metadata = map[string]string{}
		}
		req.Metadata["scheduled_for"] = when.UTC().Format(time.RFC3339)
		req.Metadata["scheduledFor"] = when.UTC().Format(time.RFC3339)
	}

	jobID := uuid.NewString()

	if err := h.enqueuer.EnqueueStart(r.Context(), jobID, req); err != nil {
		h.logger.Error("failed to enqueue start conversation", "error", err)
		http.Error(w, "Failed to schedule conversation start", http.StatusInternalServerError)
		return
	}

	h.writeAccepted(w, jobID)
}

// Message handles POST /conversations/message.
func (h *Handler) Message(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		MessageRequest
		ScheduledFor string `json:"scheduled_for,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.logger.Error("failed to decode message request", "error", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	req := payload.MessageRequest
	if strings.TrimSpace(payload.ScheduledFor) != "" {
		when, err := time.Parse(time.RFC3339, payload.ScheduledFor)
		if err != nil {
			http.Error(w, "invalid scheduled_for format", http.StatusBadRequest)
			return
		}
		if req.Metadata == nil {
			req.Metadata = map[string]string{}
		}
		req.Metadata["scheduled_for"] = when.UTC().Format(time.RFC3339)
		req.Metadata["scheduledFor"] = when.UTC().Format(time.RFC3339)
	}

	jobID := uuid.NewString()

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
