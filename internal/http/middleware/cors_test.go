package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSAllowsListedOrigin(t *testing.T) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	mw := CORS([]string{"https://example.com"})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()

	mw(handler).ServeHTTP(rec, req)

	if !called {
		t.Fatalf("expected handler to be called")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("expected allow origin header, got %q", got)
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Fatalf("expected allow methods header")
	}
	if rec.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Fatalf("expected allow headers header")
	}
}

func TestCORSDeniesUnknownOrigin(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := CORS([]string{"https://example.com"})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://unknown.example")
	rec := httptest.NewRecorder()

	mw(handler).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no allow origin header, got %q", got)
	}
}

func TestCORSAllowsAnyOrigin(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := CORS([]string{"*"})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://random.example")
	rec := httptest.NewRecorder()

	mw(handler).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://random.example" {
		t.Fatalf("expected allow origin header, got %q", got)
	}
}

func TestCORSHandlesPreflight(t *testing.T) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	mw := CORS([]string{"https://example.com"})
	req := httptest.NewRequest(http.MethodOptions, "/health", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()

	mw(handler).ServeHTTP(rec, req)

	if called {
		t.Fatalf("expected handler to not be called on preflight")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rec.Code)
	}
}
