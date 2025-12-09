package clinic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestStatsRepository_GetStats_AllTime(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	orgID := "org-123"

	// Expect leads count query
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM leads WHERE org_id = \$1`).
		WithArgs(orgID).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(42)))

	// Expect payments count query
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM payments WHERE org_id = \$1`).
		WithArgs(orgID).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(15)))

	// Expect paid payments count query
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM payments WHERE org_id = \$1 AND status = 'succeeded'`).
		WithArgs(orgID).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(10)))

	// Expect sum amount query
	mock.ExpectQuery(`SELECT COALESCE\(SUM\(amount_cents\), 0\) FROM payments WHERE org_id = \$1 AND status = 'succeeded'`).
		WithArgs(orgID).
		WillReturnRows(pgxmock.NewRows([]string{"sum"}).AddRow(int64(500000)))

	repo := NewStatsRepositoryWithDB(mock)
	stats, err := repo.GetStats(context.Background(), orgID, nil, nil)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if stats.OrgID != orgID {
		t.Errorf("OrgID = %q, want %q", stats.OrgID, orgID)
	}
	if stats.ConversationsStarted != 42 {
		t.Errorf("ConversationsStarted = %d, want 42", stats.ConversationsStarted)
	}
	if stats.DepositsRequested != 15 {
		t.Errorf("DepositsRequested = %d, want 15", stats.DepositsRequested)
	}
	if stats.DepositsPaid != 10 {
		t.Errorf("DepositsPaid = %d, want 10", stats.DepositsPaid)
	}
	if stats.DepositAmountTotal != 500000 {
		t.Errorf("DepositAmountTotal = %d, want 500000", stats.DepositAmountTotal)
	}
	if stats.PeriodStart != "all-time" {
		t.Errorf("PeriodStart = %q, want 'all-time'", stats.PeriodStart)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestStatsRepository_GetStats_WithTimeRange(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	orgID := "org-456"
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)

	// Expect leads count query with time filter
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM leads WHERE org_id = \$1 AND created_at >= \$2 AND created_at < \$3`).
		WithArgs(orgID, start, end).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(20)))

	// Expect payments count query with time filter
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM payments WHERE org_id = \$1 AND created_at >= \$2 AND created_at < \$3`).
		WithArgs(orgID, start, end).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(8)))

	// Expect paid payments count query with time filter
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM payments WHERE org_id = \$1 AND status = 'succeeded' AND created_at >= \$2 AND created_at < \$3`).
		WithArgs(orgID, start, end).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(5)))

	// Expect sum amount query with time filter
	mock.ExpectQuery(`SELECT COALESCE\(SUM\(amount_cents\), 0\) FROM payments WHERE org_id = \$1 AND status = 'succeeded' AND created_at >= \$2 AND created_at < \$3`).
		WithArgs(orgID, start, end).
		WillReturnRows(pgxmock.NewRows([]string{"sum"}).AddRow(int64(250000)))

	repo := NewStatsRepositoryWithDB(mock)
	stats, err := repo.GetStats(context.Background(), orgID, &start, &end)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if stats.ConversationsStarted != 20 {
		t.Errorf("ConversationsStarted = %d, want 20", stats.ConversationsStarted)
	}
	if stats.DepositsPaid != 5 {
		t.Errorf("DepositsPaid = %d, want 5", stats.DepositsPaid)
	}
	if stats.DepositAmountTotal != 250000 {
		t.Errorf("DepositAmountTotal = %d, want 250000", stats.DepositAmountTotal)
	}
	if stats.PeriodStart != start.Format(time.RFC3339) {
		t.Errorf("PeriodStart = %q, want %q", stats.PeriodStart, start.Format(time.RFC3339))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestStatsHandler_GetStats(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create mock pool: %v", err)
	}
	defer mock.Close()

	orgID := "org-789"

	// Set up expectations for all queries
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM leads`).
		WithArgs(orgID).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(100)))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM payments WHERE org_id`).
		WithArgs(orgID).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(30)))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM payments WHERE org_id = \$1 AND status`).
		WithArgs(orgID).
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(int64(25)))
	mock.ExpectQuery(`SELECT COALESCE`).
		WithArgs(orgID).
		WillReturnRows(pgxmock.NewRows([]string{"sum"}).AddRow(int64(1250000)))

	repo := NewStatsRepositoryWithDB(mock)
	handler := NewStatsHandler(repo, logging.Default())

	// Create request with chi context
	r := chi.NewRouter()
	r.Get("/clinics/{orgID}/stats", handler.GetStats)

	req := httptest.NewRequest(http.MethodGet, "/clinics/"+orgID+"/stats", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var stats Stats
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if stats.OrgID != orgID {
		t.Errorf("OrgID = %q, want %q", stats.OrgID, orgID)
	}
	if stats.ConversationsStarted != 100 {
		t.Errorf("ConversationsStarted = %d, want 100", stats.ConversationsStarted)
	}
	if stats.DepositsPaid != 25 {
		t.Errorf("DepositsPaid = %d, want 25", stats.DepositsPaid)
	}
	if stats.DepositAmountTotal != 1250000 {
		t.Errorf("DepositAmountTotal = %d, want 1250000", stats.DepositAmountTotal)
	}
}

func TestStatsHandler_RequiresBothStartAndEnd(t *testing.T) {
	repo := NewStatsRepositoryWithDB(nil) // won't be used
	handler := NewStatsHandler(repo, logging.Default())

	r := chi.NewRouter()
	r.Get("/clinics/{orgID}/stats", handler.GetStats)

	// Only start provided
	req := httptest.NewRequest(http.MethodGet, "/clinics/org-1/stats?start=2025-01-01T00:00:00Z", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}

	// Only end provided
	req = httptest.NewRequest(http.MethodGet, "/clinics/org-1/stats?end=2025-02-01T00:00:00Z", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}
