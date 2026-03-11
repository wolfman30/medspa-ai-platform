package middleware

import (
	"net/http"
	"strings"
)

// HTTPSRedirect redirects HTTP requests to HTTPS when running behind a
// load balancer that sets X-Forwarded-Proto. It is a no-op for health checks.
func HTTPSRedirect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip health/ready endpoints so load balancer probes work.
		if r.URL.Path == "/health" || r.URL.Path == "/ready" {
			next.ServeHTTP(w, r)
			return
		}

		proto := strings.ToLower(r.Header.Get("X-Forwarded-Proto"))
		if proto == "http" {
			target := "https://" + r.Host + r.URL.RequestURI()
			http.Redirect(w, r, target, http.StatusMovedPermanently)
			return
		}

		next.ServeHTTP(w, r)
	})
}
