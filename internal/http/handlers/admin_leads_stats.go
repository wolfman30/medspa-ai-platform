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
	if err := h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM leads WHERE org_id = $1`, orgID,
	).Scan(&stats.TotalLeads); err != nil {
		h.logger.Error("failed to count total leads", "org_id", orgID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// By status
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT status, COUNT(*) FROM leads WHERE org_id = $1 GROUP BY status`, orgID,
	)
	if err != nil {
		h.logger.Error("failed to query lead status counts", "org_id", orgID, "error", err)
	} else {
		defer rows.Close()
		for rows.Next() {
			var status string
			var count int
			if err := rows.Scan(&status, &count); err != nil {
				h.logger.Error("failed to scan status row", "org_id", orgID, "error", err)
				continue
			}
			stats.ByStatus[status] = count
		}
		if err := rows.Err(); err != nil {
			h.logger.Error("status rows iteration failed", "org_id", orgID, "error", err)
		}
	}

	// New this week
	weekAgo := time.Now().AddDate(0, 0, -7)
	if err := h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM leads WHERE org_id = $1 AND created_at >= $2`, orgID, weekAgo,
	).Scan(&stats.NewThisWeek); err != nil {
		h.logger.Error("failed to count new leads this week", "org_id", orgID, "error", err)
	}

	// New this month
	monthAgo := time.Now().AddDate(0, -1, 0)
	if err := h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM leads WHERE org_id = $1 AND created_at >= $2`, orgID, monthAgo,
	).Scan(&stats.NewThisMonth); err != nil {
		h.logger.Error("failed to count new leads this month", "org_id", orgID, "error", err)
	}

	// Conversion rate (leads with successful payments / total leads)
	var converted int
	if err := h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(DISTINCT l.id) FROM leads l
		 JOIN payments p ON l.id = p.lead_id
		 WHERE l.org_id = $1 AND p.status = 'succeeded'`, orgID,
	).Scan(&converted); err != nil {
		h.logger.Error("failed to count converted leads", "org_id", orgID, "error", err)
	}
	if stats.TotalLeads > 0 {
		stats.ConversionRate = float64(converted) / float64(stats.TotalLeads) * 100
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}
