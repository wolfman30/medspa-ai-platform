package conversation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestHandler_Start_AcceptsJob(t *testing.T) {
	enqueuer := &stubEnqueuer{
		startJobID: "job-start-1",
	}
	store := &stubJobStore{}
	handler := NewHandler(enqueuer, store, logging.Default())

	payload := StartRequest{
		LeadID: "lead-123",
		Intro:  "Hi",
		Source: "web",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/conversations/start", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.Start(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d", http.StatusAccepted, w.Code)
	}

	var resp struct {
		JobID string `json:"jobId"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.JobID != "job-start-1" {
		t.Fatalf("expected job ID job-start-1, got %s", resp.JobID)
	}

	if enqueuer.lastStartReq.LeadID != payload.LeadID {
		t.Fatalf("expected LeadID %s, got %s", payload.LeadID, enqueuer.lastStartReq.LeadID)
	}

	if store.lastPut == nil || store.lastPut.JobID != "job-start-1" || store.lastPut.RequestType != jobTypeStart {
		t.Fatalf("expected job store to capture pending job, got %#v", store.lastPut)
	}
}

func TestHandler_Message_AcceptsJob(t *testing.T) {
	enqueuer := &stubEnqueuer{
		messageJobID: "job-msg-1",
	}
	store := &stubJobStore{}
	handler := NewHandler(enqueuer, store, logging.Default())

	payload := MessageRequest{
		ConversationID: "conv-123",
		Message:        "Hello",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/conversations/message", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.Message(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d", http.StatusAccepted, w.Code)
	}

	var resp struct {
		JobID string `json:"jobId"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.JobID != "job-msg-1" {
		t.Fatalf("expected job ID job-msg-1, got %s", resp.JobID)
	}

	if enqueuer.lastMessageReq.ConversationID != payload.ConversationID {
		t.Fatalf("expected conversation ID %s, got %s", payload.ConversationID, enqueuer.lastMessageReq.ConversationID)
	}

	if store.lastPut == nil || store.lastPut.MessageRequest == nil || store.lastPut.MessageRequest.ConversationID != payload.ConversationID {
		t.Fatalf("expected job store to capture message job, got %#v", store.lastPut)
	}
}

func TestHandler_Start_InvalidJSON(t *testing.T) {
	handler := NewHandler(&stubEnqueuer{}, &stubJobStore{}, logging.Default())

	req := httptest.NewRequest(http.MethodPost, "/conversations/start", strings.NewReader("{"))
	w := httptest.NewRecorder()

	handler.Start(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandler_Message_InvalidJSON(t *testing.T) {
	handler := NewHandler(&stubEnqueuer{}, &stubJobStore{}, logging.Default())

	req := httptest.NewRequest(http.MethodPost, "/conversations/message", strings.NewReader("{"))
	w := httptest.NewRecorder()

	handler.Message(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandler_Start_EnqueueError(t *testing.T) {
	handler := NewHandler(&stubEnqueuer{startErr: errors.New("boom")}, &stubJobStore{}, logging.Default())

	payload := StartRequest{LeadID: "lead"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/conversations/start", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.Start(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestHandler_Message_EnqueueError(t *testing.T) {
	handler := NewHandler(&stubEnqueuer{messageErr: errors.New("boom")}, &stubJobStore{}, logging.Default())

	payload := MessageRequest{ConversationID: "conv", Message: "Hi"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/conversations/message", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.Message(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestHandler_JobStatus_Success(t *testing.T) {
	store := &stubJobStore{
		getJob: &JobRecord{
			JobID:  "job-123",
			Status: JobStatusCompleted,
		},
	}
	handler := NewHandler(&stubEnqueuer{}, store, logging.Default())

	req := httptest.NewRequest(http.MethodGet, "/conversations/jobs/job-123", nil)
	req = routeWithJobID(req, "job-123")
	w := httptest.NewRecorder()

	handler.JobStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestHandler_JobStatus_NotFound(t *testing.T) {
	store := &stubJobStore{
		getErr: ErrJobNotFound,
	}
	handler := NewHandler(&stubEnqueuer{}, store, logging.Default())

	req := httptest.NewRequest(http.MethodGet, "/conversations/jobs/job-xyz", nil)
	req = routeWithJobID(req, "job-xyz")
	w := httptest.NewRecorder()

	handler.JobStatus(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func routeWithJobID(req *http.Request, jobID string) *http.Request {
	chiCtx := chi.NewRouteContext()
	chiCtx.URLParams.Add("jobID", jobID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, chiCtx))
}

type stubEnqueuer struct {
	startJobID     string
	messageJobID   string
	startErr       error
	messageErr     error
	lastStartReq   StartRequest
	lastMessageReq MessageRequest
}

func (s *stubEnqueuer) EnqueueStart(ctx context.Context, req StartRequest) (string, error) {
	s.lastStartReq = req
	return s.startJobID, s.startErr
}

func (s *stubEnqueuer) EnqueueMessage(ctx context.Context, req MessageRequest) (string, error) {
	s.lastMessageReq = req
	return s.messageJobID, s.messageErr
}

type stubJobStore struct {
	lastPut *JobRecord
	putErr  error
	getJob  *JobRecord
	getErr  error
}

func (s *stubJobStore) PutPending(ctx context.Context, job *JobRecord) error {
	s.lastPut = job
	return s.putErr
}

func (s *stubJobStore) GetJob(ctx context.Context, jobID string) (*JobRecord, error) {
	if s.getJob != nil {
		return s.getJob, s.getErr
	}
	return nil, s.getErr
}
