package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

const defaultEnterpriseSubscriptionCost = 5000.0

type RevenueDashboardHandler struct {
	db         *sql.DB
	clinic     *clinic.Store
	logger     *logging.Logger
	subCostUSD float64
}

func NewRevenueDashboardHandler(db *sql.DB, clinicStore *clinic.Store, logger *logging.Logger) *RevenueDashboardHandler {
	if logger == nil {
		logger = logging.Default()
	}
	return &RevenueDashboardHandler{db: db, clinic: clinicStore, logger: logger, subCostUSD: defaultEnterpriseSubscriptionCost}
}

type RevenueDashboardResponse struct {
	OrgID                  string                  `json:"org_id"`
	Period                 string                  `json:"period"`
	RevenueRecovered       float64                 `json:"revenue_recovered"`
	MissedCallsCaught      int                     `json:"missed_calls_caught"`
	ConversationsStarted   int                     `json:"conversations_started"`
	AppointmentsBooked     int                     `json:"appointments_booked"`
	DepositsCollected      float64                 `json:"deposits_collected"`
	AvgResponseTimeSeconds float64                 `json:"avg_response_time_seconds"`
	ROIMultiplier          float64                 `json:"roi_multiplier"`
	TopServices            []RevenueTopService     `json:"top_services"`
	Funnel                 RevenueFunnel           `json:"funnel"`
	DailyBreakdown         []RevenueDailyBreakdown `json:"daily_breakdown"`
	SubscriptionCost       float64                 `json:"subscription_cost"`
}

type RevenueTopService struct {
	Service string  `json:"service"`
	Count   int     `json:"count"`
	Revenue float64 `json:"revenue"`
}

type RevenueFunnelStage struct {
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
}

type RevenueFunnel struct {
	MissedCalls   RevenueFunnelStage `json:"missed_calls"`
	Conversations RevenueFunnelStage `json:"conversations"`
	Qualified     RevenueFunnelStage `json:"qualified"`
	Booked        RevenueFunnelStage `json:"booked"`
}

type RevenueDailyBreakdown struct {
	Date               string  `json:"date"`
	Conversations      int     `json:"conversations"`
	AppointmentsBooked int     `json:"appointments_booked"`
	RevenueRecovered   float64 `json:"revenue_recovered"`
	DepositsCollected  float64 `json:"deposits_collected"`
}

