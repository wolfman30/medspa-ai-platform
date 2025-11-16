package conversation

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestPublisher_EnqueueStart(t *testing.T) {
	queue := &stubQueue{}
	jobs := &stubJobRecorder{}
	publisher := NewPublisher(queue, jobs, logging.Default())

	jobID := "job-123"
	if err := publisher.EnqueueStart(context.Background(), jobID, StartRequest{LeadID: "lead-1"}); err != nil {
		t.Fatalf("enqueue returned error: %v", err)
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
	if payload.ID != jobID {
		t.Fatalf("expected job ID %s, got %s", jobID, payload.ID)
	}
	if payload.Start.LeadID != "lead-1" {
		t.Fatalf("expected LeadID lead-1, got %s", payload.Start.LeadID)
	}
	if !payload.TrackStatus {
		t.Fatalf("expected job tracking enabled by default")
	}
}

func TestPublisher_WithoutJobTracking(t *testing.T) {
	queue := &stubQueue{}
	jobs := &stubJobRecorder{}
	publisher := NewPublisher(queue, jobs, logging.Default())

	if err := publisher.EnqueueMessage(context.Background(), "", MessageRequest{ConversationID: "conv-1"}, WithoutJobTracking()); err != nil {
		t.Fatalf("enqueue returned error: %v", err)
	}

	var payload queuePayload
	if err := json.Unmarshal([]byte(queue.sent[0]), &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if payload.TrackStatus {
		t.Fatalf("expected job tracking disabled")
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

type stubJobRecorder struct {
	jobs []*JobRecord
}

func (s *stubJobRecorder) PutPending(ctx context.Context, job *JobRecord) error {
	s.jobs = append(s.jobs, job)
	return nil
}

func (s *stubJobRecorder) GetJob(ctx context.Context, jobID string) (*JobRecord, error) {
	for _, job := range s.jobs {
		if job.JobID == jobID {
			return job, nil
		}
	}
	return nil, ErrJobNotFound
}
