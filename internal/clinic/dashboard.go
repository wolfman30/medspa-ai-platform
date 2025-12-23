package clinic

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

type dashboardDB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type dashboardRepo interface {
	MissedCallCohortByDay(ctx context.Context, orgID string, start, end time.Time) ([]MissedCallCohortDay, error)
}

// MissedCallCohortDay captures missed-call lead funnel counts by lead-created day.
type MissedCallCohortDay struct {
	Day             time.Time `json:"-"`
	DayLabel        string    `json:"day"`
	MissedCallLeads int64     `json:"missed_call_leads"`
	PaidLeads       int64     `json:"paid_leads"`
}

type LLMLatencySnapshot struct {
	Total   int64              `json:"total"`
	P90Ms   float64            `json:"p90_ms"`
	P95Ms   float64            `json:"p95_ms"`
	Buckets []LLMLatencyBucket `json:"buckets"`
}

type LLMLatencyBucket struct {
	LeSeconds float64 `json:"le_seconds"`
	Label     string  `json:"label,omitempty"`
	Count     int64   `json:"count"`
}

type ClinicDashboard struct {
	OrgID                   string               `json:"org_id"`
	PeriodStart             string               `json:"period_start"`
	PeriodEnd               string               `json:"period_end"`
	MissedCallLeads         int64                `json:"missed_call_leads"`
	MissedCallPaidLeads     int64                `json:"missed_call_paid_leads"`
	MissedCallConversionPct float64              `json:"missed_call_conversion_pct"`
	LLMLatency              LLMLatencySnapshot   `json:"llm_latency"`
	Daily                   []MissedCallCohortDay `json:"daily"`
}

// DashboardRepository queries clinic-level operational metrics from the database.
type DashboardRepository struct {
	db dashboardDB
}

func NewDashboardRepository(pool *pgxpool.Pool) *DashboardRepository {
	if pool == nil {
		panic("clinic: pgx pool required for dashboard")
	}
	return &DashboardRepository{db: pool}
}

func NewDashboardRepositoryWithDB(db dashboardDB) *DashboardRepository {
	return &DashboardRepository{db: db}
}

