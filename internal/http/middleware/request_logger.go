package middleware

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// RequestLogger emits structured logs for every HTTP request.
func RequestLogger(logger *logging.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = logging.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			reqID := r.Header.Get("X-Request-ID")
			if reqID == "" {
				reqID = uuid.NewString()
			}
			logger.Info("request started",
				"method", r.Method,
				"path", r.URL.Path,
				"request_id", reqID,
				"remote_ip", r.RemoteAddr,
			)
			next.ServeHTTP(w, r)
			logger.Info("request completed",
				"method", r.Method,
				"path", r.URL.Path,
				"request_id", reqID,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}
