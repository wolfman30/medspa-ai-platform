package stories

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"
)

func newTestHandler(t *testing.T) (*Handler, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	return NewHandler(NewRepository(db)), mock, func() { _ = db.Close() }
}

func withChiID(req *http.Request, id string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	return req.WithContext(ctx)
}

func TestHandler_List_SuccessAndFilterParams(t *testing.T) {
	h, mock, cleanup := newTestHandler(t)
	defer cleanup()

	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{"id", "title", "description", "status", "priority", "labels", "parent_id", "assigned_to", "created_at", "updated_at", "completed_at", "sub_task_count"}).
		AddRow("s1", "Story 1", "Desc", "backlog", "high", "{frontend,bug}", nil, "alice", now, now, nil, 2)

	mock.ExpectQuery("SELECT s.id, s.title").WithArgs("backlog", "high", "frontend").WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/admin/stories?status=backlog&priority=high&label=frontend", nil)
	rr := httptest.NewRecorder()
	h.List(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var got struct{ Stories []Story `json:"stories"` }
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Stories) != 1 || got.Stories[0].ID != "s1" {
		t.Fatalf("unexpected stories payload: %+v", got.Stories)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestHandler_List_InternalErrorDoesNotLeak(t *testing.T) {
	h, mock, cleanup := newTestHandler(t)
	defer cleanup()

	mock.ExpectQuery("SELECT s.id, s.title").WillReturnError(errors.New("pq: password authentication failed"))
	req := httptest.NewRequest(http.MethodGet, "/admin/stories", nil)
	rr := httptest.NewRecorder()
	h.List(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, internalServerErrorMessage) || strings.Contains(body, "password authentication failed") {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestHandler_Create_Cases(t *testing.T) {
	h, mock, cleanup := newTestHandler(t)
	defer cleanup()

	t.Run("invalid json", func(t *testing.T) {
		rr := httptest.NewRecorder()
		h.Create(rr, httptest.NewRequest(http.MethodPost, "/admin/stories", strings.NewReader("{")))
		if rr.Code != http.StatusBadRequest { t.Fatalf("expected 400, got %d", rr.Code) }
	})

	t.Run("missing title", func(t *testing.T) {
		rr := httptest.NewRecorder()
		h.Create(rr, httptest.NewRequest(http.MethodPost, "/admin/stories", strings.NewReader(`{"description":"d"}`)))
		if rr.Code != http.StatusBadRequest { t.Fatalf("expected 400, got %d", rr.Code) }
	})

	t.Run("success", func(t *testing.T) {
		now := time.Now().UTC()
		rows := sqlmock.NewRows([]string{"id", "title", "description", "status", "priority", "labels", "parent_id", "assigned_to", "created_at", "updated_at", "completed_at"}).
			AddRow("s1", "Title", "Desc", "backlog", "medium", "{frontend}", nil, "alice", now, now, nil)
		mock.ExpectQuery("INSERT INTO stories").WithArgs("Title", "Desc", "backlog", "medium", sqlmock.AnyArg(), nil, "alice").WillReturnRows(rows)
		rr := httptest.NewRecorder()
		h.Create(rr, httptest.NewRequest(http.MethodPost, "/admin/stories", strings.NewReader(`{"title":"Title","description":"Desc","status":"backlog","priority":"medium","labels":["frontend"],"assignedTo":"alice"}`)))
		if rr.Code != http.StatusCreated { t.Fatalf("expected 201, got %d body=%s", rr.Code, rr.Body.String()) }
	})

	t.Run("repo error hidden", func(t *testing.T) {
		mock.ExpectQuery("INSERT INTO stories").WillReturnError(errors.New("pq: duplicate key"))
		rr := httptest.NewRecorder()
		h.Create(rr, httptest.NewRequest(http.MethodPost, "/admin/stories", strings.NewReader(`{"title":"Title"}`)))
		if rr.Code != http.StatusInternalServerError { t.Fatalf("expected 500, got %d", rr.Code) }
		if strings.Contains(rr.Body.String(), "duplicate") { t.Fatalf("leak: %s", rr.Body.String()) }
	})

	if err := mock.ExpectationsWereMet(); err != nil { t.Fatalf("expectations: %v", err) }
}

func TestHandler_Update_Cases(t *testing.T) {
	h, mock, cleanup := newTestHandler(t)
	defer cleanup()

	rr := httptest.NewRecorder()
	h.Update(rr, httptest.NewRequest(http.MethodPut, "/admin/stories/", strings.NewReader(`{}`)))
	if rr.Code != http.StatusBadRequest { t.Fatalf("expected 400, got %d", rr.Code) }

	rr = httptest.NewRecorder()
	h.Update(rr, withChiID(httptest.NewRequest(http.MethodPut, "/admin/stories/s1", strings.NewReader("{")), "s1"))
	if rr.Code != http.StatusBadRequest { t.Fatalf("expected 400, got %d", rr.Code) }

	mock.ExpectQuery("SELECT s.id, s.title").WithArgs("s404").WillReturnError(sql.ErrNoRows)
	rr = httptest.NewRecorder()
	h.Update(rr, withChiID(httptest.NewRequest(http.MethodPut, "/admin/stories/s404", strings.NewReader(`{"title":"x"}`)), "s404"))
	if rr.Code != http.StatusNotFound { t.Fatalf("expected 404, got %d", rr.Code) }

	now := time.Now().UTC()
	mock.ExpectQuery("SELECT s.id, s.title").WithArgs("s1").WillReturnRows(
		sqlmock.NewRows([]string{"id", "title", "description", "status", "priority", "labels", "parent_id", "assigned_to", "created_at", "updated_at", "completed_at", "sub_task_count"}).
			AddRow("s1", "Old", "Old Desc", "backlog", "medium", "{frontend}", nil, "alice", now, now, nil, 0),
	)
	mock.ExpectQuery("UPDATE stories SET").WithArgs("New", "Old Desc", "backlog", "medium", sqlmock.AnyArg(), nil, "alice", nil, "s1").WillReturnRows(
		sqlmock.NewRows([]string{"id", "title", "description", "status", "priority", "labels", "parent_id", "assigned_to", "created_at", "updated_at", "completed_at"}).
			AddRow("s1", "New", "Old Desc", "backlog", "medium", "{frontend}", nil, "alice", now, now, nil),
	)
	rr = httptest.NewRecorder()
	h.Update(rr, withChiID(httptest.NewRequest(http.MethodPut, "/admin/stories/s1", strings.NewReader(`{"title":"New"}`)), "s1"))
	if rr.Code != http.StatusOK { t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String()) }

	mock.ExpectQuery("SELECT s.id, s.title").WithArgs("s2").WillReturnError(errors.New("pq: connection refused"))
	rr = httptest.NewRecorder()
	h.Update(rr, withChiID(httptest.NewRequest(http.MethodPut, "/admin/stories/s2", strings.NewReader(`{"title":"New"}`)), "s2"))
	if rr.Code != http.StatusInternalServerError { t.Fatalf("expected 500, got %d", rr.Code) }
	if strings.Contains(rr.Body.String(), "connection refused") { t.Fatalf("leak: %s", rr.Body.String()) }

	if err := mock.ExpectationsWereMet(); err != nil { t.Fatalf("expectations: %v", err) }
}

func TestHandler_Delete_Cases(t *testing.T) {
	h, mock, cleanup := newTestHandler(t)
	defer cleanup()

	rr := httptest.NewRecorder()
	h.Delete(rr, httptest.NewRequest(http.MethodDelete, "/admin/stories/", nil))
	if rr.Code != http.StatusBadRequest { t.Fatalf("expected 400, got %d", rr.Code) }

	mock.ExpectExec("DELETE FROM stories").WithArgs("s1").WillReturnError(errors.New("pq: timeout"))
	rr = httptest.NewRecorder()
	h.Delete(rr, withChiID(httptest.NewRequest(http.MethodDelete, "/admin/stories/s1", nil), "s1"))
	if rr.Code != http.StatusInternalServerError { t.Fatalf("expected 500, got %d", rr.Code) }
	if strings.Contains(rr.Body.String(), "timeout") { t.Fatalf("leak: %s", rr.Body.String()) }

	mock.ExpectExec("DELETE FROM stories").WithArgs("s2").WillReturnResult(sqlmock.NewResult(0, 1))
	rr = httptest.NewRecorder()
	h.Delete(rr, withChiID(httptest.NewRequest(http.MethodDelete, "/admin/stories/s2", nil), "s2"))
	if rr.Code != http.StatusNoContent { t.Fatalf("expected 204, got %d", rr.Code) }

	if err := mock.ExpectationsWereMet(); err != nil { t.Fatalf("expectations: %v", err) }
}

func TestHandler_Get_Cases(t *testing.T) {
	h, mock, cleanup := newTestHandler(t)
	defer cleanup()

	rr := httptest.NewRecorder()
	h.Get(rr, httptest.NewRequest(http.MethodGet, "/admin/stories/", nil))
	if rr.Code != http.StatusBadRequest { t.Fatalf("expected 400, got %d", rr.Code) }

	mock.ExpectQuery("SELECT s.id, s.title").WithArgs("s404").WillReturnError(sql.ErrNoRows)
	rr = httptest.NewRecorder()
	h.Get(rr, withChiID(httptest.NewRequest(http.MethodGet, "/admin/stories/s404", nil), "s404"))
	if rr.Code != http.StatusNotFound { t.Fatalf("expected 404, got %d", rr.Code) }

	now := time.Now().UTC()
	mock.ExpectQuery("SELECT s.id, s.title").WithArgs("s1").WillReturnRows(
		sqlmock.NewRows([]string{"id", "title", "description", "status", "priority", "labels", "parent_id", "assigned_to", "created_at", "updated_at", "completed_at", "sub_task_count"}).
			AddRow("s1", "Story", "Desc", "backlog", "medium", "{}", nil, "", now, now, nil, 0),
	)
	mock.ExpectQuery("FROM stories s WHERE s.parent_id = \\$1").WithArgs("s1").WillReturnError(errors.New("pq: relation missing"))
	rr = httptest.NewRecorder()
	h.Get(rr, withChiID(httptest.NewRequest(http.MethodGet, "/admin/stories/s1", nil), "s1"))
	if rr.Code != http.StatusInternalServerError { t.Fatalf("expected 500, got %d", rr.Code) }
	if strings.Contains(rr.Body.String(), "relation") { t.Fatalf("leak: %s", rr.Body.String()) }

	mock.ExpectQuery("SELECT s.id, s.title").WithArgs("s2").WillReturnRows(
		sqlmock.NewRows([]string{"id", "title", "description", "status", "priority", "labels", "parent_id", "assigned_to", "created_at", "updated_at", "completed_at", "sub_task_count"}).
			AddRow("s2", "Story2", "Desc2", "in_progress", "high", "{backend}", nil, "bob", now, now, nil, 1),
	)
	mock.ExpectQuery("FROM stories s WHERE s.parent_id = \\$1").WithArgs("s2").WillReturnRows(
		sqlmock.NewRows([]string{"id", "title", "description", "status", "priority", "labels", "parent_id", "assigned_to", "created_at", "updated_at", "completed_at", "sub_task_count"}).
			AddRow("sub1", "Sub", "SubD", "backlog", "low", "{}", "s2", "", now, now, nil, 0),
	)
	rr = httptest.NewRecorder()
	h.Get(rr, withChiID(httptest.NewRequest(http.MethodGet, "/admin/stories/s2", nil), "s2"))
	if rr.Code != http.StatusOK { t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String()) }

	if err := mock.ExpectationsWereMet(); err != nil { t.Fatalf("expectations: %v", err) }
}
