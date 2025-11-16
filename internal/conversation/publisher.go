package conversation

import (
	"context"
	"fmt"

	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// Publisher enqueues conversation jobs for asynchronous processing.
type Publisher struct {
	queue  queueClient
	jobs   JobRecorder
	logger *logging.Logger
}

// NewPublisher creates a queue-backed publisher.
func NewPublisher(queue queueClient, jobs JobRecorder, logger *logging.Logger) *Publisher {
	if queue == nil {
		panic("conversation: queue cannot be nil")
	}
	if jobs == nil {
		panic("conversation: jobs cannot be nil")
	}
	if logger == nil {
		logger = logging.Default()
	}
	return &Publisher{
		queue:  queue,
		jobs:   jobs,
		logger: logger,
	}
}

// EnqueueStart publishes a StartConversation job.
func (p *Publisher) EnqueueStart(ctx context.Context, jobID string, req StartRequest, opts ...PublishOption) error {
	payload := queuePayload{
		ID:    jobID,
		Kind:  jobTypeStart,
		Start: req,
	}
	return p.enqueue(ctx, payload, opts...)
}

// EnqueueMessage publishes a ProcessMessage job.
func (p *Publisher) EnqueueMessage(ctx context.Context, jobID string, req MessageRequest, opts ...PublishOption) error {
	payload := queuePayload{
		ID:      jobID,
		Kind:    jobTypeMessage,
		Message: req,
	}
	return p.enqueue(ctx, payload, opts...)
}

// EnqueuePaymentSucceeded publishes a payment event for downstream processors.
func (p *Publisher) EnqueuePaymentSucceeded(ctx context.Context, event events.PaymentSucceededV1) error {
	payload := queuePayload{
		ID:      event.EventID,
		Kind:    jobTypePayment,
		Payment: &event,
	}
	return p.enqueue(ctx, payload, WithoutJobTracking())
}

func (p *Publisher) enqueue(ctx context.Context, payload queuePayload, opts ...PublishOption) error {
	if ctx == nil {
		ctx = context.Background()
	}

	payload.TrackStatus = true
	for _, opt := range opts {
		opt(&payload)
	}

	var err error
	payload, body, err := encodePayload(payload)
	if err != nil {
		return err
	}

	// Create job record in database before sending to queue
	if payload.TrackStatus {
		jobRecord := &JobRecord{
			JobID:       payload.ID,
			RequestType: payload.Kind,
		}
		switch payload.Kind {
		case jobTypeStart:
			jobRecord.StartRequest = &payload.Start
		case jobTypeMessage:
			jobRecord.MessageRequest = &payload.Message
		}
		if err := p.jobs.PutPending(ctx, jobRecord); err != nil {
			return fmt.Errorf("conversation: failed to create job record: %w", err)
		}
		p.logger.Debug("conversation job record created", "job_id", payload.ID, "kind", payload.Kind)
	}

	if err := p.queue.Send(ctx, body); err != nil {
		return fmt.Errorf("conversation: failed to enqueue job: %w", err)
	}

	p.logger.Debug("conversation job enqueued", "job_id", payload.ID, "kind", payload.Kind, "track_status", payload.TrackStatus)
	return nil
}
