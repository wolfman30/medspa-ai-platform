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
		return service.startCount() > 0
	}, time.Second, t)

	cancel()
	worker.Wait()

	if service.startCount() != 1 {
		t.Fatalf("expected 1 start call, got %d", service.startCount())
	}

	if jobs := store.completedJobs(); len(jobs) != 1 || jobs[0] != "job-1" {
		t.Fatalf("expected job completion to be recorded, got %#v", jobs)
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
		return messenger.wasCalled()
	}, time.Second, t)

	cancel()
	worker.Wait()

	last := messenger.lastReply()
	if last.Body != "auto-reply" {
		t.Fatalf("expected auto-reply body, got %s", last.Body)
	}
	if last.To != "+12223334444" || last.From != "+15556667777" {
		t.Fatalf("unexpected to/from: %#v", last)
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
		return bookings.callCount() == 1 && messenger.wasCalled()
	}, time.Second, t)

	cancel()
	worker.Wait()

	if bookings.callCount() != 1 {
		t.Fatalf("expected booking confirm call, got %d", bookings.callCount())
	}
	if messenger.lastReply().To != event.LeadPhone {
		t.Fatalf("expected sms to lead, got %s", messenger.lastReply().To)
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
		return store.failureCount() == 1
	}, time.Second, t)

	cancel()
	worker.Wait()

	if store.failureCount() != 1 {
		t.Fatalf("expected failure to be recorded")
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

	if service.startCount() != 0 && service.messageCount() != 0 {
		t.Fatalf("expected no processor calls for malformed body")
	}
	if len(store.completedJobs()) != 0 || store.failureCount() != 0 {
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

func (r *recordingService) startCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.startCalls
}

func (r *recordingService) messageCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.messageCalls
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
	mu sync.Mutex
}

func (s *stubJobUpdater) MarkCompleted(ctx context.Context, jobID string, resp *Response, conversationID string) error {
	s.mu.Lock()
	s.completed = append(s.completed, jobID)
	s.mu.Unlock()
	return nil
}

func (s *stubJobUpdater) MarkFailed(ctx context.Context, jobID string, errMsg string) error {
	s.mu.Lock()
	s.failed = append(s.failed, struct {
		jobID string
		err   string
	}{jobID: jobID, err: errMsg})
	s.mu.Unlock()
	return nil
}

func (s *stubJobUpdater) completedJobs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.completed...)
}

func (s *stubJobUpdater) failureCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.failed)
}

type stubMessenger struct {
	called bool
	last   OutboundReply
	mu     sync.Mutex
}

func (s *stubMessenger) SendReply(ctx context.Context, reply OutboundReply) error {
	s.mu.Lock()
	s.called = true
	s.last = reply
	s.mu.Unlock()
	return nil
}

func (s *stubMessenger) wasCalled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.called
}

func (s *stubMessenger) lastReply() OutboundReply {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

type stubBookingConfirmer struct {
	calls []struct {
		org  uuid.UUID
		lead uuid.UUID
	}
	mu sync.Mutex
}

func (s *stubBookingConfirmer) ConfirmBooking(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID, scheduledFor *time.Time) error {
	s.mu.Lock()
	s.calls = append(s.calls, struct {
		org  uuid.UUID
		lead uuid.UUID
	}{org: orgID, lead: leadID})
	s.mu.Unlock()
	return nil
}

func (s *stubBookingConfirmer) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}
