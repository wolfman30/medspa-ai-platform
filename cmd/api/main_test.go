package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestSetupMessagingMetricsExposesMetrics(t *testing.T) {
	handler, metrics := setupMessagingMetrics()
	if handler == nil || metrics == nil {
		t.Fatalf("expected non-nil handler and metrics")
	}

	metrics.ObserveInbound("message.received", "ok")

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "medspa_messaging_inbound_webhook_total") {
		t.Fatalf("expected inbound counter to be exported")
	}
}

func TestConnectPostgresPoolEmptyURLReturnsNil(t *testing.T) {
	logger := logging.New("error")
	if pool := connectPostgresPool(context.Background(), "", logger); pool != nil {
		t.Fatalf("expected nil pool for empty URL")
	}
}

func TestSetupConversationSQSPath(t *testing.T) {
	logger := logging.New("error")
	cfg := &appconfig.Config{
		UseMemoryQueue:        false,
		AWSRegion:             "us-east-1",
		AWSAccessKeyID:        "test",
		AWSSecretAccessKey:    "test",
		ConversationQueueURL:  "http://localhost:4566/queue/test",
		ConversationJobsTable: "jobs-table",
	}
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	pub, recorder, updater, memoryQueue := setupConversation(context.Background(), cfg, nil, logger)
	if pub == nil {
		t.Fatalf("expected publisher")
	}
	if recorder == nil || updater == nil {
		t.Fatalf("expected job recorder/updater")
	}
	if memoryQueue != nil {
		t.Fatalf("expected memoryQueue to be nil for SQS path")
	}
}

func TestSetupInlineWorkerDisabled(t *testing.T) {
	logger := logging.New("error")
	cfg := &appconfig.Config{UseMemoryQueue: false}

	worker, _ := setupInlineWorker(context.Background(), cfg, logger, nil, "none", stubJobUpdater{}, nil, nil, nil, nil, nil, nil, nil)
	if worker != nil {
		t.Fatalf("expected no worker when memory queue is disabled")
	}
}

func TestSetupInlineWorkerStartsAndStops(t *testing.T) {
	logger := logging.New("error")
	cfg := &appconfig.Config{
		UseMemoryQueue: true,
		WorkerCount:    1,
		SMSProvider:    "auto",
	}
	memoryQueue := conversation.NewMemoryQueue(2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	worker, _ := setupInlineWorker(ctx, cfg, logger, stubMessenger{}, "no credentials", stubJobUpdater{}, memoryQueue, nil, nil, nil, nil, nil, nil)
	if worker == nil {
		t.Fatalf("expected worker when memory queue is enabled")
	}

	cancel()
	waitForInlineWorker(worker, logger)
}

type stubJobUpdater struct{}

func (stubJobUpdater) MarkCompleted(_ context.Context, _ string, _ *conversation.Response, _ string) error {
	return nil
}

func (stubJobUpdater) MarkFailed(_ context.Context, _ string, _ string) error {
	return nil
}

type stubMessenger struct{}

func (stubMessenger) SendReply(_ context.Context, _ conversation.OutboundReply) error {
	return nil
}
