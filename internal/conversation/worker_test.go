package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestWorkerProcessesMessages(t *testing.T) {
	queue := newScriptedQueue()
	service := &recordingService{}
	store := &stubJobUpdater{}
	worker := NewWorker(service, queue, store, nil, nil, logging.Default(), WithWorkerCount(1), WithReceiveBatchSize(1), WithReceiveWaitSeconds(0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	worker.Start(ctx)

	payload := queuePayload{
		ID:          "job-1",
		Kind:        jobTypeStart,
		TrackStatus: true,
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

	if len(store.completed) != 1 || store.completed[0] != "job-1" {
		t.Fatalf("expected job completion to be recorded, got %#v", store.completed)
	}

	if queue.deleted != 1 {
		t.Fatalf("expected delete to be invoked once, got %d", queue.deleted)
	}
}

func TestWorkerSendsReplies(t *testing.T) {
	queue := newScriptedQueue()
	service := &replyService{}
	store := &stubJobUpdater{}
	messenger := &stubMessenger{}
	worker := NewWorker(service, queue, store, messenger, nil, logging.Default(), WithWorkerCount(1), WithReceiveBatchSize(1), WithReceiveWaitSeconds(0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	payload := queuePayload{
		ID:          "job-msg",
		Kind:        jobTypeMessage,
		TrackStatus: true,
		Message: MessageRequest{
			ConversationID: "conv-1",
			OrgID:          "org-1",
			LeadID:         "lead-1",
			Message:        "hi",
			Channel:        ChannelSMS,
			From:           "+12223334444",
			To:             "+15556667777",
		},
	}
	body, _ := json.Marshal(payload)
	queue.enqueue(queueMessage{
		ID:            "msg-2",
		Body:          string(body),
		ReceiptHandle: "rh-2",
	})

	waitFor(func() bool {
		return messenger.called
	}, time.Second, t)

	cancel()
	worker.Wait()

	if messenger.last.Body != "auto-reply" {
		t.Fatalf("expected auto-reply body, got %s", messenger.last.Body)
	}
	if messenger.last.To != "+12223334444" || messenger.last.From != "+15556667777" {
		t.Fatalf("unexpected to/from: %#v", messenger.last)
	}
}

func TestWorkerProcessesPaymentEvent(t *testing.T) {
	queue := newScriptedQueue()
	service := &recordingService{}
	store := &stubJobUpdater{}
	messenger := &stubMessenger{}
	bookings := &stubBookingConfirmer{}
	worker := NewWorker(service, queue, store, messenger, bookings, logging.Default(), WithWorkerCount(1), WithReceiveBatchSize(1), WithReceiveWaitSeconds(0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	orgID := uuid.New()
	leadID := uuid.New()
	event := events.PaymentSucceededV1{
		EventID:     "evt-123",
		OrgID:       orgID.String(),
		LeadID:      leadID.String(),
		LeadPhone:   "+19998887777",
		FromNumber:  "+15550000000",
		AmountCents: 5000,
		OccurredAt:  time.Now().UTC(),
	}
	payload := queuePayload{
		ID:          "job-payment",
		Kind:        jobTypePayment,
		TrackStatus: false,
		Payment:     &event,
	}
	body, _ := json.Marshal(payload)
	queue.enqueue(queueMessage{ID: "msg-pay", Body: string(body), ReceiptHandle: "rh-pay"})

	waitFor(func() bool {
		return len(bookings.calls) == 1 && messenger.called
	}, time.Second, t)

	cancel()
	worker.Wait()

	if len(bookings.calls) != 1 {
		t.Fatalf("expected booking confirm call, got %d", len(bookings.calls))
	}
	if messenger.last.To != event.LeadPhone {
		t.Fatalf("expected sms to lead, got %s", messenger.last.To)
	}
}

func TestWorkerHandlesProcessingErrors(t *testing.T) {
	queue := newScriptedQueue()
	service := &recordingService{failStart: true}
	store := &stubJobUpdater{}
	worker := NewWorker(service, queue, store, nil, nil, logging.Default(), WithWorkerCount(1), WithReceiveBatchSize(1), WithReceiveWaitSeconds(0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	payload := queuePayload{
		ID:          "job-fail",
		Kind:        jobTypeStart,
		TrackStatus: true,
		Start: StartRequest{
			LeadID: "lead-err",
		},
	}
	body, _ := json.Marshal(payload)
	queue.enqueue(queueMessage{
		ID:            "msg-fail",
		Body:          string(body),
		ReceiptHandle: "rh-fail",
	})

	waitFor(func() bool {
		return len(store.failed) == 1
	}, time.Second, t)

	cancel()
	worker.Wait()

	if store.failed[0].jobID != "job-fail" || store.failed[0].err == "" {
		t.Fatalf("expected failure to be recorded, got %#v", store.failed[0])
	}
}

func TestWorkerSkipsMalformedPayload(t *testing.T) {
	queue := newScriptedQueue()
	service := &recordingService{}
	store := &stubJobUpdater{}
	worker := NewWorker(service, queue, store, nil, nil, logging.Default(), WithWorkerCount(1), WithReceiveBatchSize(1), WithReceiveWaitSeconds(0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	queue.enqueue(queueMessage{ID: "bad", Body: "{", ReceiptHandle: "rh-bad"})

	time.Sleep(50 * time.Millisecond)

	cancel()
	worker.Wait()

	if service.startCalls != 0 && service.messageCalls != 0 {
		t.Fatalf("expected no processor calls for malformed body")
	}
	if len(store.completed) != 0 || len(store.failed) != 0 {
		t.Fatalf("expected no job updates for malformed payload")
	}
}

func TestWorkerConfigOptions(t *testing.T) {
	queue := newScriptedQueue()
	service := &recordingService{}
	store := &stubJobUpdater{}

	worker := NewWorker(
		service,
		queue,
		store,
		nil,
		nil,
		logging.Default(),
		WithWorkerCount(3),
		WithReceiveBatchSize(20),
		WithReceiveWaitSeconds(30),
	)

	if worker.cfg.workers != 3 {
		t.Fatalf("expected worker count override, got %d", worker.cfg.workers)
	}
	if worker.cfg.receiveBatchSize != maxReceiveBatchSize {
		t.Fatalf("expected batch size capped at %d, got %d", maxReceiveBatchSize, worker.cfg.receiveBatchSize)
	}
	if worker.cfg.receiveWaitSecs != maxWaitSeconds {
		t.Fatalf("expected wait seconds capped at %d, got %d", maxWaitSeconds, worker.cfg.receiveWaitSecs)
	}
}

type recordingService struct {
	startCalls   int
	messageCalls int
	failStart    bool
	mu           sync.Mutex
}

func (r *recordingService) StartConversation(ctx context.Context, req StartRequest) (*Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.startCalls++
	if r.failStart {
		return nil, errors.New("processor boom")
	}
	return &Response{}, nil
}

func (r *recordingService) ProcessMessage(ctx context.Context, req MessageRequest) (*Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messageCalls++
	return &Response{}, nil
}

type replyService struct{}

func (r *replyService) StartConversation(ctx context.Context, req StartRequest) (*Response, error) {
	return &Response{}, nil
}

func (r *replyService) ProcessMessage(ctx context.Context, req MessageRequest) (*Response, error) {
	return &Response{
		ConversationID: req.ConversationID,
		Message:        "auto-reply",
	}, nil
}

type scriptedQueue struct {
	ch       chan queueMessage
	deleted  int
	delMutex sync.Mutex
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
	s.delMutex.Lock()
	s.deleted++
	s.delMutex.Unlock()
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

type stubJobUpdater struct {
	completed []string
	failed    []struct {
		jobID string
		err   string
	}
}

func (s *stubJobUpdater) MarkCompleted(ctx context.Context, jobID string, resp *Response, conversationID string) error {
	s.completed = append(s.completed, jobID)
	return nil
}

func (s *stubJobUpdater) MarkFailed(ctx context.Context, jobID string, errMsg string) error {
	s.failed = append(s.failed, struct {
		jobID string
		err   string
	}{jobID: jobID, err: errMsg})
	return nil
}

type stubMessenger struct {
	called bool
	last   OutboundReply
}

func (s *stubMessenger) SendReply(ctx context.Context, reply OutboundReply) error {
	s.called = true
	s.last = reply
	return nil
}

type stubBookingConfirmer struct {
	calls []struct {
		org  uuid.UUID
		lead uuid.UUID
	}
}

func (s *stubBookingConfirmer) ConfirmBooking(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID, scheduledFor *time.Time) error {
	s.calls = append(s.calls, struct {
		org  uuid.UUID
		lead uuid.UUID
	}{org: orgID, lead: leadID})
	return nil
}
