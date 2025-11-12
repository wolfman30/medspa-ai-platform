package messaging

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	pgxmock "github.com/pashagolub/pgxmock/v4"
)

func TestStoreInsertMessage(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()

	store := &Store{pool: mock}
	clinicID := uuid.New()
	mock.ExpectQuery("INSERT INTO messages").
		WithArgs(clinicID, "+1555", "+1666", "outbound", "hello", pgxmock.AnyArg(), "queued", "msg_1", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(uuid.New()))

	if _, err := store.InsertMessage(context.Background(), mock, MessageRecord{
		ClinicID:          clinicID,
		From:              "+1555",
		To:                "+1666",
		Direction:         "outbound",
		Body:              "hello",
		ProviderStatus:    "queued",
		ProviderMessageID: "msg_1",
	}); err != nil {
		t.Fatalf("insert message: %v", err)
	}
}

func TestStoreUpdateMessageStatus(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := &Store{pool: mock}
	now := time.Now()
	mock.ExpectExec("UPDATE messages").
		WithArgs("msg_1", "delivered", &now, (*time.Time)(nil)).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	if err := store.UpdateMessageStatus(context.Background(), "msg_1", "delivered", &now, nil); err != nil {
		t.Fatalf("update status: %v", err)
	}
}

func TestStoreUnsubscribe(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := &Store{pool: mock}
	clinicID := uuid.New()

	mock.ExpectExec("INSERT INTO unsubscribes").
		WithArgs(clinicID, "+1222", "STOP").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	if err := store.InsertUnsubscribe(context.Background(), nil, clinicID, "+1222", "STOP"); err != nil {
		t.Fatalf("insert unsubscribe: %v", err)
	}

	mock.ExpectQuery("SELECT 1 FROM unsubscribes").
		WithArgs(clinicID, "+1222").
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(1))
	if ok, err := store.IsUnsubscribed(context.Background(), clinicID, "+1222"); err != nil || !ok {
		t.Fatalf("expected unsubscribe true, got %v err=%v", ok, err)
	}
}

func TestScheduleRetryAndListCandidates(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := &Store{pool: mock}
	msgID := uuid.New()
	mock.ExpectExec("UPDATE messages").
		WithArgs(msgID, "retry_pending", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	if err := store.ScheduleRetry(context.Background(), nil, msgID, "retry_pending", time.Now()); err != nil {
		t.Fatalf("schedule retry: %v", err)
	}
	mock.ExpectQuery("SELECT id, clinic_id").
		WithArgs(5, 10).
		WillReturnRows(pgxmock.NewRows([]string{"id", "clinic_id", "from_e164", "to_e164", "body", "mms_media", "provider_status", "provider_message_id", "send_attempts", "next_retry_at"}).
			AddRow(msgID, uuid.New(), "+1555", "+1666", "hi", []byte(`[]`), "retry_pending", "msg_provider", 1, time.Now()))
	if _, err := store.ListRetryCandidates(context.Background(), 10, 5); err != nil {
		t.Fatalf("list retry: %v", err)
	}
}

func TestPendingHostedOrders(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := &Store{pool: mock}
	mock.ExpectQuery("SELECT id, clinic_id, e164_number").
		WithArgs(5).
		WillReturnRows(pgxmock.NewRows([]string{"id", "clinic_id", "e164_number", "status", "last_error", "provider_order_id"}).
			AddRow(uuid.New(), uuid.New(), "+1555", "pending", "", "hno_1"))
	if _, err := store.PendingHostedOrders(context.Background(), 5); err != nil {
		t.Fatalf("pending hosted: %v", err)
	}
}
