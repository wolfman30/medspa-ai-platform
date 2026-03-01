package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wolfman30/medspa-ai-platform/internal/api/router"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/internal/payments"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

var _ conversation.Enqueuer = (*stubEnqueuer)(nil)
var _ messaging.OrgResolver = (*messaging.StaticOrgResolver)(nil)
var _ payments.OrgNumberResolver = (*messaging.StaticOrgResolver)(nil)
var _ leads.Repository = (*leads.InMemoryRepository)(nil)

type stubEnqueuer struct{}

func (s *stubEnqueuer) EnqueueStart(ctx context.Context, jobID string, req conversation.StartRequest, opts ...conversation.PublishOption) error {
	return errors.New("not implemented")
}

func (s *stubEnqueuer) EnqueueMessage(ctx context.Context, jobID string, req conversation.MessageRequest, opts ...conversation.PublishOption) error {
	return errors.New("not implemented")
}

func TestRouterConfigBuildsWithoutPanicking(t *testing.T) {
	logger := logging.Default()
	leadsRepo := leads.NewInMemoryRepository()
	leadsHandler := leads.NewHandler(leadsRepo, logger)

	msgHandler := messaging.NewHandler(
		"",
		&stubEnqueuer{},
		messaging.NewStaticOrgResolver(map[string]string{"+15555550123": "org_test"}),
		nil,
		leadsRepo,
		logger,
	)

	cfg := &router.Config{
		Logger:           logger,
		LeadsHandler:     leadsHandler,
		MessagingHandler: msgHandler,
		HasSMSProvider:   true,
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("router.New panicked: %v", r)
		}
	}()

	h := router.New(cfg)
	if h == nil {
		t.Fatal("expected non-nil router")
	}

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected /ready status 200, got %d", rr.Code)
	}
}
