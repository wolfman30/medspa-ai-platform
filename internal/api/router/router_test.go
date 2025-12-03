package router

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()

	logger := logging.Default()
	leadRepo := leads.NewInMemoryRepository()
	leadsHandler := leads.NewHandler(leadRepo, logger)
	publisher := &noopPublisher{}
	resolver := messaging.NewStaticOrgResolver(map[string]string{
		"+10000000000": "org-test",
	})
	messagingHandler := messaging.NewHandler("", publisher, resolver, nil, leadRepo, logger)

	cfg := &Config{
		Logger:           logger,
		LeadsHandler:     leadsHandler,
		MessagingHandler: messagingHandler,
	}

	return New(cfg)
}

func TestRouterHealthEndpoint(t *testing.T) {
	router := newTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got %q", resp["status"])
	}
}

func TestRouterLeadsWebEndpoint(t *testing.T) {
	router := newTestRouter(t)

	payload := leads.CreateLeadRequest{
		Name:    "Router Test",
		Email:   "router@example.com",
		Phone:   "+12223334444",
		Message: "Interested in services",
		Source:  "test",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/leads/web", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Org-Id", "org-test")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, rr.Code)
	}

	var created leads.Lead
	if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if created.Email != payload.Email {
		t.Errorf("expected email %s, got %s", payload.Email, created.Email)
	}
}

func TestRouterMessagingWebhookEndpoint(t *testing.T) {
	router := newTestRouter(t)

	form := url.Values{}
	form.Set("MessageSid", "SM123")
	form.Set("From", "+10000000000")
	form.Set("To", "+10000000000")
	form.Set("Body", "Hi there")

	req := httptest.NewRequest(http.MethodPost, "/messaging/twilio/webhook", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	if ct := rr.Header().Get("Content-Type"); ct != "application/xml" {
		t.Fatalf("expected XML response, got %s", ct)
	}
}

type noopPublisher struct{}

func (noopPublisher) EnqueueStart(ctx context.Context, jobID string, req conversation.StartRequest, opts ...conversation.PublishOption) error {
	return nil
}

func (noopPublisher) EnqueueMessage(ctx context.Context, jobID string, req conversation.MessageRequest, opts ...conversation.PublishOption) error {
	return nil
}
