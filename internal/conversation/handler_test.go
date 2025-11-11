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
	enqueuer := &stubEnqueuer{}
	store := &stubJobStore{}
	handler := NewHandler(enqueuer, store, nil, nil, logging.Default())

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

	if enqueuer.lastStartReq.LeadID != payload.LeadID {
		t.Fatalf("expected LeadID %s, got %s", payload.LeadID, enqueuer.lastStartReq.LeadID)
	}

	if store.lastPut == nil || store.lastPut.JobID != resp.JobID || store.lastPut.RequestType != jobTypeStart {
		t.Fatalf("expected job store to capture pending job, got %#v", store.lastPut)
	}

	if enqueuer.lastStartJobID != resp.JobID {
		t.Fatalf("expected enqueuer to receive jobID %s, got %s", resp.JobID, enqueuer.lastStartJobID)
	}
}

func TestHandler_Message_AcceptsJob(t *testing.T) {
	enqueuer := &stubEnqueuer{}
	store := &stubJobStore{}
	handler := NewHandler(enqueuer, store, nil, nil, logging.Default())

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

	if enqueuer.lastMessageReq.ConversationID != payload.ConversationID {
		t.Fatalf("expected conversation ID %s, got %s", payload.ConversationID, enqueuer.lastMessageReq.ConversationID)
	}

	if store.lastPut == nil || store.lastPut.MessageRequest == nil || store.lastPut.MessageRequest.ConversationID != payload.ConversationID {
		t.Fatalf("expected job store to capture message job, got %#v", store.lastPut)
	}

	if enqueuer.lastMessageJobID != resp.JobID {
		t.Fatalf("expected enqueuer to receive jobID %s, got %s", resp.JobID, enqueuer.lastMessageJobID)
	}
}

func TestHandler_Start_InvalidJSON(t *testing.T) {
	handler := NewHandler(&stubEnqueuer{}, &stubJobStore{}, nil, nil, logging.Default())

	req := httptest.NewRequest(http.MethodPost, "/conversations/start", strings.NewReader("{"))
	w := httptest.NewRecorder()

	handler.Start(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandler_Message_InvalidJSON(t *testing.T) {
	handler := NewHandler(&stubEnqueuer{}, &stubJobStore{}, nil, nil, logging.Default())

	req := httptest.NewRequest(http.MethodPost, "/conversations/message", strings.NewReader("{"))
	w := httptest.NewRecorder()

	handler.Message(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandler_Start_EnqueueError(t *testing.T) {
	handler := NewHandler(&stubEnqueuer{startErr: errors.New("boom")}, &stubJobStore{}, nil, nil, logging.Default())

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
	handler := NewHandler(&stubEnqueuer{messageErr: errors.New("boom")}, &stubJobStore{}, nil, nil, logging.Default())

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
	handler := NewHandler(&stubEnqueuer{}, store, nil, nil, logging.Default())

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
	handler := NewHandler(&stubEnqueuer{}, store, nil, nil, logging.Default())

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
	startErr         error
	messageErr       error
	lastStartReq     StartRequest
	lastMessageReq   MessageRequest
	lastStartJobID   string
	lastMessageJobID string
}

func (s *stubEnqueuer) EnqueueStart(ctx context.Context, jobID string, req StartRequest, opts ...PublishOption) error {
	s.lastStartReq = req
	s.lastStartJobID = jobID
	return s.startErr
}

func (s *stubEnqueuer) EnqueueMessage(ctx context.Context, jobID string, req MessageRequest, opts ...PublishOption) error {
	s.lastMessageReq = req
	s.lastMessageJobID = jobID
	return s.messageErr
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

func TestHandler_AddKnowledge_Success(t *testing.T) {
	repo := &stubKnowledgeRepo{}
	rag := &stubRAGIngestor{}
	handler := NewHandler(&stubEnqueuer{}, &stubJobStore{}, repo, rag, logging.Default())

	payload := map[string]any{"documents": []string{"Doc A", "Doc B"}}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/knowledge/clinic-1", bytes.NewReader(body))
	req = routeWithClinicID(req, "clinic-1")
	w := httptest.NewRecorder()

	handler.AddKnowledge(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	if len(repo.appended["clinic-1"]) != 2 {
		t.Fatalf("expected repo to store docs, got %#v", repo.appended)
	}
	if rag.calls != 1 {
		t.Fatalf("expected rag ingestor to be called once, got %d", rag.calls)
	}
}

func TestHandler_AddKnowledge_InvalidBody(t *testing.T) {
	handler := NewHandler(&stubEnqueuer{}, &stubJobStore{}, nil, nil, logging.Default())

	req := httptest.NewRequest(http.MethodPost, "/knowledge/clinic", strings.NewReader("{}"))
	req = routeWithClinicID(req, "clinic")
	w := httptest.NewRecorder()

	handler.AddKnowledge(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when repo not configured, got %d", w.Code)
	}
}

func routeWithClinicID(req *http.Request, clinicID string) *http.Request {
	chiCtx := chi.NewRouteContext()
	chiCtx.URLParams.Add("clinicID", clinicID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, chiCtx))
}

type stubKnowledgeRepo struct {
	appended map[string][]string
	err      error
}

func (s *stubKnowledgeRepo) AppendDocuments(ctx context.Context, clinicID string, docs []string) error {
	if s.err != nil {
		return s.err
	}
	if s.appended == nil {
		s.appended = make(map[string][]string)
	}
	s.appended[clinicID] = append(s.appended[clinicID], docs...)
	return nil
}

func (s *stubKnowledgeRepo) GetDocuments(ctx context.Context, clinicID string) ([]string, error) {
	return s.appended[clinicID], nil
}

func (s *stubKnowledgeRepo) LoadAll(ctx context.Context) (map[string][]string, error) {
	return s.appended, nil
}

type stubRAGIngestor struct {
	calls int
}

func (s *stubRAGIngestor) AddDocuments(ctx context.Context, clinicID string, docs []string) error {
	s.calls++
	return nil
}
