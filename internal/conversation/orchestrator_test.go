package conversation

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestOrchestrator_StartConversation(t *testing.T) {
	service := &fakeProcessor{
		startResp: &Response{
			ConversationID: "conv-1",
			Message:        "hello",
		},
	}
	queue := newStubQueue()

	o := NewOrchestrator(
		service,
		queue,
		logging.Default(),
		WithWorkerCount(1),
		WithReceiveBatchSize(1),
		WithReceiveWaitSeconds(0),
	)
	t.Cleanup(func() {
		_ = o.Shutdown(context.Background())
	})

	req := StartRequest{LeadID: "lead-123"}
	resp, err := o.StartConversation(context.Background(), req)
	if err != nil {
		t.Fatalf("StartConversation returned error: %v", err)
	}

	if resp.ConversationID != "conv-1" {
		t.Fatalf("expected conversation ID conv-1, got %s", resp.ConversationID)
	}

	if service.lastStartReq.LeadID != req.LeadID {
		t.Fatalf("expected LeadID %s, got %s", req.LeadID, service.lastStartReq.LeadID)
	}
}

func TestOrchestrator_ContextCancellation(t *testing.T) {
	block := make(chan struct{})
	service := &blockingProcessor{block: block}
	queue := newStubQueue()

	o := NewOrchestrator(
		service,
		queue,
		logging.Default(),
		WithWorkerCount(1),
		WithReceiveBatchSize(1),
		WithReceiveWaitSeconds(0),
	)
	t.Cleanup(func() {
		_ = o.Shutdown(context.Background())
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if _, err := o.StartConversation(ctx, StartRequest{LeadID: "first"}); err != context.DeadlineExceeded {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}

	close(block)
}

type fakeProcessor struct {
	startResp    *Response
	messageResp  *Response
	lastStartReq StartRequest
	lastMsgReq   MessageRequest
}

func (f *fakeProcessor) StartConversation(ctx context.Context, req StartRequest) (*Response, error) {
	f.lastStartReq = req
	if f.startResp != nil {
		return f.startResp, nil
	}
	return &Response{ConversationID: "default", Message: "ok"}, nil
}

func (f *fakeProcessor) ProcessMessage(ctx context.Context, req MessageRequest) (*Response, error) {
	f.lastMsgReq = req
	if f.messageResp != nil {
		return f.messageResp, nil
	}
	return &Response{ConversationID: req.ConversationID, Message: "ok"}, nil
}

type blockingProcessor struct {
	block chan struct{}
}

func (b *blockingProcessor) StartConversation(ctx context.Context, req StartRequest) (*Response, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.block:
		return &Response{ConversationID: "unblocked", Message: "done"}, nil
	}
}

func (b *blockingProcessor) ProcessMessage(ctx context.Context, req MessageRequest) (*Response, error) {
	return &Response{ConversationID: req.ConversationID, Message: "done"}, nil
}

type stubQueue struct {
	ch chan queueMessage
}

func newStubQueue() *stubQueue {
	return &stubQueue{ch: make(chan queueMessage, 32)}
}

func (s *stubQueue) Send(ctx context.Context, body string) error {
	msg := queueMessage{
		ID:            uuid.NewString(),
		Body:          body,
		ReceiptHandle: uuid.NewString(),
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.ch <- msg:
		return nil
	}
}

func (s *stubQueue) Receive(ctx context.Context, maxMessages int, waitSeconds int) ([]queueMessage, error) {
	timeout := time.Duration(waitSeconds) * time.Millisecond
	if waitSeconds <= 0 {
		timeout = 5 * time.Millisecond
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg := <-s.ch:
		return []queueMessage{msg}, nil
	case <-timer.C:
		return nil, nil
	}
}

func (s *stubQueue) Delete(ctx context.Context, receiptHandle string) error {
	return nil
}
