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

type fakeHostedStore struct {
	orders  []messaging.HostedOrderRecord
	updates int
	err     error
}

func (f *fakeHostedStore) PendingHostedOrders(ctx context.Context, limit int) ([]messaging.HostedOrderRecord, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.orders, nil
}

func (f *fakeHostedStore) UpsertHostedOrder(ctx context.Context, q messaging.Querier, record messaging.HostedOrderRecord) error {
	f.updates++
	return nil
}

type fakeHostedStoreUpsertErr struct {
	orders []messaging.HostedOrderRecord
}

func (f *fakeHostedStoreUpsertErr) PendingHostedOrders(ctx context.Context, limit int) ([]messaging.HostedOrderRecord, error) {
	return f.orders, nil
}

func (f *fakeHostedStoreUpsertErr) UpsertHostedOrder(ctx context.Context, q messaging.Querier, record messaging.HostedOrderRecord) error {
	return errors.New("update failed")
}

type fakeHostedClient struct{}

func (fakeHostedClient) GetHostedOrder(ctx context.Context, orderID string) (*telnyxclient.HostedOrder, error) {
	return &telnyxclient.HostedOrder{ID: orderID, Status: "activated", PhoneNumber: "+1555"}, nil
}

type fakeHostedClientErr struct{}

func (fakeHostedClientErr) GetHostedOrder(ctx context.Context, orderID string) (*telnyxclient.HostedOrder, error) {
	return nil, errors.New("boom")
}

func TestHostedPollerUpdatesOrders(t *testing.T) {
	store := &fakeHostedStore{orders: []messaging.HostedOrderRecord{{ID: uuid.New(), ClinicID: uuid.New(), ProviderOrderID: "hno_1"}}}
	poller := NewHostedPoller(store, fakeHostedClient{}, nil).WithInterval(time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		poller.drain(ctx)
		cancel()
	}()
	<-ctx.Done()

	if store.updates == 0 {
		t.Fatalf("expected updates")
	}
}

func TestHostedPollerHandlesErrors(t *testing.T) {
	store := &fakeHostedStore{err: errors.New("boom")}
	poller := NewHostedPoller(store, fakeHostedClient{}, nil)
	poller.drain(context.Background())
}

func TestHostedPollerClientErrors(t *testing.T) {
	store := &fakeHostedStore{orders: []messaging.HostedOrderRecord{{ID: uuid.New(), ClinicID: uuid.New(), ProviderOrderID: "hno_2"}}}
	poller := NewHostedPoller(store, fakeHostedClientErr{}, nil)
	poller.drain(context.Background())
}

func TestHostedPollerUpsertErrorLogged(t *testing.T) {
	store := &fakeHostedStoreUpsertErr{orders: []messaging.HostedOrderRecord{{ID: uuid.New(), ClinicID: uuid.New(), ProviderOrderID: "hno_3"}}}
	poller := NewHostedPoller(store, fakeHostedClient{}, nil)
	poller.drain(context.Background())
}

func TestHostedPollerRunStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := &fakeHostedStore{}
	poller := NewHostedPoller(store, fakeHostedClient{}, nil).WithInterval(5 * time.Millisecond).WithBatchSize(5)
	go poller.Run(ctx)
	time.Sleep(10 * time.Millisecond)
	cancel()
}
