package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
	deposits := &stubDepositSender{}
	worker := NewWorker(service, queue, store, messenger, bookings, logging.Default(), WithWorkerCount(1), WithReceiveBatchSize(1), WithReceiveWaitSeconds(0), WithDepositSender(deposits))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	orgID := uuid.New()
	leadID := uuid.New()
	scheduled := time.Now().Add(24 * time.Hour).UTC()
	event := events.PaymentSucceededV1{
		EventID:      "evt-123",
		OrgID:        orgID.String(),
		LeadID:       leadID.String(),
		LeadPhone:    "+19998887777",
		FromNumber:   "+15550000000",
		AmountCents:  5000,
		OccurredAt:   time.Now().UTC(),
		ScheduledFor: &scheduled,
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
	if sched := bookings.firstScheduled(); sched == nil || sched.Unix() != scheduled.Unix() {
		t.Fatalf("expected scheduled time passed to booking confirmer")
	}
	if messenger.lastReply().To != event.LeadPhone {
		t.Fatalf("expected sms to lead, got %s", messenger.lastReply().To)
	}
	call := bookings.lastCall()
	if call == nil || call.scheduled == nil || !call.scheduled.Equal(scheduled) {
		t.Fatalf("expected scheduled time to propagate, got %#v", call)
	}
	// Confirmation SMS uses human-friendly format "Monday, January 2 at 3:04 PM"
	if !strings.Contains(messenger.lastReply().Body, scheduled.Format("Monday, January 2 at 3:04 PM")) {
		t.Fatalf("expected scheduled time in confirmation sms, got %q", messenger.lastReply().Body)
	}
}

func TestWorkerProcessesPaymentEvent_IsIdempotent(t *testing.T) {
	queue := newScriptedQueue()
	service := &recordingService{}
	store := &stubJobUpdater{}
	messenger := &stubMessenger{}
	bookings := &stubBookingConfirmer{}
	processed := &stubProcessedStore{seen: map[string]bool{}}
	worker := NewWorker(service, queue, store, messenger, bookings, logging.Default(), WithProcessedEventsStore(processed))

	orgID := uuid.New()
	leadID := uuid.New()
	scheduled := time.Now().Add(24 * time.Hour).UTC()
	event := events.PaymentSucceededV1{
		EventID:      "evt-1",
		OrgID:        orgID.String(),
		LeadID:       leadID.String(),
		ProviderRef:  "pay-123",
		LeadPhone:    "+19998887777",
		FromNumber:   "+15550000000",
		AmountCents:  5000,
		OccurredAt:   time.Now().UTC(),
		ScheduledFor: &scheduled,
	}

	if err := worker.handlePaymentEvent(context.Background(), &event); err != nil {
		t.Fatalf("first handlePaymentEvent failed: %v", err)
	}
	event.EventID = "evt-2"
	if err := worker.handlePaymentEvent(context.Background(), &event); err != nil {
		t.Fatalf("second handlePaymentEvent failed: %v", err)
	}

	if got := messenger.callCount(); got != 1 {
		t.Fatalf("expected one confirmation sms, got %d", got)
	}
	if got := bookings.callCount(); got != 1 {
		t.Fatalf("expected booking confirmation once, got %d", got)
	}
}

