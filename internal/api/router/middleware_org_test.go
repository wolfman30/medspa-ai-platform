package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireOrgIDPassesThrough(t *testing.T) {
	t.Helper()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		orgID, ok := orgIDFromRequest(r)
		if !ok || orgID != "org-abc" {
			t.Fatalf("expected org id propagated, got %s / %v", orgID, ok)
		}
		w.WriteHeader(http.StatusTeapot)
	})

	handler := requireOrgID(next)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(orgHeader, "org-abc")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTeapot {
		t.Fatalf("expected downstream status, got %d", rr.Code)
	}
}

func TestRequireOrgIDMissingHeader(t *testing.T) {
	handler := requireOrgID(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing org, got %d", rr.Code)
	}
}