func (r *DashboardRepository) MissedCallCohortByDay(ctx context.Context, orgID string, start, end time.Time) ([]MissedCallCohortDay, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, fmt.Errorf("clinic dashboard: org_id required")
	}
	if end.Before(start) || end.Equal(start) {
		return nil, fmt.Errorf("clinic dashboard: invalid time range")
	}

	query := `
		SELECT date_trunc('day', l.created_at) AS day,
		       COUNT(*) AS missed_call_leads,
		       COUNT(DISTINCT p.lead_id) AS paid_leads
		FROM leads l
		LEFT JOIN payments p
		  ON p.lead_id = l.id
		 AND p.status = 'succeeded'
		WHERE l.org_id = $1
		  AND l.source IN ('twilio_voice', 'telnyx_voice')
		  AND l.created_at >= $2
		  AND l.created_at < $3
		GROUP BY day
		ORDER BY day
	`

	rows, err := r.db.Query(ctx, query, orgID, start, end)
	if err != nil {
		return nil, fmt.Errorf("clinic dashboard: query cohort: %w", err)
	}
	defer rows.Close()

	var results []MissedCallCohortDay
	for rows.Next() {
		var day time.Time
		var missed int64
		var paid int64
		if err := rows.Scan(&day, &missed, &paid); err != nil {
			return nil, fmt.Errorf("clinic dashboard: scan cohort: %w", err)
		}
		results = append(results, MissedCallCohortDay{
			Day:             day.UTC(),
			DayLabel:        day.UTC().Format("2006-01-02"),
			MissedCallLeads: missed,
			PaidLeads:       paid,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("clinic dashboard: iterate cohort: %w", err)
	}
	return results, nil
}

// DashboardHandler serves operational dashboard JSON for a clinic.
type DashboardHandler struct {
	repo     dashboardRepo
	gatherer prometheus.Gatherer
	logger   *logging.Logger
}

func NewDashboardHandler(repo dashboardRepo, gatherer prometheus.Gatherer, logger *logging.Logger) *DashboardHandler {
	if logger == nil {
		logger = logging.Default()
	}
	if gatherer == nil {
		gatherer = prometheus.DefaultGatherer
	}
	return &DashboardHandler{
		repo:     repo,
		gatherer: gatherer,
		logger:   logger,
	}
}

// GetDashboard returns clinic operational metrics.
// GET /admin/clinics/{orgID}/dashboard
// Query params:
//   - start: RFC3339 timestamp (optional, requires end)
//   - end: RFC3339 timestamp (optional, requires start)
//   - days: integer window (default 7) when start/end omitted
func (h *DashboardHandler) GetDashboard(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if strings.TrimSpace(orgID) == "" {
		http.Error(w, `{"error":"org_id required"}`, http.StatusBadRequest)
		return
	}
	if h.repo == nil {
		http.Error(w, `{"error":"dashboard disabled (db not configured)"}`, http.StatusServiceUnavailable)
		return
	}

	start, end, err := parseDashboardWindow(r)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	cohort, err := h.repo.MissedCallCohortByDay(r.Context(), orgID, start, end)
	if err != nil {
		h.logger.Error("failed to query dashboard cohort", "org_id", orgID, "error", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	cohort = fillMissingDays(cohort, start, end)

	var missedTotal int64
	var paidTotal int64
	for _, day := range cohort {
		missedTotal += day.MissedCallLeads
		paidTotal += day.PaidLeads
	}

	conversionPct := 0.0
	if missedTotal > 0 {
		conversionPct = (float64(paidTotal) / float64(missedTotal)) * 100.0
	}

	latency := snapshotLLMLatency(h.gatherer)

	resp := ClinicDashboard{
		OrgID:                   orgID,
		PeriodStart:             start.UTC().Format(time.RFC3339),
		PeriodEnd:               end.UTC().Format(time.RFC3339),
		MissedCallLeads:         missedTotal,
		MissedCallPaidLeads:     paidTotal,
		MissedCallConversionPct: conversionPct,
		LLMLatency:              latency,
		Daily:                   cohort,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func parseDashboardWindow(r *http.Request) (time.Time, time.Time, error) {
	q := r.URL.Query()

	startRaw := strings.TrimSpace(q.Get("start"))
	endRaw := strings.TrimSpace(q.Get("end"))
	if (startRaw == "") != (endRaw == "") {
		return time.Time{}, time.Time{}, fmt.Errorf("both start and end must be provided, or neither")
	}
	if startRaw != "" {
		start, err := time.Parse(time.RFC3339, startRaw)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid start time, use RFC3339 format")
		}
		end, err := time.Parse(time.RFC3339, endRaw)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end time, use RFC3339 format")
		}
		if !end.After(start) {
			return time.Time{}, time.Time{}, fmt.Errorf("end must be after start")
		}
		return start.UTC(), end.UTC(), nil
	}

	days := 7
	if raw := strings.TrimSpace(q.Get("days")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 90 {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid days; must be 1-90")
		}
		days = parsed
	}

	now := time.Now().UTC()
	end := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	start := end.AddDate(0, 0, -days)
	return start, end, nil
}

func fillMissingDays(existing []MissedCallCohortDay, start, end time.Time) []MissedCallCohortDay {
	startDay := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	endDay := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)

	lookup := map[string]MissedCallCohortDay{}
	for _, d := range existing {
		key := d.Day.UTC().Format("2006-01-02")
		lookup[key] = d
	}

	out := make([]MissedCallCohortDay, 0, int(endDay.Sub(startDay).Hours()/24)+1)
	for day := startDay; day.Before(endDay); day = day.AddDate(0, 0, 1) {
		key := day.Format("2006-01-02")
		if found, ok := lookup[key]; ok {
			out = append(out, found)
			continue
		}
		out = append(out, MissedCallCohortDay{
			Day:             day,
			DayLabel:        key,
			MissedCallLeads: 0,
			PaidLeads:       0,
		})
	}
	return out
}

func snapshotLLMLatency(gatherer prometheus.Gatherer) LLMLatencySnapshot {
	if gatherer == nil {
		gatherer = prometheus.DefaultGatherer
	}
	mfs, err := gatherer.Gather()
	if err != nil {
		return LLMLatencySnapshot{}
	}

	var family *dto.MetricFamily
	for _, mf := range mfs {
		if mf != nil && mf.GetName() == "medspa_conversation_llm_latency_seconds" {
			family = mf
			break
		}
	}
	if family == nil {
		return LLMLatencySnapshot{}
	}

	// Aggregate histograms across models, keeping only status="ok".
	cumulativeByUpper := map[float64]uint64{}
	var sampleCount uint64

	for _, metric := range family.Metric {
		if metric == nil {
			continue
		}
		if !hasLabel(metric, "status", "ok") {
			continue
		}
		h := metric.GetHistogram()
		if h == nil {
			continue
		}
		sampleCount += h.GetSampleCount()
		for _, b := range h.Bucket {
			if b == nil {
				continue
			}
			cumulativeByUpper[b.GetUpperBound()] += b.GetCumulativeCount()
		}
	}

	if sampleCount == 0 || len(cumulativeByUpper) == 0 {
		return LLMLatencySnapshot{}
	}

	uppers := make([]float64, 0, len(cumulativeByUpper))
	for upper := range cumulativeByUpper {
		uppers = append(uppers, upper)
	}
	sort.Float64s(uppers)

	buckets := make([]LLMLatencyBucket, 0, len(uppers))
	var prev uint64
	var lastFiniteUpper float64
	for _, upper := range uppers {
		cum := cumulativeByUpper[upper]
		if math.IsInf(upper, 1) {
			overflow := int64(0)
			if cum >= prev {
				overflow = int64(cum - prev)
			} else {
				overflow = int64(cum)
			}
			if overflow > 0 {
				buckets = append(buckets, LLMLatencyBucket{
					LeSeconds: lastFiniteUpper,
					Label:     fmt.Sprintf(">%s", formatSeconds(lastFiniteUpper)),
					Count:     overflow,
				})
			}
			prev = cum
			continue
		}

		lastFiniteUpper = upper
		count := int64(0)
		if cum >= prev {
			count = int64(cum - prev)
		} else {
			count = int64(cum)
		}
		buckets = append(buckets, LLMLatencyBucket{
			LeSeconds: upper,
			Count:     count,
		})
		prev = cum
	}

	p90 := histogramQuantile(0.90, sampleCount, uppers, cumulativeByUpper)
	p95 := histogramQuantile(0.95, sampleCount, uppers, cumulativeByUpper)

	return LLMLatencySnapshot{
		Total:   int64(sampleCount),
		P90Ms:   p90 * 1000.0,
		P95Ms:   p95 * 1000.0,
		Buckets: buckets,
	}
}

func hasLabel(metric *dto.Metric, name, value string) bool {
	for _, lp := range metric.Label {
		if lp == nil {
			continue
		}
		if lp.GetName() == name && lp.GetValue() == value {
			return true
		}
	}
	return false
}

func histogramQuantile(q float64, total uint64, uppers []float64, cumulativeByUpper map[float64]uint64) float64 {
	if total == 0 || q <= 0 {
		return 0
	}
	if q >= 1 {
		for i := len(uppers) - 1; i >= 0; i-- {
			if !math.IsInf(uppers[i], 1) {
				return uppers[i]
			}
		}
		return 0
	}

	target := q * float64(total)
	var prevUpper float64
	var prevCum float64

	for _, upper := range uppers {
		cum := float64(cumulativeByUpper[upper])
		if cum < target {
			prevUpper = upper
			prevCum = cum
			continue
		}

		// If we can't interpolate, return the bucket upper bound.
		bucketCount := cum - prevCum
		if bucketCount <= 0 || upper == prevUpper {
			return upper
		}
		if math.IsInf(upper, 1) {
			return prevUpper
		}

		fraction := (target - prevCum) / bucketCount
		if fraction < 0 {
			fraction = 0
		}
		if fraction > 1 {
			fraction = 1
		}

		lower := prevUpper
		return lower + fraction*(upper-lower)
	}

	return uppers[len(uppers)-1]
}

func formatSeconds(seconds float64) string {
	if seconds <= 0 {
		return "0s"
	}
	if seconds < 1 {
		return fmt.Sprintf("%.2fs", seconds)
	}
	if seconds < 10 {
		return fmt.Sprintf("%.1fs", seconds)
	}
	return fmt.Sprintf("%.0fs", seconds)
}
