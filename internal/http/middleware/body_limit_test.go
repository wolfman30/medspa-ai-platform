package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMaxBodySize(t *testing.T) {
	handler := MaxBodySize(100)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("small body passes", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", strings.NewReader("hello"))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})

	t.Run("oversized body rejected", func(t *testing.T) {
		bigBody := strings.Repeat("x", 200)
		req := httptest.NewRequest("POST", "/", strings.NewReader(bigBody))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("expected 413, got %d", rr.Code)
		}
	})
}
