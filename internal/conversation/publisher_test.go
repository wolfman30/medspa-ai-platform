package conversation

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestPublisher_EnqueueStart(t *testing.T) {
	queue := &stubQueue{}
	publisher := NewPublisher(queue, logging.Default())

	jobID, err := publisher.EnqueueStart(context.Background(), StartRequest{LeadID: "lead-1"})
	if err != nil {
		t.Fatalf("enqueue returned error: %v", err)
	}
	if jobID == "" {
		t.Fatal("expected job ID to be set")
	}
	if len(queue.sent) != 1 {
		t.Fatalf("expected 1 message, got %d", len(queue.sent))
	}

	var payload queuePayload
	if err := json.Unmarshal([]byte(queue.sent[0]), &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if payload.Kind != jobTypeStart {
		t.Fatalf("expected jobType start, got %s", payload.Kind)
	}
	if payload.Start.LeadID != "lead-1" {
		t.Fatalf("expected LeadID lead-1, got %s", payload.Start.LeadID)
	}
}

type stubQueue struct {
	sent []string
}

func (s *stubQueue) Send(ctx context.Context, body string) error {
	s.sent = append(s.sent, body)
	return nil
}

func (s *stubQueue) Receive(ctx context.Context, maxMessages int, waitSeconds int) ([]queueMessage, error) {
	return nil, context.Canceled
}

func (s *stubQueue) Delete(ctx context.Context, receiptHandle string) error {
	return nil
}
