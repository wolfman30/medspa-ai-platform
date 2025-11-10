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

// EnqueueStart publishes a StartConversation job and returns the job ID.
func (p *Publisher) EnqueueStart(ctx context.Context, req StartRequest) (string, error) {
	return p.enqueue(ctx, jobTypeStart, req, MessageRequest{})
}

// EnqueueMessage publishes a ProcessMessage job and returns the job ID.
func (p *Publisher) EnqueueMessage(ctx context.Context, req MessageRequest) (string, error) {
	return p.enqueue(ctx, jobTypeMessage, StartRequest{}, req)
}

func (p *Publisher) enqueue(ctx context.Context, kind jobType, start StartRequest, message MessageRequest) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	payload, body, err := encodePayload(kind, start, message)
	if err != nil {
		return "", err
	}

	if err := p.queue.Send(ctx, body); err != nil {
		return "", fmt.Errorf("conversation: failed to enqueue job: %w", err)
	}

	p.logger.Debug("conversation job enqueued", "job_id", payload.ID, "kind", kind)
	return payload.ID, nil
}