func TestWorkerDispatchesDepositIntent(t *testing.T) {
	queue := newScriptedQueue()
	service := &replyService{
		deposit: &DepositIntent{
			AmountCents: 5000,
			SuccessURL:  "http://success",
			CancelURL:   "http://cancel",
			Description: "Test deposit",
		},
	}
	store := &stubJobUpdater{}
	messenger := &stubMessenger{}
	deposits := &stubDepositSender{}

	worker := NewWorker(service, queue, store, messenger, nil, logging.Default(), WithWorkerCount(1), WithReceiveBatchSize(1), WithReceiveWaitSeconds(0), WithDepositSender(deposits))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	payload := queuePayload{
		ID:          "job-deposit",
		Kind:        jobTypeMessage,
		TrackStatus: true,
		Message: MessageRequest{
			ConversationID: "conv-1",
			OrgID:          "org-1",
			LeadID:         "lead-1",
			Message:        "please book",
			Channel:        ChannelSMS,
			From:           "+12223334444",
			To:             "+15556667777",
		},
	}
	body, _ := json.Marshal(payload)
	queue.enqueue(queueMessage{ID: "msg-deposit", Body: string(body), ReceiptHandle: "rh-deposit"})

	waitFor(func() bool {
		return deposits.called
	}, time.Second, t)

	cancel()
	worker.Wait()

	if !deposits.called {
		t.Fatalf("expected deposit sender to be called")
	}
	if deposits.lastMsg.OrgID != "org-1" || deposits.lastMsg.LeadID != "lead-1" {
		t.Fatalf("unexpected deposit msg: %#v", deposits.lastMsg)
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

func (r *recordingService) GetHistory(ctx context.Context, conversationID string) ([]Message, error) {
	return []Message{}, nil
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

type replyService struct {
	deposit *DepositIntent
}

func (r *replyService) StartConversation(ctx context.Context, req StartRequest) (*Response, error) {
	return &Response{}, nil
}

func (r *replyService) ProcessMessage(ctx context.Context, req MessageRequest) (*Response, error) {
	return &Response{
		ConversationID: req.ConversationID,
		Message:        "auto-reply",
		DepositIntent:  r.deposit,
	}, nil
}

func (r *replyService) GetHistory(ctx context.Context, conversationID string) ([]Message, error) {
	return []Message{}, nil
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
	count  int
	last   OutboundReply
	mu     sync.Mutex
}

func (s *stubMessenger) SendReply(ctx context.Context, reply OutboundReply) error {
	s.mu.Lock()
	s.called = true
	s.count++
	s.last = reply
	s.mu.Unlock()
	return nil
}

func (s *stubMessenger) wasCalled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.called
}

func (s *stubMessenger) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

func (s *stubMessenger) lastReply() OutboundReply {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

type stubProcessedStore struct {
	seen map[string]bool
	mu   sync.Mutex
}

func (s *stubProcessedStore) AlreadyProcessed(ctx context.Context, provider, eventID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen[provider+":"+eventID], nil
}

func (s *stubProcessedStore) MarkProcessed(ctx context.Context, provider, eventID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := provider + ":" + eventID
	if s.seen[key] {
		return false, nil
	}
	s.seen[key] = true
	return true, nil
}

type stubDepositSender struct {
	called  bool
	lastMsg MessageRequest
	lastRes *Response
}

func (s *stubDepositSender) SendDeposit(ctx context.Context, msg MessageRequest, resp *Response) error {
	s.called = true
	s.lastMsg = msg
	s.lastRes = resp
	return nil
}

type stubBookingConfirmer struct {
	calls []struct {
		org       uuid.UUID
		lead      uuid.UUID
		scheduled *time.Time
	}
	mu sync.Mutex
}

func (s *stubBookingConfirmer) ConfirmBooking(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID, scheduledFor *time.Time) error {
	s.mu.Lock()
	s.calls = append(s.calls, struct {
		org       uuid.UUID
		lead      uuid.UUID
		scheduled *time.Time
	}{org: orgID, lead: leadID, scheduled: scheduledFor})
	s.mu.Unlock()
	return nil
}

func (s *stubBookingConfirmer) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *stubBookingConfirmer) lastCall() *struct {
	org       uuid.UUID
	lead      uuid.UUID
	scheduled *time.Time
} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.calls) == 0 {
		return nil
	}
	return &s.calls[len(s.calls)-1]
}

func (s *stubBookingConfirmer) firstScheduled() *time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.calls) == 0 {
		return nil
	}
	return s.calls[0].scheduled
}
