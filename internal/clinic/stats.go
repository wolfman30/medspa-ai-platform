package clinic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// Stats represents per-clinic metrics as specified in revenue-mvp.md
type Stats struct {
	OrgID                string `json:"org_id"`
	ConversationsStarted int64  `json:"conversations_started"`
	DepositsRequested    int64  `json:"deposits_requested"`
	DepositsPaid         int64  `json:"deposits_paid"`
	DepositAmountTotal   int64  `json:"deposit_amount_total_cents"`
	PeriodStart          string `json:"period_start"`
	PeriodEnd            string `json:"period_end"`
}

// statsDB defines the database interface needed by StatsRepository
type statsDB interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// StatsRepository queries clinic metrics from the database.
type StatsRepository struct {
	db statsDB
}

// NewStatsRepository creates a new stats repository.
func NewStatsRepository(pool *pgxpool.Pool) *StatsRepository {
	if pool == nil {
		panic("clinic: pgx pool required for stats")
	}
	return &StatsRepository{db: pool}
}

// NewStatsRepositoryWithDB allows injecting a mock database for testing.
func NewStatsRepositoryWithDB(db statsDB) *StatsRepository {
	return &StatsRepository{db: db}
}

// GetStats retrieves aggregated metrics for a clinic.
// Optional start/end times for filtering. If nil, returns all-time stats.
func (r *StatsRepository) GetStats(ctx context.Context, orgID string, start, end *time.Time) (*Stats, error) {
	stats := &Stats{OrgID: orgID}

	// Build time filter clause
	var timeFilter string
	var args []interface{}
	args = append(args, orgID)
	argIdx := 2

	if start != nil && end != nil {
		timeFilter = fmt.Sprintf(" AND created_at >= $%d AND created_at < $%d", argIdx, argIdx+1)
		args = append(args, *start, *end)
		stats.PeriodStart = start.Format(time.RFC3339)
		stats.PeriodEnd = end.Format(time.RFC3339)
	} else {
		stats.PeriodStart = "all-time"
		stats.PeriodEnd = "now"
	}

	// Count leads (conversations_started)
	leadsQuery := `SELECT COUNT(*) FROM leads WHERE org_id = $1` + timeFilter
	if err := r.db.QueryRow(ctx, leadsQuery, args...).Scan(&stats.ConversationsStarted); err != nil {
		return nil, fmt.Errorf("clinic stats: count leads: %w", err)
	}

	// Count payments created (deposits_requested)
	paymentsQuery := `SELECT COUNT(*) FROM payments WHERE org_id = $1` + timeFilter
	if err := r.db.QueryRow(ctx, paymentsQuery, args...).Scan(&stats.DepositsRequested); err != nil {
		return nil, fmt.Errorf("clinic stats: count payments: %w", err)
	}

	// Count succeeded payments (deposits_paid)
	paidQuery := `SELECT COUNT(*) FROM payments WHERE org_id = $1 AND status = 'succeeded'` + timeFilter
	if err := r.db.QueryRow(ctx, paidQuery, args...).Scan(&stats.DepositsPaid); err != nil {
		return nil, fmt.Errorf("clinic stats: count paid: %w", err)
	}

	// Sum amount of succeeded payments (deposit_amount_total)
	amountQuery := `SELECT COALESCE(SUM(amount_cents), 0) FROM payments WHERE org_id = $1 AND status = 'succeeded'` + timeFilter
	if err := r.db.QueryRow(ctx, amountQuery, args...).Scan(&stats.DepositAmountTotal); err != nil {
		return nil, fmt.Errorf("clinic stats: sum amount: %w", err)
	}

	return stats, nil
}

// StatsHandler provides HTTP endpoints for clinic statistics.
type StatsHandler struct {
	repo   *StatsRepository
	logger *logging.Logger
}

// NewStatsHandler creates a new stats HTTP handler.
func NewStatsHandler(repo *StatsRepository, logger *logging.Logger) *StatsHandler {
	if logger == nil {
		logger = logging.Default()
	}
	return &StatsHandler{
		repo:   repo,
		logger: logger,
	}
}

// GetStats returns aggregated metrics for a clinic.
// GET /admin/clinics/{orgID}/stats
// Query params:
//   - start: RFC3339 timestamp for period start (optional)
//   - end: RFC3339 timestamp for period end (optional)
func (h *StatsHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		http.Error(w, `{"error": "org_id required"}`, http.StatusBadRequest)
		return
	}

	var start, end *time.Time
	if s := r.URL.Query().Get("start"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			http.Error(w, `{"error": "invalid start time, use RFC3339 format"}`, http.StatusBadRequest)
			return
		}
		start = &t
	}
	if e := r.URL.Query().Get("end"); e != "" {
		t, err := time.Parse(time.RFC3339, e)
		if err != nil {
			http.Error(w, `{"error": "invalid end time, use RFC3339 format"}`, http.StatusBadRequest)
			return
		}
		end = &t
	}

	// If only one is provided, require both
	if (start == nil) != (end == nil) {
		http.Error(w, `{"error": "both start and end must be provided, or neither"}`, http.StatusBadRequest)
		return
	}

	stats, err := h.repo.GetStats(r.Context(), orgID, start, end)
	if err != nil {
		h.logger.Error("failed to get clinic stats", "org_id", orgID, "error", err)
		http.Error(w, `{"error": "internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		h.logger.Error("failed to encode clinic stats", "org_id", orgID, "error", err)
	}
}
