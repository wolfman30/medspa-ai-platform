package messagingworker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/telnyxclient"
)

type fakeRetryStore struct {
	messages      []messaging.MessageRecord
	scheduled     map[uuid.UUID]time.Time
	statusUpdates []string
	scheduleErr   error
	listErr       error
}

func (f *fakeRetryStore) ListRetryCandidates(ctx context.Context, limit int, max int) ([]messaging.MessageRecord, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if len(f.messages) > limit {
		return f.messages[:limit], nil
	}
	return f.messages, nil
}

func (f *fakeRetryStore) ScheduleRetry(ctx context.Context, q messaging.Querier, id uuid.UUID, status string, next time.Time) error {
	if f.scheduleErr != nil {
		return f.scheduleErr
	}
	if f.scheduled == nil {
		f.scheduled = make(map[uuid.UUID]time.Time)
	}
	f.scheduled[id] = next
	return nil
}

func (f *fakeRetryStore) UpdateMessageStatus(ctx context.Context, providerID, status string, deliveredAt, failedAt *time.Time) error {
	f.statusUpdates = append(f.statusUpdates, providerID+":"+status)
	return nil
}

type fakeTelnyxSender struct {
	resp *telnyxclient.MessageResponse
	err  error
}

func (f *fakeTelnyxSender) SendMessage(ctx context.Context, req telnyxclient.SendMessageRequest) (*telnyxclient.MessageResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.resp != nil {
		return f.resp, nil
	}
	return &telnyxclient.MessageResponse{ID: "msg_test", Status: "queued"}, nil
}

func TestRetrySenderSchedulesRetryOnFailure(t *testing.T) {
	store := &fakeRetryStore{messages: []messaging.MessageRecord{{ID: uuid.New(), From: "+1", To: "+2", Body: "hi", SendAttempts: 1, ProviderStatus: "failed"}}}
	telnyx := &fakeTelnyxSender{err: errors.New("boom")}
	sender := NewRetrySender(store, telnyx, nil).WithBaseDelay(time.Minute).WithInterval(time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		sender.drain(ctx)
		cancel()
	}()
	<-ctx.Done()

	if len(store.scheduled) != 1 {
		t.Fatalf("expected schedule call, got %d", len(store.scheduled))
	}
}

func TestRetrySenderUpdatesStatusOnSuccess(t *testing.T) {
	msgID := uuid.New()
	store := &fakeRetryStore{messages: []messaging.MessageRecord{{ID: msgID, From: "+1", To: "+2", Body: "hi"}}}
	telnyx := &fakeTelnyxSender{resp: &telnyxclient.MessageResponse{ID: "msg_provider", Status: "queued"}}
	sender := NewRetrySender(store, telnyx, nil)

	sender.drain(context.Background())

	if len(store.statusUpdates) != 1 {
		t.Fatalf("expected status update")
	}
}

func TestNextDelayCaps(t *testing.T) {
	sender := NewRetrySender(&fakeRetryStore{}, &fakeTelnyxSender{}, nil)
	if d := sender.nextDelay(10); d > 24*time.Hour {
		t.Fatalf("expected cap, got %s", d)
	}
}

func TestRetrySenderRunNilDeps(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sender := NewRetrySender(nil, nil, nil).WithInterval(time.Millisecond)
	go sender.Run(ctx)
	cancel()
}

func TestRetrySenderRunLoop(t *testing.T) {
	cancelCtx, cancel := context.WithCancel(context.Background())
	store := &fakeRetryStore{messages: []messaging.MessageRecord{{ID: uuid.New(), From: "+1", To: "+2", Body: "hi"}}}
	telnyx := &fakeTelnyxSender{resp: &telnyxclient.MessageResponse{ID: "msg", Status: "queued"}}
	sender := NewRetrySender(store, telnyx, nil).WithInterval(5 * time.Millisecond).WithBatchSize(5)
	go func() {
		sender.Run(cancelCtx)
	}()
	time.Sleep(15 * time.Millisecond)
	cancel()
}

func TestRetrySenderHandlesStoreErrors(t *testing.T) {
	store := &fakeRetryStore{listErr: errors.New("boom")}
	sender := NewRetrySender(store, &fakeTelnyxSender{}, nil)
	sender.drain(context.Background())
}

func TestRetrySenderHandlesScheduleError(t *testing.T) {
	store := &fakeRetryStore{messages: []messaging.MessageRecord{{ID: uuid.New(), From: "+1", To: "+2", Body: "hi"}}, scheduleErr: errors.New("nope")}
	sender := NewRetrySender(store, &fakeTelnyxSender{err: errors.New("boom")}, nil)
	sender.drain(context.Background())
}

func TestRetrySenderDrainWithoutClient(t *testing.T) {
	sender := NewRetrySender(&fakeRetryStore{}, nil, nil)
	sender.drain(context.Background())
}
