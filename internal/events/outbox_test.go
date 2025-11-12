package events

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	pgxmock "github.com/pashagolub/pgxmock/v4"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestOutboxStoreFlow(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create pgx mock: %v", err)
	}
	defer mock.Close()

	store := newOutboxStoreWithExec(mock)

	mock.ExpectExec("INSERT INTO outbox").WithArgs(pgxmock.AnyArg(), "org-1", "event.v1", pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	if _, err := store.Insert(context.Background(), "org-1", "event.v1", map[string]string{"foo": "bar"}); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	now := time.Now().UTC()
	id := uuid.New()
	rows := pgxmock.NewRows([]string{"id", "aggregate", "event_type", "payload", "created_at"}).AddRow(id, "org-1", "event.v1", []byte("{\"foo\":\"bar\"}"), now)
	mock.ExpectQuery("SELECT id").WithArgs(int32(10)).WillReturnRows(rows)

	entries, err := store.FetchPending(context.Background(), 10)
	if err != nil {
		t.Fatalf("fetch pending failed: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != id {
		t.Fatalf("unexpected entries: %#v", entries)
	}
	if entries[0].Aggregate != "org-1" || entries[0].EventType != "event.v1" {
		t.Fatalf("unexpected entry fields: %#v", entries[0])
	}

	mock.ExpectExec("UPDATE outbox").WithArgs(id).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	ok, err := store.MarkDelivered(context.Background(), id)
	if err != nil {
		t.Fatalf("mark delivered failed: %v", err)
	}
	if !ok {
		t.Fatal("expected mark delivered to report success")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDelivererDrain(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgx mock: %v", err)
	}
	defer mock.Close()

	store := newOutboxStoreWithExec(mock)
	handler := &stubDeliveryHandler{}
	deliverer := NewDeliverer(store, handler, logging.Default())

	id := uuid.New()
	now := time.Now().UTC()
	rows := pgxmock.NewRows([]string{"id", "aggregate", "event_type", "payload", "created_at"}).
		AddRow(id, "clinic:1", "event.v1", []byte("{}"), now)
	mock.ExpectQuery("SELECT id").WithArgs(int32(25)).WillReturnRows(rows)
	mock.ExpectExec("UPDATE outbox").WithArgs(id).WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	deliverer.drain(context.Background())
	if len(handler.entries) != 1 || handler.entries[0].ID != id {
		t.Fatalf("expected handler to receive entry")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDelivererStartStopsOnContextCancel(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgx mock: %v", err)
	}
	defer mock.Close()

	store := newOutboxStoreWithExec(mock)
	ctx, cancel := context.WithCancel(context.Background())
	handler := &stubDeliveryHandler{afterHandle: cancel}
	deliverer := NewDeliverer(store, handler, logging.Default()).WithInterval(5 * time.Millisecond)

	id := uuid.New()
	now := time.Now().UTC()
	rows := pgxmock.NewRows([]string{"id", "aggregate", "event_type", "payload", "created_at"}).
		AddRow(id, "clinic:1", "event.v1", []byte("{}"), now)
	mock.ExpectQuery("SELECT id").WithArgs(int32(25)).WillReturnRows(rows)
	mock.ExpectExec("UPDATE outbox").WithArgs(id).WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	done := make(chan struct{})
	go func() {
		deliverer.Start(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("deliverer did not stop after cancellation")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDelivererOptionHelpers(t *testing.T) {
	deliverer := NewDeliverer(nil, nil, nil)
	deliverer.WithBatchSize(100)
	if deliverer.batchSize != 100 {
		t.Fatalf("expected batch size override")
	}
	interval := 123 * time.Millisecond
	deliverer.WithInterval(interval)
	if deliverer.interval != interval {
		t.Fatalf("expected interval override")
	}
}

func TestNewOutboxStorePanicsOnNilPool(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for nil pool")
		}
	}()
	NewOutboxStore(nil)
}

func TestNewOutboxStoreWithExecPanicsOnNil(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for nil exec")
		}
	}()
	newOutboxStoreWithExec(nil)
}

func TestNewOutboxStoreReturnsInstance(t *testing.T) {
	store := NewOutboxStore(&pgxpool.Pool{})
	if store == nil {
		t.Fatalf("expected store instance")
	}
}

func TestOutboxStoreInsertMarshalError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgx mock: %v", err)
	}
	defer mock.Close()
	store := newOutboxStoreWithExec(mock)
	if _, err := store.Insert(context.Background(), "agg", "event", make(chan int)); err == nil {
		t.Fatalf("expected marshal error")
	}
}

func TestFetchPendingQueryError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgx mock: %v", err)
	}
	defer mock.Close()
	store := newOutboxStoreWithExec(mock)
	mock.ExpectQuery("SELECT id").WithArgs(int32(5)).WillReturnError(fmt.Errorf("boom"))
	if _, err := store.FetchPending(context.Background(), 5); err == nil {
		t.Fatalf("expected query error")
	}
}

func TestMarkDeliveredError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgx mock: %v", err)
	}
	defer mock.Close()
	store := newOutboxStoreWithExec(mock)
	mock.ExpectExec("UPDATE outbox").WithArgs(pgxmock.AnyArg()).WillReturnError(fmt.Errorf("boom"))
	if _, err := store.MarkDelivered(context.Background(), uuid.New()); err == nil {
		t.Fatalf("expected update error")
	}
}

func TestDelivererDrainHandlesHandlerErrors(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgx mock: %v", err)
	}
	defer mock.Close()
	store := newOutboxStoreWithExec(mock)
	id := uuid.New()
	rows := pgxmock.NewRows([]string{"id", "aggregate", "event_type", "payload", "created_at"}).
		AddRow(id, "agg", "evt", []byte("{}"), time.Now().UTC())
	mock.ExpectQuery("SELECT id").WithArgs(int32(25)).WillReturnRows(rows)
	badHandler := deliveryHandlerFunc(func(ctx context.Context, entry OutboxEntry) error {
		return errors.New("handler failed")
	})
	deliverer := NewDeliverer(store, badHandler, logging.Default())
	deliverer.drain(context.Background())
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDelivererDrainHandlesMarkDeliveredError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgx mock: %v", err)
	}
	defer mock.Close()
	store := newOutboxStoreWithExec(mock)
	id := uuid.New()
	rows := pgxmock.NewRows([]string{"id", "aggregate", "event_type", "payload", "created_at"}).
		AddRow(id, "agg", "evt", []byte("{}"), time.Now().UTC())
	mock.ExpectQuery("SELECT id").WithArgs(int32(25)).WillReturnRows(rows)
	mock.ExpectExec("UPDATE outbox").WithArgs(id).WillReturnError(errors.New("db down"))
	deliverer := NewDeliverer(store, deliveryHandlerFunc(func(ctx context.Context, entry OutboxEntry) error {
		return nil
	}), logging.Default())
	deliverer.drain(context.Background())
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDelivererStartNoopWithoutDeps(t *testing.T) {
	deliverer := NewDeliverer(nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deliverer.Start(ctx) // should return immediately without panic
}

type deliveryHandlerFunc func(ctx context.Context, entry OutboxEntry) error

func (f deliveryHandlerFunc) Handle(ctx context.Context, entry OutboxEntry) error {
	return f(ctx, entry)
}

type stubDeliveryHandler struct {
	entries     []OutboxEntry
	afterHandle func()
}

func (s *stubDeliveryHandler) Handle(ctx context.Context, entry OutboxEntry) error {
	s.entries = append(s.entries, entry)
	if s.afterHandle != nil {
		s.afterHandle()
	}
	return nil
}
