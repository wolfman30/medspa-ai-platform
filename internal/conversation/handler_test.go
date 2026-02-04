package conversation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/tenancy"
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

	if enqueuer.lastStartJobID != resp.JobID {
		t.Fatalf("expected enqueuer to receive jobID %s, got %s", resp.JobID, enqueuer.lastStartJobID)
	}
}

func TestHandler_Start_PropagatesScheduledFor(t *testing.T) {
	enqueuer := &stubEnqueuer{}
	store := &stubJobStore{}
	handler := NewHandler(enqueuer, store, nil, nil, logging.Default())

	scheduled := "2025-12-01T15:00:00Z"
	body := []byte(fmt.Sprintf(`{"lead_id":"lead-123","intro":"Hi","scheduled_for":"%s"}`, scheduled))

	req := httptest.NewRequest(http.MethodPost, "/conversations/start", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.Start(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d", http.StatusAccepted, w.Code)
	}
	if enqueuer.lastStartReq.Metadata["scheduled_for"] != scheduled {
		t.Fatalf("expected scheduled_for to be set, got %q", enqueuer.lastStartReq.Metadata["scheduled_for"])
	}
}

func TestHandler_Start_InvalidScheduledFor(t *testing.T) {
	handler := NewHandler(&stubEnqueuer{}, &stubJobStore{}, nil, nil, logging.Default())

	body := []byte(`{"lead_id":"lead-123","intro":"Hi","scheduled_for":"not-a-time"}`)
	req := httptest.NewRequest(http.MethodPost, "/conversations/start", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.Start(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid scheduled_for, got %d", w.Code)
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

	if enqueuer.lastMessageJobID != resp.JobID {
		t.Fatalf("expected enqueuer to receive jobID %s, got %s", resp.JobID, enqueuer.lastMessageJobID)
	}
}

func TestHandler_Message_PropagatesScheduledFor(t *testing.T) {
	enqueuer := &stubEnqueuer{}
	store := &stubJobStore{}
	handler := NewHandler(enqueuer, store, nil, nil, logging.Default())

	scheduled := "2025-12-01T15:00:00Z"
	body := []byte(fmt.Sprintf(`{"conversation_id":"conv-123","message":"Hi","scheduled_for":"%s"}`, scheduled))

	req := httptest.NewRequest(http.MethodPost, "/conversations/message", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.Message(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d", http.StatusAccepted, w.Code)
	}
	if enqueuer.lastMessageReq.Metadata["scheduled_for"] != scheduled {
		t.Fatalf("expected scheduled_for to be set, got %q", enqueuer.lastMessageReq.Metadata["scheduled_for"])
	}
}

func TestHandler_Message_InvalidScheduledFor(t *testing.T) {
	handler := NewHandler(&stubEnqueuer{}, &stubJobStore{}, nil, nil, logging.Default())

	body := []byte(`{"conversation_id":"conv","message":"Hi","scheduled_for":"not-a-time"}`)
	req := httptest.NewRequest(http.MethodPost, "/conversations/message", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.Message(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid scheduled_for, got %d", w.Code)
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

func TestHandler_AddKnowledge_AcceptsTitledDocuments(t *testing.T) {
	repo := &stubKnowledgeRepo{}
	rag := &stubRAGIngestor{}
	handler := NewHandler(&stubEnqueuer{}, &stubJobStore{}, repo, rag, logging.Default())

	payload := map[string]any{
		"documents": []map[string]any{
			{"title": "Doc 1", "content": "Content 1"},
			{"title": "Doc 2", "content": "Content 2"},
		},
	}
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
	if !strings.Contains(repo.appended["clinic-1"][0], "Doc 1") || !strings.Contains(repo.appended["clinic-1"][0], "Content 1") {
		t.Fatalf("expected titled doc conversion, got %q", repo.appended["clinic-1"][0])
	}
	if rag.calls != 1 {
		t.Fatalf("expected rag ingestor to be called once, got %d", rag.calls)
	}
}

func TestHandler_AddKnowledge_AllowsStoreWhenRAGMissing(t *testing.T) {
	repo := &stubKnowledgeRepo{}
	handler := NewHandler(&stubEnqueuer{}, &stubJobStore{}, repo, nil, logging.Default())

	payload := map[string]any{"documents": []string{"Doc A"}}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/knowledge/clinic-1", bytes.NewReader(body))
	req = routeWithClinicID(req, "clinic-1")
	w := httptest.NewRecorder()

	handler.AddKnowledge(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	if len(repo.appended["clinic-1"]) != 1 {
		t.Fatalf("expected repo to store docs, got %#v", repo.appended)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if embedded, ok := resp["embedded"].(bool); !ok || embedded {
		t.Fatalf("expected embedded=false, got %#v", resp["embedded"])
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
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, chiCtx)
	ctx = tenancy.WithOrgID(ctx, clinicID)
	return req.WithContext(ctx)
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

// --- Booking callback handler tests ---

type callbackMessenger struct {
	replies []OutboundReply
	mu      sync.Mutex
}

func (m *callbackMessenger) SendReply(ctx context.Context, reply OutboundReply) error {
	m.mu.Lock()
	m.replies = append(m.replies, reply)
	m.mu.Unlock()
	return nil
}

func setupBookingCallbackTest(t *testing.T) (*BookingCallbackHandler, *leads.InMemoryRepository, *callbackMessenger, *leads.Lead) {
	t.Helper()
	repo := leads.NewInMemoryRepository()
	lead, err := repo.Create(context.Background(), &leads.CreateLeadRequest{
		OrgID:   "org-1",
		Name:    "Test Patient",
		Phone:   "+15551234567",
		Source:  "sms",
		Message: "Botox",
	})
	if err != nil {
		t.Fatalf("create lead: %v", err)
	}
	// Set booking session ID on lead
	if err := repo.UpdateBookingSession(context.Background(), lead.ID, leads.BookingSessionUpdate{
		SessionID: "session-abc",
		Platform:  "moxie",
	}); err != nil {
		t.Fatalf("update booking session: %v", err)
	}

	messenger := &callbackMessenger{}
	handler := NewBookingCallbackHandler(repo, messenger, logging.Default())
	return handler, repo, messenger, lead
}

func TestBookingCallback_Success(t *testing.T) {
	handler, repo, messenger, lead := setupBookingCallbackTest(t)

	body := `{"sessionId":"session-abc","state":"completed","outcome":"success","confirmationDetails":{"confirmationNumber":"CONF-123","appointmentTime":"Feb 10 at 3:30 PM"}}`
	req := httptest.NewRequest("POST", "/webhooks/booking/callback?orgId=org-1&from=%2B15559999999", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.Handle(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify lead updated
	updated, _ := repo.GetByID(context.Background(), "org-1", lead.ID)
	if updated.BookingOutcome != "success" {
		t.Errorf("outcome = %q, want success", updated.BookingOutcome)
	}
	if updated.BookingConfirmationNumber != "CONF-123" {
		t.Errorf("confirmation = %q, want CONF-123", updated.BookingConfirmationNumber)
	}
	if updated.BookingCompletedAt == nil {
		t.Error("expected BookingCompletedAt to be set")
	}

	// Verify SMS sent
	messenger.mu.Lock()
	defer messenger.mu.Unlock()
	if len(messenger.replies) != 1 {
		t.Fatalf("expected 1 SMS, got %d", len(messenger.replies))
	}
	sms := messenger.replies[0]
	if !strings.Contains(sms.Body, "confirmed") {
		t.Errorf("SMS body = %q, want confirmation message", sms.Body)
	}
	if !strings.Contains(sms.Body, "CONF-123") {
		t.Errorf("SMS body = %q, want confirmation number", sms.Body)
	}
	if sms.To != "+15551234567" {
		t.Errorf("SMS to = %q, want patient phone", sms.To)
	}
	if sms.From != "+15559999999" {
		t.Errorf("SMS from = %q, want clinic number", sms.From)
	}
}

func TestBookingCallback_PaymentFailed(t *testing.T) {
	handler, repo, messenger, lead := setupBookingCallbackTest(t)

	body := `{"sessionId":"session-abc","state":"failed","outcome":"payment_failed"}`
	req := httptest.NewRequest("POST", "/webhooks/booking/callback?orgId=org-1&from=%2B15559999999", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.Handle(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	updated, _ := repo.GetByID(context.Background(), "org-1", lead.ID)
	if updated.BookingOutcome != "payment_failed" {
		t.Errorf("outcome = %q, want payment_failed", updated.BookingOutcome)
	}

	messenger.mu.Lock()
	defer messenger.mu.Unlock()
	if len(messenger.replies) != 1 {
		t.Fatalf("expected 1 SMS, got %d", len(messenger.replies))
	}
	if !strings.Contains(messenger.replies[0].Body, "payment didn't go through") {
		t.Errorf("SMS = %q, want payment failed message", messenger.replies[0].Body)
	}
}

func TestBookingCallback_Timeout(t *testing.T) {
	handler, _, messenger, _ := setupBookingCallbackTest(t)

	body := `{"sessionId":"session-abc","state":"completed","outcome":"timeout"}`
	req := httptest.NewRequest("POST", "/webhooks/booking/callback?orgId=org-1&from=%2B15559999999", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.Handle(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	messenger.mu.Lock()
	defer messenger.mu.Unlock()
	if len(messenger.replies) != 1 {
		t.Fatalf("expected 1 SMS, got %d", len(messenger.replies))
	}
	if !strings.Contains(messenger.replies[0].Body, "expired") {
		t.Errorf("SMS = %q, want expiration message", messenger.replies[0].Body)
	}
}

func TestBookingCallback_InvalidJSON(t *testing.T) {
	handler, _, _, _ := setupBookingCallbackTest(t)

	req := httptest.NewRequest("POST", "/webhooks/booking/callback?orgId=org-1", strings.NewReader("not json"))
	rr := httptest.NewRecorder()

	handler.Handle(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestBookingCallback_MissingSessionID(t *testing.T) {
	handler, _, _, _ := setupBookingCallbackTest(t)

	body := `{"state":"completed","outcome":"success"}`
	req := httptest.NewRequest("POST", "/webhooks/booking/callback?orgId=org-1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.Handle(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestBookingCallback_UnknownSession(t *testing.T) {
	handler, _, _, _ := setupBookingCallbackTest(t)

	body := `{"sessionId":"session-unknown","state":"completed","outcome":"success"}`
	req := httptest.NewRequest("POST", "/webhooks/booking/callback?orgId=org-1&from=%2B15559999999", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.Handle(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}
