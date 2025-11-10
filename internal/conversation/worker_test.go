package conversation

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestWorkerProcessesMessages(t *testing.T) {
	queue := newScriptedQueue()
	service := &recordingService{}
	worker := NewWorker(service, queue, logging.Default(), WithWorkerCount(1), WithReceiveBatchSize(1), WithReceiveWaitSeconds(0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	worker.Start(ctx)

	payload := queuePayload{
		ID:   "job-1",
		Kind: jobTypeStart,
		Start: StartRequest{
			LeadID: "lead-123",
		},
	}
	body, _ := json.Marshal(payload)
	queue.enqueue(queueMessage{
		ID:            "msg-1",
		Body:          string(body),
		ReceiptHandle: "rh-1",
	})

	waitFor(func() bool {
		return service.startCalls > 0
	}, time.Second, t)

	cancel()
	worker.Wait()

	if service.startCalls != 1 {
		t.Fatalf("expected 1 start call, got %d", service.startCalls)
	}
}

type recordingService struct {
	startCalls   int
	messageCalls int
	mu           sync.Mutex
}

func (r *recordingService) StartConversation(ctx context.Context, req StartRequest) (*Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.startCalls++
	return &Response{}, nil
}

func (r *recordingService) ProcessMessage(ctx context.Context, req MessageRequest) (*Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messageCalls++
	return &Response{}, nil
}

type scriptedQueue struct {
	ch chan queueMessage
}

func newScriptedQueue() *scriptedQueue {
	return &scriptedQueue{
		ch: make(chan queueMessage, 10),
	}
}

func (s *scriptedQueue) enqueue(msg queueMessage) {
	s.ch <- msg
}

func (s *scriptedQueue) Send(ctx context.Context, body string) error {
	return nil
}

func (s *scriptedQueue) Receive(ctx context.Context, maxMessages int, waitSeconds int) ([]queueMessage, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg := <-s.ch:
		return []queueMessage{msg}, nil
	case <-time.After(50 * time.Millisecond):
		return nil, nil
	}
}

func (s *scriptedQueue) Delete(ctx context.Context, receiptHandle string) error {
	return nil
}

func waitFor(cond func() bool, timeout time.Duration, t *testing.T) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}
