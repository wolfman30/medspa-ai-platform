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
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestHandler_Start_Success(t *testing.T) {
	timestamp := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	service := &stubConversationService{
		startResponse: &Response{
			ConversationID: "conv-123",
			Message:        "Welcome to MedSpa AI",
			Timestamp:      timestamp,
		},
	}
	handler := NewHandler(service, logging.Default())

	reqPayload := StartRequest{
		LeadID: "lead-42",
		Intro:  "Need a consultation",
		Source: "web",
	}
	body, err := json.Marshal(reqPayload)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/conversations/start", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.Start(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, w.Code)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.ConversationID != "conv-123" {
		t.Errorf("expected conversation ID conv-123, got %s", resp.ConversationID)
	}

	if resp.Message == "" {
		t.Error("expected response message to be populated")
	}
}

func TestHandler_Message_Success(t *testing.T) {
	timestamp := time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC)
	service := &stubConversationService{
		messageResponse: &Response{
			ConversationID: "conv-xyz",
			Message:        "You said: Hello. A full AI response will arrive soon.",
			Timestamp:      timestamp,
		},
	}
	handler := NewHandler(service, logging.Default())

	reqPayload := MessageRequest{
		ConversationID: "conv-xyz",
		Message:        "Hello",
	}
	body, err := json.Marshal(reqPayload)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/conversations/message", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.Message(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Message != service.messageResponse.Message {
		t.Errorf("expected message %q, got %q", service.messageResponse.Message, resp.Message)
	}
}

func TestHandler_Start_InvalidJSON(t *testing.T) {
	service := &stubConversationService{
		startResponse: &Response{},
	}
	handler := NewHandler(service, logging.Default())

	req := httptest.NewRequest(http.MethodPost, "/conversations/start", strings.NewReader("{"))
	w := httptest.NewRecorder()

	handler.Start(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandler_Message_InvalidJSON(t *testing.T) {
	service := &stubConversationService{
		messageResponse: &Response{},
	}
	handler := NewHandler(service, logging.Default())

	req := httptest.NewRequest(http.MethodPost, "/conversations/message", strings.NewReader("{"))
	w := httptest.NewRecorder()

	handler.Message(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandler_Start_ServiceError(t *testing.T) {
	service := &stubConversationService{
		startErr: errors.New("service down"),
	}
	handler := NewHandler(service, logging.Default())

	reqPayload := StartRequest{
		LeadID: "lead-42",
		Intro:  "Need help",
	}
	body, err := json.Marshal(reqPayload)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/conversations/start", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.Start(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestHandler_Message_ServiceError(t *testing.T) {
	service := &stubConversationService{
		messageErr: errors.New("service down"),
	}
	handler := NewHandler(service, logging.Default())

	reqPayload := MessageRequest{
		ConversationID: "conv-xyz",
		Message:        "Hi",
	}
	body, err := json.Marshal(reqPayload)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/conversations/message", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.Message(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

type stubConversationService struct {
	startResponse   *Response
	startErr        error
	messageResponse *Response
	messageErr      error
	lastStartReq    StartRequest
	lastMessageReq  MessageRequest
}

func (s *stubConversationService) StartConversation(ctx context.Context, req StartRequest) (*Response, error) {
	s.lastStartReq = req
	return s.startResponse, s.startErr
}

func (s *stubConversationService) ProcessMessage(ctx context.Context, req MessageRequest) (*Response, error) {
	s.lastMessageReq = req
	return s.messageResponse, s.messageErr
}
