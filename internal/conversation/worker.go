package conversation

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
)

// appendTranscript persists a message to both Redis (real-time) and PostgreSQL (long-term).
func (w *Worker) appendTranscript(ctx context.Context, conversationID string, msg SMSTranscriptMessage) {
	if w == nil || strings.TrimSpace(conversationID) == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// Append to Redis (real-time, ephemeral)
	if w.transcript != nil {
		if err := w.transcript.Append(ctx, conversationID, msg); err != nil {
			w.logger.Warn("failed to append sms transcript to Redis", "error", err, "conversation_id", conversationID)
		}
	}

	// Persist to PostgreSQL (long-term history)
	if w.convStore != nil {
		if err := w.convStore.AppendMessage(ctx, conversationID, msg); err != nil {
			w.logger.Warn("failed to persist message to database", "error", err, "conversation_id", conversationID)
		}
	}
}

// isOptedOut checks whether a recipient has unsubscribed from SMS for a clinic.
func (w *Worker) isOptedOut(ctx context.Context, orgID string, recipient string) bool {
	if w == nil || w.optOutChecker == nil {
		return false
	}
	orgID = strings.TrimSpace(orgID)
	recipient = strings.TrimSpace(recipient)
	if orgID == "" || recipient == "" {
		return false
	}
	clinicID, err := uuid.Parse(orgID)
	if err != nil {
		w.logger.Warn("opt-out check skipped: invalid org id", "org_id", orgID)
		return false
	}
	unsubscribed, err := w.optOutChecker.IsUnsubscribed(ctx, clinicID, recipient)
	if err != nil {
		w.logger.Warn("opt-out check failed", "error", err, "org_id", orgID)
		return false
	}
	if unsubscribed {
		w.logger.Info("suppressing sms for opted-out recipient", "org_id", orgID, "to", recipient)
	}
	return unsubscribed
}

// clinicName returns the display name for a clinic, or empty string on failure.
func (w *Worker) clinicName(ctx context.Context, orgID string) string {
	cfg := w.clinicConfig(ctx, orgID)
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Name)
}

// clinicConfig loads the clinic configuration from the store.
func (w *Worker) clinicConfig(ctx context.Context, orgID string) *clinic.Config {
	if w == nil || w.clinicStore == nil {
		return nil
	}
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cfg, err := w.clinicStore.Get(ctx, orgID)
	if err != nil {
		w.logger.Warn("failed to load clinic config", "error", err, "org_id", orgID)
		return nil
	}
	return cfg
}

// Start launches worker goroutines until ctx is cancelled.
func (w *Worker) Start(ctx context.Context) {
	for i := 0; i < w.cfg.workers; i++ {
		w.wg.Add(1)
		go w.run(ctx, i+1)
	}
}

// Wait blocks until all worker goroutines exit.
func (w *Worker) Wait() {
	w.wg.Wait()
}

// run is the main loop for a single worker goroutine. It polls the queue
// for messages and dispatches each one via handleMessage.
func (w *Worker) run(ctx context.Context, workerID int) {
	defer w.wg.Done()
	w.logger.Debug("conversation worker started", "worker_id", workerID)

	backoff := time.Second

	for {
		select {
		case <-ctx.Done():
			w.logger.Debug("conversation worker stopping", "worker_id", workerID)
			return
		default:
		}

		messages, err := w.queue.Receive(ctx, w.cfg.receiveBatchSize, w.cfg.receiveWaitSecs)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			w.logger.Error("failed to receive conversation jobs", "error", err, "worker_id", workerID)
			time.Sleep(backoff)
			if backoff < 5*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second

		for _, msg := range messages {
			w.handleMessage(ctx, msg)
		}
	}
}
