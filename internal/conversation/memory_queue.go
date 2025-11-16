package conversation

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// MemoryQueue is a queueClient backed by an in-memory buffered channel.
type MemoryQueue struct {
	ch chan queueMessage
}

// NewMemoryQueue creates a MemoryQueue with the provided buffer capacity.
func NewMemoryQueue(buffer int) *MemoryQueue {
	if buffer <= 0 {
		buffer = 128
	}
	return &MemoryQueue{
		ch: make(chan queueMessage, buffer),
	}
}

// Send enqueues a payload or blocks until ctx is done.
func (q *MemoryQueue) Send(ctx context.Context, body string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	msg := queueMessage{
		ID:            uuid.NewString(),
		Body:          body,
		ReceiptHandle: uuid.NewString(),
	}

	select {
	case q.ch <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Receive blocks until a message is available, ctx is done, or waitSeconds elapses.
func (q *MemoryQueue) Receive(ctx context.Context, maxMessages int, waitSeconds int) ([]queueMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if maxMessages <= 0 {
		maxMessages = 1
	}

	var timer *time.Timer
	if waitSeconds > 0 {
		timer = time.NewTimer(time.Duration(waitSeconds) * time.Second)
		defer timer.Stop()
	}

	for {
		if timer == nil {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case msg := <-q.ch:
				return q.collect(ctx, msg, maxMessages), nil
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, nil
		case msg := <-q.ch:
			return q.collect(ctx, msg, maxMessages), nil
		}
	}
}

// Delete is a no-op for the in-memory queue.
func (q *MemoryQueue) Delete(_ context.Context, _ string) error {
	return nil
}

func (q *MemoryQueue) collect(ctx context.Context, first queueMessage, max int) []queueMessage {
	if ctx == nil {
		ctx = context.Background()
	}
	messages := make([]queueMessage, 0, max)
	messages = append(messages, first)

	for len(messages) < max {
		select {
		case <-ctx.Done():
			return messages
		case msg := <-q.ch:
			messages = append(messages, msg)
		default:
			return messages
		}
	}
	return messages
}
