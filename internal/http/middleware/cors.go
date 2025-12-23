package middleware

import (
	"net/http"
	"strings"
)

// CORS provides a simple allowlist-based CORS middleware.
// If allowedOrigins contains "*", any Origin is echoed back.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	allowAny := false
	allow := map[string]struct{}{}
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		if origin == "*" {
			allowAny = true
			continue
		}
		allow[origin] = struct{}{}
	}

	allowedHeaders := "Authorization, Content-Type, X-Org-Id"
	allowedMethods := "GET, POST, PUT, PATCH, DELETE, OPTIONS"

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin != "" && (allowAny || isAllowedOrigin(allow, origin)) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Headers", allowedHeaders)
				w.Header().Set("Access-Control-Allow-Methods", allowedMethods)
				w.Header().Set("Access-Control-Max-Age", "600")
			}

			// Handle preflight requests.
			if r.Method == http.MethodOptions && origin != "" && r.Header.Get("Access-Control-Request-Method") != "" {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isAllowedOrigin(allow map[string]struct{}, origin string) bool {
	_, ok := allow[origin]
	return ok
}
