package events

import (
	"context"
	"testing"

	pgx "github.com/jackc/pgx/v5"
	pgxmock "github.com/pashagolub/pgxmock/v4"
)

func TestProcessedStore(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create pgx mock: %v", err)
	}
	defer mock.Close()

	store := newProcessedStoreWithExec(mock)

	eventUUID, _, _, err := normalizeProcessedEvent("square", "evt")
	if err != nil {
		t.Fatalf("normalize event: %v", err)
	}
	mock.ExpectQuery("SELECT 1 FROM processed_events").WithArgs(eventUUID).WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(1))
	processed, err := store.AlreadyProcessed(context.Background(), "square", "evt")
	if err != nil || !processed {
		t.Fatalf("expected existing row, got processed=%v err=%v", processed, err)
	}

	missUUID, _, _, err := normalizeProcessedEvent("square", "evt-miss")
	if err != nil {
		t.Fatalf("normalize event: %v", err)
	}
	mock.ExpectQuery("SELECT 1 FROM processed_events").WithArgs(missUUID).WillReturnError(pgx.ErrNoRows)
	processed, err = store.AlreadyProcessed(context.Background(), "square", "evt-miss")
	if err != nil || processed {
		t.Fatalf("expected missing row, got processed=%v err=%v", processed, err)
	}

	insertUUID, _, _, err := normalizeProcessedEvent("square", "evt-new")
	if err != nil {
		t.Fatalf("normalize insert: %v", err)
	}
	mock.ExpectExec("INSERT INTO processed_events").WithArgs(insertUUID, "square", "evt-new").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	ok, err := store.MarkProcessed(context.Background(), "square", "evt-new")
	if err != nil || !ok {
		t.Fatalf("expected mark processed success, got %v %v", ok, err)
	}

	if _, _, _, err := normalizeProcessedEvent("square", ""); err == nil {
		t.Fatalf("expected error for empty event id")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestNewProcessedStorePanicsOnNilPool(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for nil pool")
		}
	}()
	NewProcessedStore(nil)
}
