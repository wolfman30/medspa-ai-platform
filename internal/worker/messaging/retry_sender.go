package messagingworker

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/telnyxclient"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

type retryStore interface {
	ListRetryCandidates(ctx context.Context, limit int, maxAttempts int) ([]messaging.MessageRecord, error)
	ScheduleRetry(ctx context.Context, q messaging.Querier, id uuid.UUID, status string, nextRetry time.Time) error
	UpdateMessageStatus(ctx context.Context, providerMessageID, status string, deliveredAt, failedAt *time.Time) error
}

type telnyxSender interface {
	SendMessage(ctx context.Context, req telnyxclient.SendMessageRequest) (*telnyxclient.MessageResponse, error)
}

// RetrySender retries failed outbound SMS until max attempts.
type RetrySender struct {
	store       retryStore
	telnyx      telnyxSender
	logger      *logging.Logger
	maxAttempts int
	baseDelay   time.Duration
	interval    time.Duration
	batchSize   int
}

func NewRetrySender(store retryStore, telnyx telnyxSender, logger *logging.Logger) *RetrySender {
	if logger == nil {
		logger = logging.Default()
	}
	return &RetrySender{
		store:       store,
		telnyx:      telnyx,
		logger:      logger,
		maxAttempts: 5,
		baseDelay:   5 * time.Minute,
		interval:    1 * time.Minute,
		batchSize:   25,
	}
}

func (r *RetrySender) WithMaxAttempts(n int) *RetrySender {
	if n > 0 {
		r.maxAttempts = n
	}
	return r
}

func (r *RetrySender) WithBaseDelay(d time.Duration) *RetrySender {
	if d > 0 {
		r.baseDelay = d
	}
	return r
}

func (r *RetrySender) WithInterval(d time.Duration) *RetrySender {
	if d > 0 {
		r.interval = d
	}
	return r
}

func (r *RetrySender) WithBatchSize(n int) *RetrySender {
	if n > 0 {
		r.batchSize = n
	}
	return r
}

func (r *RetrySender) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	r.drain(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.drain(ctx)
		}
	}
}

func (r *RetrySender) drain(ctx context.Context) {
	if r.store == nil || r.telnyx == nil {
		return
	}
	msgs, err := r.store.ListRetryCandidates(ctx, r.batchSize, r.maxAttempts)
	if err != nil {
		r.logger.Error("retry fetch failed", "error", err)
		return
	}
	for _, m := range msgs {
		req := telnyxclient.SendMessageRequest{
			From: m.From,
			To:   m.To,
			Body: m.Body,
		}
		if len(m.Media) > 0 {
			req.MediaURLs = m.Media
		}
		resp, err := r.telnyx.SendMessage(ctx, req)
		if err != nil {
			next := r.nextDelay(m.SendAttempts)
			if err := r.store.ScheduleRetry(ctx, nil, m.ID, "retry_pending", time.Now().Add(next)); err != nil {
				r.logger.Error("schedule retry failed", "error", err, "message_id", m.ID)
			}
			continue
		}
		status := resp.Status
		if status == "" {
			status = "queued"
		}
		if err := r.store.UpdateMessageStatus(ctx, resp.ID, status, nil, nil); err != nil {
			r.logger.Error("update message status failed", "error", err, "provider_message_id", resp.ID)
		}
	}
}

func (r *RetrySender) nextDelay(attempts int) time.Duration {
	delay := r.baseDelay * time.Duration(1<<attempts)
	if delay > 24*time.Hour {
		delay = 24 * time.Hour
	}
	return delay
}
