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

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestHandler_Start_AcceptsJob(t *testing.T) {
	enqueuer := &stubEnqueuer{
		startJobID: "job-start-1",
	}
	handler := NewHandler(enqueuer, logging.Default())

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
}

func TestHandler_Message_AcceptsJob(t *testing.T) {
	enqueuer := &stubEnqueuer{
		messageJobID: "job-msg-1",
	}
	handler := NewHandler(enqueuer, logging.Default())

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
}

func TestHandler_Start_InvalidJSON(t *testing.T) {
	handler := NewHandler(&stubEnqueuer{}, logging.Default())

	req := httptest.NewRequest(http.MethodPost, "/conversations/start", strings.NewReader("{"))
	w := httptest.NewRecorder()

	handler.Start(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandler_Message_InvalidJSON(t *testing.T) {
	handler := NewHandler(&stubEnqueuer{}, logging.Default())

	req := httptest.NewRequest(http.MethodPost, "/conversations/message", strings.NewReader("{"))
	w := httptest.NewRecorder()

	handler.Message(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandler_Start_EnqueueError(t *testing.T) {
	handler := NewHandler(&stubEnqueuer{startErr: errors.New("boom")}, logging.Default())

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
	handler := NewHandler(&stubEnqueuer{messageErr: errors.New("boom")}, logging.Default())

	payload := MessageRequest{ConversationID: "conv", Message: "Hi"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/conversations/message", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.Message(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected %d, got %d", http.StatusInternalServerError, w.Code)
	}
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
