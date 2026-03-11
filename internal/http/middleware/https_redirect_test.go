package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPSRedirect(t *testing.T) {
	handler := HTTPSRedirect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("redirects HTTP", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/admin", nil)
		req.Header.Set("X-Forwarded-Proto", "http")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusMovedPermanently {
			t.Errorf("expected 301, got %d", rr.Code)
		}
		loc := rr.Header().Get("Location")
		if loc != "https://example.com/admin" {
			t.Errorf("unexpected redirect location: %s", loc)
		}
	})

	t.Run("passes HTTPS through", func(t *testing.T) {
		req := httptest.NewRequest("GET", "https://example.com/admin", nil)
		req.Header.Set("X-Forwarded-Proto", "https")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})

	t.Run("skips health check", func(t *testing.T) {
		req := httptest.NewRequest("GET", "http://example.com/health", nil)
		req.Header.Set("X-Forwarded-Proto", "http")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 for health check, got %d", rr.Code)
		}
	})
}