func (h *RevenueDashboardHandler) GetRevenueDashboard(w http.ResponseWriter, r *http.Request) {
	orgID := strings.TrimSpace(r.URL.Query().Get("org_id"))
	if orgID == "" {
		http.Error(w, "missing org_id", http.StatusBadRequest)
		return
	}

	period := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("period")))
	if period == "" {
		period = "month"
	}
	startAt, ok := periodStart(period)
	if !ok {
		http.Error(w, "invalid period: must be week|month|all", http.StatusBadRequest)
		return
	}

	resp := RevenueDashboardResponse{
		OrgID:            orgID,
		Period:           period,
		TopServices:      []RevenueTopService{},
		DailyBreakdown:   []RevenueDailyBreakdown{},
		SubscriptionCost: h.subCostUSD,
	}

	if err := h.loadCoreMetrics(r, &resp, startAt); err != nil {
		h.logger.Error("revenue dashboard core metrics query failed", "org_id", orgID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := h.loadResponseTimeMetric(r, &resp, startAt); err != nil {
		h.logger.Error("revenue dashboard response time query failed", "org_id", orgID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := h.loadServiceAttribution(r, &resp, startAt); err != nil {
		h.logger.Error("revenue dashboard service attribution failed", "org_id", orgID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := h.loadDailyBreakdown(r, &resp, startAt); err != nil {
		h.logger.Error("revenue dashboard daily breakdown failed", "org_id", orgID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if resp.SubscriptionCost > 0 {
		resp.ROIMultiplier = round2(resp.RevenueRecovered / resp.SubscriptionCost)
	}
	resp.Funnel = buildFunnel(resp.MissedCallsCaught, resp.ConversationsStarted, resp.Funnel.Qualified.Count, resp.AppointmentsBooked)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("revenue dashboard encode failed", "org_id", orgID, "error", err)
	}
}

func (h *RevenueDashboardHandler) loadCoreMetrics(r *http.Request, resp *RevenueDashboardResponse, startAt *time.Time) error {
	where, args := orgRangeClause("org_id", "created_at", resp.OrgID, startAt)
	q := `
		SELECT
			COUNT(*) AS missed_calls,
			COUNT(*) FILTER (WHERE COALESCE(ai_message_count, 0) > 0) AS conversations_started,
			COUNT(*) FILTER (WHERE status = 'booked') AS appointments_booked,
			COUNT(*) FILTER (WHERE status IN ('qualified', 'booked')) AS qualified
		FROM conversations` + where
	if err := h.db.QueryRowContext(r.Context(), q, args...).Scan(
		&resp.MissedCallsCaught,
		&resp.ConversationsStarted,
		&resp.AppointmentsBooked,
		&resp.Funnel.Qualified.Count,
	); err != nil {
		return err
	}

	pWhere, pArgs := orgRangeClause("org_id", "created_at", resp.OrgID, startAt)
	pq := `SELECT COALESCE(SUM(amount_cents), 0) FROM payments` + pWhere + ` AND status IN ('succeeded', 'paid', 'completed')`
	var cents int64
	if err := h.db.QueryRowContext(r.Context(), pq, pArgs...).Scan(&cents); err != nil {
		return err
	}
	resp.DepositsCollected = round2(float64(cents) / 100.0)
	return nil
}

func (h *RevenueDashboardHandler) loadResponseTimeMetric(r *http.Request, resp *RevenueDashboardResponse, startAt *time.Time) error {
	where, args := orgRangeClause("c.org_id", "c.created_at", resp.OrgID, startAt)
	q := `
		WITH ordered AS (
			SELECT
				cm.conversation_id,
				cm.role,
				cm.created_at,
				LEAD(cm.role) OVER (PARTITION BY cm.conversation_id ORDER BY cm.created_at) AS next_role,
				LEAD(cm.created_at) OVER (PARTITION BY cm.conversation_id ORDER BY cm.created_at) AS next_created_at
			FROM conversation_messages cm
			JOIN conversations c ON c.conversation_id = cm.conversation_id` + where + `
		)
		SELECT COALESCE(AVG(EXTRACT(EPOCH FROM (next_created_at - created_at))), 0)
		FROM ordered
		WHERE role = 'user' AND next_role = 'assistant' AND next_created_at IS NOT NULL`

	var avg float64
	if err := h.db.QueryRowContext(r.Context(), q, args...).Scan(&avg); err != nil {
		return err
	}
	resp.AvgResponseTimeSeconds = round2(avg)
	return nil
}

func (h *RevenueDashboardHandler) loadServiceAttribution(r *http.Request, resp *RevenueDashboardResponse, startAt *time.Time) error {
	prices := h.getServicePrices(r.Context(), resp.OrgID)
	where, args := orgRangeClause("c.org_id", "c.created_at", resp.OrgID, startAt)
	q := `
		SELECT COALESCE(NULLIF(TRIM(l.selected_service), ''), 'Unknown Service') AS service_name, COUNT(*)
		FROM conversations c
		LEFT JOIN leads l ON l.id = c.lead_id` + where + ` AND c.status = 'booked'
		GROUP BY 1`
	rows, err := h.db.QueryContext(r.Context(), q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	services := make([]RevenueTopService, 0)
	totalRevenue := 0.0
	for rows.Next() {
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			return err
		}
		price := prices[strings.ToLower(strings.TrimSpace(name))]
		revenue := float64(count) * price
		totalRevenue += revenue
		services = append(services, RevenueTopService{Service: name, Count: count, Revenue: round2(revenue)})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	sort.Slice(services, func(i, j int) bool { return services[i].Revenue > services[j].Revenue })
	if len(services) > 6 {
		services = services[:6]
	}
	resp.TopServices = services
	resp.RevenueRecovered = round2(totalRevenue)
	return nil
}

func (h *RevenueDashboardHandler) loadDailyBreakdown(r *http.Request, resp *RevenueDashboardResponse, startAt *time.Time) error {
	prices := h.getServicePrices(r.Context(), resp.OrgID)
	where, args := orgRangeClause("c.org_id", "c.created_at", resp.OrgID, startAt)

	daily := map[string]*RevenueDailyBreakdown{}

	convQ := `
		SELECT DATE(c.created_at) AS d,
			COUNT(*) AS conversations,
			COUNT(*) FILTER (WHERE c.status = 'booked') AS booked
		FROM conversations c` + where + `
		GROUP BY 1`
	rows, err := h.db.QueryContext(r.Context(), convQ, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var d time.Time
		var conv, booked int
		if err := rows.Scan(&d, &conv, &booked); err != nil {
			rows.Close()
			return err
		}
		key := d.Format("2006-01-02")
		daily[key] = &RevenueDailyBreakdown{Date: key, Conversations: conv, AppointmentsBooked: booked}
	}
	rows.Close()

	revQ := `
		SELECT DATE(c.created_at) AS d, COALESCE(NULLIF(TRIM(l.selected_service), ''), 'Unknown Service') AS service_name, COUNT(*)
		FROM conversations c
		LEFT JOIN leads l ON l.id = c.lead_id` + where + ` AND c.status = 'booked'
		GROUP BY 1,2`
	rows, err = h.db.QueryContext(r.Context(), revQ, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var d time.Time
		var service string
		var count int
		if err := rows.Scan(&d, &service, &count); err != nil {
			rows.Close()
			return err
		}
		key := d.Format("2006-01-02")
		item, ok := daily[key]
		if !ok {
			item = &RevenueDailyBreakdown{Date: key}
			daily[key] = item
		}
		item.RevenueRecovered += float64(count) * prices[strings.ToLower(strings.TrimSpace(service))]
	}
	rows.Close()

	pWhere, pArgs := orgRangeClause("org_id", "created_at", resp.OrgID, startAt)
	payQ := `
		SELECT DATE(created_at) AS d, COALESCE(SUM(amount_cents),0)
		FROM payments` + pWhere + ` AND status IN ('succeeded', 'paid', 'completed')
		GROUP BY 1`
	rows, err = h.db.QueryContext(r.Context(), payQ, pArgs...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var d time.Time
		var cents int64
		if err := rows.Scan(&d, &cents); err != nil {
			rows.Close()
			return err
		}
		key := d.Format("2006-01-02")
		item, ok := daily[key]
		if !ok {
			item = &RevenueDailyBreakdown{Date: key}
			daily[key] = item
		}
		item.DepositsCollected = round2(float64(cents) / 100.0)
	}
	rows.Close()

	keys := make([]string, 0, len(daily))
	for k := range daily {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	resp.DailyBreakdown = make([]RevenueDailyBreakdown, 0, len(keys))
	for _, k := range keys {
		item := daily[k]
		item.RevenueRecovered = round2(item.RevenueRecovered)
		resp.DailyBreakdown = append(resp.DailyBreakdown, *item)
	}
	return nil
}

func (h *RevenueDashboardHandler) getServicePrices(ctx context.Context, orgID string) map[string]float64 {
	prices := map[string]float64{}
	if h.clinic == nil {
		return prices
	}
	cfg, err := h.clinic.Get(ctx, orgID)
	if err != nil || cfg == nil {
		return prices
	}
	for key, text := range cfg.ServicePriceText {
		if p := parsePriceUSD(text); p > 0 {
			prices[strings.ToLower(strings.TrimSpace(key))] = p
		}
	}
	return prices
}

func periodStart(period string) (*time.Time, bool) {
	now := time.Now().UTC()
	switch period {
	case "week":
		t := now.AddDate(0, 0, -7)
		return &t, true
	case "month":
		t := now.AddDate(0, -1, 0)
		return &t, true
	case "all":
		return nil, true
	default:
		return nil, false
	}
}

func orgRangeClause(orgField, timeField, orgID string, startAt *time.Time) (string, []interface{}) {
	if startAt == nil {
		return " WHERE " + orgField + " = $1", []interface{}{orgID}
	}
	return " WHERE " + orgField + " = $1 AND " + timeField + " >= $2", []interface{}{orgID, *startAt}
}

func buildFunnel(missed, conversations, qualified, booked int) RevenueFunnel {
	base := float64(max(missed, 1))
	return RevenueFunnel{
		MissedCalls:   RevenueFunnelStage{Count: missed, Percentage: 100},
		Conversations: RevenueFunnelStage{Count: conversations, Percentage: round2(float64(conversations) / base * 100)},
		Qualified:     RevenueFunnelStage{Count: qualified, Percentage: round2(float64(qualified) / base * 100)},
		Booked:        RevenueFunnelStage{Count: booked, Percentage: round2(float64(booked) / base * 100)},
	}
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var dollarPattern = regexp.MustCompile(`\$?([0-9]+(?:\.[0-9]{1,2})?)`)

func parsePriceUSD(s string) float64 {
	m := dollarPattern.FindStringSubmatch(s)
	if len(m) < 2 {
		return 0
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	return f
}
