package events

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	pgxmock "github.com/pashagolub/pgxmock/v4"
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
	rows := pgxmock.NewRows([]string{"id", "org_id", "type", "payload", "created_at"}).AddRow(id, "org-1", "event.v1", []byte("{\"foo\":\"bar\"}"), now)
	mock.ExpectQuery("SELECT id").WithArgs(int32(10)).WillReturnRows(rows)

	entries, err := store.FetchPending(context.Background(), 10)
	if err != nil {
		t.Fatalf("fetch pending failed: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != id {
		t.Fatalf("unexpected entries: %#v", entries)
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
