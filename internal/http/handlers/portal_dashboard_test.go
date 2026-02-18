package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestPortalDashboardHandlerWithConversationTable(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	handler := NewPortalDashboardHandler(db, logging.Default())

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 8, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery("SELECT EXISTS\\(SELECT 1 FROM information_schema.tables").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM conversations WHERE org_id = \\$1 LIMIT 1").
		WithArgs("org-123").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	mock.ExpectQuery("(?s)SELECT COUNT\\(\\*\\) FROM conversations.*").
		WithArgs("org-123", start, end, "5005550001", "15005550001").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(8))

	mock.ExpectQuery("(?s)SELECT COUNT\\(\\*\\).*FROM payments p.*").
		WithArgs("org-123", start, end, "5005550001", "15005550001").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	mock.ExpectQuery("(?s)SELECT COALESCE\\(SUM\\(p.amount_cents\\), 0\\).*FROM payments p.*").
		WithArgs("org-123", start, end, "5005550001", "15005550001").
		WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(15000))

	req := httptest.NewRequest(http.MethodGet, "/portal/orgs/org-123/dashboard?start=2025-01-01T00:00:00Z&end=2025-01-08T00:00:00Z&phone=(500)%20555-0001", nil)
	req = withOrgParam(req, "org-123")
	rec := httptest.NewRecorder()

	handler.GetDashboard(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp PortalDashboardResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Conversations != 8 {
		t.Fatalf("expected conversations 8, got %d", resp.Conversations)
	}
	if resp.SuccessfulDeposits != 2 {
		t.Fatalf("expected successful deposits 2, got %d", resp.SuccessfulDeposits)
	}
	if resp.TotalCollectedCents != 15000 {
		t.Fatalf("expected total collected 15000, got %d", resp.TotalCollectedCents)
	}
	if resp.ConversionPct != 25.0 {
		t.Fatalf("expected conversion pct 25.0, got %f", resp.ConversionPct)
	}
	if resp.PeriodStart != "2025-01-01T00:00:00Z" || resp.PeriodEnd != "2025-01-08T00:00:00Z" {
		t.Fatalf("unexpected period window: %s - %s", resp.PeriodStart, resp.PeriodEnd)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestPortalDashboardHandlerFallbackToConversationJobs(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	handler := NewPortalDashboardHandler(db, logging.Default())

	mock.ExpectQuery("SELECT EXISTS\\(SELECT 1 FROM information_schema.tables").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	mock.ExpectQuery("(?s)SELECT COUNT\\(DISTINCT conversation_id\\) FROM conversation_jobs.*").
		WithArgs("sms:org-123:%", "5005550001", "15005550001").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	mock.ExpectQuery("(?s)SELECT COUNT\\(\\*\\).*FROM payments p.*").
		WithArgs("org-123", "5005550001", "15005550001").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	mock.ExpectQuery("(?s)SELECT COALESCE\\(SUM\\(p.amount_cents\\), 0\\).*FROM payments p.*").
		WithArgs("org-123", "5005550001", "15005550001").
		WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(4200))

	req := httptest.NewRequest(http.MethodGet, "/portal/orgs/org-123/dashboard?phone=5005550001", nil)
	req = withOrgParam(req, "org-123")
	rec := httptest.NewRecorder()

	handler.GetDashboard(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp PortalDashboardResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Conversations != 5 {
		t.Fatalf("expected conversations 5, got %d", resp.Conversations)
	}
	if resp.SuccessfulDeposits != 1 {
		t.Fatalf("expected successful deposits 1, got %d", resp.SuccessfulDeposits)
	}
	if resp.TotalCollectedCents != 4200 {
		t.Fatalf("expected total collected 4200, got %d", resp.TotalCollectedCents)
	}
	if resp.ConversionPct != 20.0 {
		t.Fatalf("expected conversion pct 20.0, got %f", resp.ConversionPct)
	}
	if resp.PeriodStart != "all-time" || resp.PeriodEnd != "now" {
		t.Fatalf("unexpected period window: %s - %s", resp.PeriodStart, resp.PeriodEnd)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func withOrgParam(req *http.Request, orgID string) *http.Request {
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("orgID", orgID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx)
	return req.WithContext(ctx)
}
