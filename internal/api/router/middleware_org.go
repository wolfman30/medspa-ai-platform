package router

import (
	"net/http"
	"strings"

	"github.com/wolfman30/medspa-ai-platform/internal/tenancy"
)

const orgHeader = "X-Org-Id"

// requireOrgID middleware enforces multi-tenancy headers for API requests.
func requireOrgID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		orgID := strings.TrimSpace(r.Header.Get(orgHeader))
		if orgID == "" {
			http.Error(w, "missing X-Org-Id", http.StatusBadRequest)
			return
		}
		ctx := tenancy.WithOrgID(r.Context(), orgID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// orgIDFromRequest exposes the org id for local handlers.
func orgIDFromRequest(r *http.Request) (string, bool) {
	return tenancy.OrgIDFromContext(r.Context())
}

