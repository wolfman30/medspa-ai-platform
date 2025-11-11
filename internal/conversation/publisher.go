package conversation

import (
	"context"
	"fmt"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// Publisher enqueues conversation jobs for asynchronous processing.
type Publisher struct {
	queue  queueClient
	logger *logging.Logger
}

// NewPublisher creates a queue-backed publisher.
func NewPublisher(queue queueClient, logger *logging.Logger) *Publisher {
	if queue == nil {
		panic("conversation: queue cannot be nil")
	}
	if logger == nil {
		logger = logging.Default()
	}
	return &Publisher{
		queue:  queue,
		logger: logger,
	}
}

// EnqueueStart publishes a StartConversation job.
func (p *Publisher) EnqueueStart(ctx context.Context, jobID string, req StartRequest) error {
	return p.enqueue(ctx, jobTypeStart, jobID, req, MessageRequest{})
}

// EnqueueMessage publishes a ProcessMessage job.
func (p *Publisher) EnqueueMessage(ctx context.Context, jobID string, req MessageRequest) error {
	return p.enqueue(ctx, jobTypeMessage, jobID, StartRequest{}, req)
}

func (p *Publisher) enqueue(ctx context.Context, kind jobType, jobID string, start StartRequest, message MessageRequest) error {
	if ctx == nil {
		ctx = context.Background()
	}

	payload, body, err := encodePayload(kind, jobID, start, message)
	if err != nil {
		return err
	}

	if err := p.queue.Send(ctx, body); err != nil {
		return fmt.Errorf("conversation: failed to enqueue job: %w", err)
	}

	p.logger.Debug("conversation job enqueued", "job_id", payload.ID, "kind", kind)
	return nil
}
