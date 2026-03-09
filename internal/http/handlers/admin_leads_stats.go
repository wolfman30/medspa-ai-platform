package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// GetLeadStats returns aggregated lead statistics.
// GET /admin/orgs/{orgID}/leads/stats
func (h *AdminLeadsHandler) GetLeadStats(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		http.Error(w, "missing orgID", http.StatusBadRequest)
		return
	}

	stats := LeadStatsResponse{
		ByStatus: make(map[string]int),
	}

	// Total leads
	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM leads WHERE org_id = $1`, orgID,
	).Scan(&stats.TotalLeads)

	// By status
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT status, COUNT(*) FROM leads WHERE org_id = $1 GROUP BY status`, orgID,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var status string
			var count int
			if rows.Scan(&status, &count) == nil {
				stats.ByStatus[status] = count
			}
		}
	}

	// New this week
	weekAgo := time.Now().AddDate(0, 0, -7)
	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM leads WHERE org_id = $1 AND created_at >= $2`, orgID, weekAgo,
	).Scan(&stats.NewThisWeek)

	// New this month
	monthAgo := time.Now().AddDate(0, -1, 0)
	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM leads WHERE org_id = $1 AND created_at >= $2`, orgID, monthAgo,
	).Scan(&stats.NewThisMonth)

	// Conversion rate (leads with successful payments / total leads)
	var converted int
	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(DISTINCT l.id) FROM leads l
		 JOIN payments p ON l.id = p.lead_id
		 WHERE l.org_id = $1 AND p.status = 'succeeded'`, orgID,
	).Scan(&converted)
	if stats.TotalLeads > 0 {
		stats.ConversionRate = float64(converted) / float64(stats.TotalLeads) * 100
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}
