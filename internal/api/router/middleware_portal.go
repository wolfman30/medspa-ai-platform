package router

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	httpmiddleware "github.com/wolfman30/medspa-ai-platform/internal/http/middleware"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func requirePortalOrgOwner(db *sql.DB, logger *logging.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if db == nil {
				http.Error(w, `{"error":"portal disabled"}`, http.StatusServiceUnavailable)
				return
			}

			orgID := strings.TrimSpace(chi.URLParam(r, "orgID"))
			if orgID == "" {
				http.Error(w, `{"error":"missing orgID"}`, http.StatusBadRequest)
				return
			}

			claims, ok := httpmiddleware.CognitoClaimsFromContext(r.Context())
			if !ok || claims == nil {
				// Admin JWT path - allow through.
				next.ServeHTTP(w, r)
				return
			}

			if hasAdminGroup(claims.CognitoGroups) {
				next.ServeHTTP(w, r)
				return
			}

			email := strings.ToLower(strings.TrimSpace(claims.Email))
			if email == "" {
				email = strings.ToLower(strings.TrimSpace(claims.Username))
			}
			if email == "" {
				email = strings.ToLower(strings.TrimSpace(claims.CognitoUsername))
			}
			if email == "" {
				http.Error(w, `{"error":"missing user email"}`, http.StatusUnauthorized)
				return
			}

			var ownerEmail string
			if err := db.QueryRowContext(r.Context(),
				`SELECT COALESCE(owner_email, '') FROM organizations WHERE id = $1`, orgID,
			).Scan(&ownerEmail); err != nil {
				if err == sql.ErrNoRows {
					http.Error(w, `{"error":"organization not found"}`, http.StatusNotFound)
					return
				}
				if logger != nil {
					logger.Error("failed to check portal org ownership", "org_id", orgID, "error", err)
				}
				http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
				return
			}

			ownerEmail = strings.ToLower(strings.TrimSpace(ownerEmail))
			if ownerEmail == "" {
				http.Error(w, `{"error":"portal access not configured"}`, http.StatusForbidden)
				return
			}
			if ownerEmail != email {
				http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func hasAdminGroup(groups []string) bool {
	for _, g := range groups {
		switch strings.ToLower(strings.TrimSpace(g)) {
		case "admin", "admins":
			return true
		}
	}
	return false
}
