package router

import (
	"net/http"
	"strings"
)

const onboardingTokenHeader = "X-Onboarding-Token"
const onboardingTokenQuery = "onboarding_token"

// requireOnboardingToken enforces an invite token for onboarding/knowledge endpoints.
// When expected is empty, the middleware is a no-op.
func requireOnboardingToken(expected string) func(http.Handler) http.Handler {
	expected = strings.TrimSpace(expected)
	return func(next http.Handler) http.Handler {
		if expected == "" {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := strings.TrimSpace(r.Header.Get(onboardingTokenHeader))
			if token == "" {
				token = strings.TrimSpace(r.URL.Query().Get(onboardingTokenQuery))
			}
			if token == "" || token != expected {
				http.Error(w, "invalid onboarding token", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
