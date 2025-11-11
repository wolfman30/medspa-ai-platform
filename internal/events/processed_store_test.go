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

	mock.ExpectQuery("SELECT 1 FROM processed_events").WithArgs("square", "evt").WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(1))
	processed, err := store.AlreadyProcessed(context.Background(), "square", "evt")
	if err != nil || !processed {
		t.Fatalf("expected existing row, got processed=%v err=%v", processed, err)
	}

	mock.ExpectQuery("SELECT 1 FROM processed_events").WithArgs("square", "evt-miss").WillReturnError(pgx.ErrNoRows)
	processed, err = store.AlreadyProcessed(context.Background(), "square", "evt-miss")
	if err != nil || processed {
		t.Fatalf("expected missing row, got processed=%v err=%v", processed, err)
	}

	mock.ExpectExec("INSERT INTO processed_events").WithArgs("square", "evt-new").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	ok, err := store.MarkProcessed(context.Background(), "square", "evt-new")
	if err != nil || !ok {
		t.Fatalf("expected mark processed success, got %v %v", ok, err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
