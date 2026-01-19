package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// AdminDepositsHandler handles admin API endpoints for deposit/payment viewing.
type AdminDepositsHandler struct {
	db     *sql.DB
	logger *logging.Logger
}

// NewAdminDepositsHandler creates a new admin deposits handler.
func NewAdminDepositsHandler(db *sql.DB, logger *logging.Logger) *AdminDepositsHandler {
	if logger == nil {
		logger = logging.Default()
	}
	return &AdminDepositsHandler{
		db:     db,
		logger: logger,
	}
}

// DepositListItem represents a deposit in list responses.
type DepositListItem struct {
	ID              string  `json:"id"`
	OrgID           string  `json:"org_id"`
	LeadID          *string `json:"lead_id,omitempty"`
	LeadPhone       string  `json:"lead_phone"`
	LeadName        *string `json:"lead_name,omitempty"`
	LeadEmail       *string `json:"lead_email,omitempty"`
	ServiceInterest *string `json:"service_interest,omitempty"`
	PatientType     *string `json:"patient_type,omitempty"`
	AmountCents     int     `json:"amount_cents"`
	Status          string  `json:"status"`
	Provider        string  `json:"provider"`
	ProviderRef     *string `json:"provider_ref,omitempty"`
	ScheduledFor    *string `json:"scheduled_for,omitempty"`
	CreatedAt       string  `json:"created_at"`
}

// DepositsListResponse represents a paginated list of deposits.
type DepositsListResponse struct {
	Deposits   []DepositListItem `json:"deposits"`
	Total      int               `json:"total"`
	Page       int               `json:"page"`
	PageSize   int               `json:"page_size"`
	TotalPages int               `json:"total_pages"`
}

// DepositDetailResponse represents detailed deposit information.
type DepositDetailResponse struct {
	ID               string  `json:"id"`
	OrgID            string  `json:"org_id"`
	LeadID           *string `json:"lead_id,omitempty"`
	LeadPhone        string  `json:"lead_phone"`
	LeadName         *string `json:"lead_name,omitempty"`
	LeadEmail        *string `json:"lead_email,omitempty"`
	ServiceInterest  *string `json:"service_interest,omitempty"`
	PatientType      *string `json:"patient_type,omitempty"`
	PreferredDays    *string `json:"preferred_days,omitempty"`
	PreferredTimes   *string `json:"preferred_times,omitempty"`
	SchedulingNotes  *string `json:"scheduling_notes,omitempty"`
	AmountCents      int     `json:"amount_cents"`
	Status           string  `json:"status"`
	Provider         string  `json:"provider"`
	ProviderRef      *string `json:"provider_ref,omitempty"`
	BookingIntentID  *string `json:"booking_intent_id,omitempty"`
	ScheduledFor     *string `json:"scheduled_for,omitempty"`
	CreatedAt        string  `json:"created_at"`
	ConversationID   *string `json:"conversation_id,omitempty"`
}

// DepositStatsResponse contains aggregated deposit statistics.
type DepositStatsResponse struct {
	TotalDeposits      int            `json:"total_deposits"`
	TotalAmountCents   int64          `json:"total_amount_cents"`
	ByStatus           map[string]int `json:"by_status"`
	TodayCount         int            `json:"today_count"`
	TodayAmountCents   int64          `json:"today_amount_cents"`
	WeekCount          int            `json:"week_count"`
	WeekAmountCents    int64          `json:"week_amount_cents"`
	MonthCount         int            `json:"month_count"`
	MonthAmountCents   int64          `json:"month_amount_cents"`
	AverageAmountCents int            `json:"average_amount_cents"`
}

// ListDeposits returns a paginated list of deposits for an organization.
// GET /admin/orgs/{orgID}/deposits
func (h *AdminDepositsHandler) ListDeposits(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		jsonError(w, "missing orgID", http.StatusBadRequest)
		return
	}

	// Parse query parameters
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	status := r.URL.Query().Get("status")
	dateFrom := r.URL.Query().Get("date_from")
	dateTo := r.URL.Query().Get("date_to")

	offset := (page - 1) * pageSize

	query := `
		SELECT p.id, p.org_id, p.lead_id, p.amount_cents, p.status, p.provider,
		       p.provider_ref, p.scheduled_for, p.created_at,
		       l.phone, l.name, l.email, l.service_interest, l.patient_type
		FROM payments p
		LEFT JOIN leads l ON p.lead_id = l.id
		WHERE p.org_id = $1
	`
	args := []any{orgID}
	argNum := 2

	if status != "" {
		query += " AND p.status = $" + strconv.Itoa(argNum)
		args = append(args, status)
		argNum++
	}
	if dateFrom != "" {
		if t, err := time.Parse("2006-01-02", dateFrom); err == nil {
			query += " AND p.created_at >= $" + strconv.Itoa(argNum)
			args = append(args, t)
			argNum++
		}
	}
	if dateTo != "" {
		if t, err := time.Parse("2006-01-02", dateTo); err == nil {
			query += " AND p.created_at < $" + strconv.Itoa(argNum)
			args = append(args, t.AddDate(0, 0, 1))
			argNum++
		}
	}

	query += " ORDER BY p.created_at DESC"

	// Get total count
	countQuery := "SELECT COUNT(*) FROM payments WHERE org_id = $1"
	countArgs := []any{orgID}
	if status != "" {
		countQuery += " AND status = $2"
		countArgs = append(countArgs, status)
	}
	var total int
	h.db.QueryRowContext(r.Context(), countQuery, countArgs...).Scan(&total)

	// Add pagination
	query += " LIMIT $" + strconv.Itoa(argNum) + " OFFSET $" + strconv.Itoa(argNum+1)
	args = append(args, pageSize, offset)

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		h.logger.Error("failed to query deposits", "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var deposits []DepositListItem
	for rows.Next() {
		var d DepositListItem
		var leadID, providerRef, scheduledFor sql.NullString
		var phone, name, email, serviceInterest, patientType sql.NullString
		var createdAt time.Time
		var scheduledTime sql.NullTime

		err := rows.Scan(
			&d.ID, &d.OrgID, &leadID, &d.AmountCents, &d.Status, &d.Provider,
			&providerRef, &scheduledTime, &createdAt,
			&phone, &name, &email, &serviceInterest, &patientType,
		)
		if err != nil {
			h.logger.Error("failed to scan deposit", "error", err)
			continue
		}

		d.CreatedAt = createdAt.Format(time.RFC3339)
		if leadID.Valid {
			d.LeadID = &leadID.String
		}
		if providerRef.Valid {
			d.ProviderRef = &providerRef.String
		}
		if scheduledTime.Valid {
			formatted := scheduledTime.Time.Format(time.RFC3339)
			d.ScheduledFor = &formatted
			scheduledFor.String = formatted
		}
		if phone.Valid {
			d.LeadPhone = phone.String
		}
		if name.Valid {
			d.LeadName = &name.String
		}
		if email.Valid {
			d.LeadEmail = &email.String
		}
		if serviceInterest.Valid {
			d.ServiceInterest = &serviceInterest.String
		}
		if patientType.Valid {
			d.PatientType = &patientType.String
		}

		deposits = append(deposits, d)
	}

	if deposits == nil {
		deposits = []DepositListItem{}
	}

	totalPages := (total + pageSize - 1) / pageSize
	response := DepositsListResponse{
		Deposits:   deposits,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetDeposit returns detailed information about a specific deposit.
// GET /admin/orgs/{orgID}/deposits/{depositID}
func (h *AdminDepositsHandler) GetDeposit(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	depositID := chi.URLParam(r, "depositID")

	if orgID == "" || depositID == "" {
		jsonError(w, "missing orgID or depositID", http.StatusBadRequest)
		return
	}

	query := `
		SELECT p.id, p.org_id, p.lead_id, p.amount_cents, p.status, p.provider,
		       p.provider_ref, p.booking_intent_id, p.scheduled_for, p.created_at,
		       l.phone, l.name, l.email, l.service_interest, l.patient_type,
		       l.preferred_days, l.preferred_times, l.scheduling_notes
		FROM payments p
		LEFT JOIN leads l ON p.lead_id = l.id
		WHERE p.id = $1 AND p.org_id = $2
	`

	var d DepositDetailResponse
	var leadID, providerRef, bookingIntentID sql.NullString
	var phone, name, email, serviceInterest, patientType sql.NullString
	var preferredDays, preferredTimes, schedulingNotes sql.NullString
	var createdAt time.Time
	var scheduledTime sql.NullTime

	err := h.db.QueryRowContext(r.Context(), query, depositID, orgID).Scan(
		&d.ID, &d.OrgID, &leadID, &d.AmountCents, &d.Status, &d.Provider,
		&providerRef, &bookingIntentID, &scheduledTime, &createdAt,
		&phone, &name, &email, &serviceInterest, &patientType,
		&preferredDays, &preferredTimes, &schedulingNotes,
	)

	if err == sql.ErrNoRows {
		jsonError(w, "deposit not found", http.StatusNotFound)
		return
	}
	if err != nil {
		h.logger.Error("failed to get deposit", "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	d.CreatedAt = createdAt.Format(time.RFC3339)
	if leadID.Valid {
		d.LeadID = &leadID.String
	}
	if providerRef.Valid {
		d.ProviderRef = &providerRef.String
	}
	if bookingIntentID.Valid {
		d.BookingIntentID = &bookingIntentID.String
	}
	if scheduledTime.Valid {
		formatted := scheduledTime.Time.Format(time.RFC3339)
		d.ScheduledFor = &formatted
	}
	if phone.Valid {
		d.LeadPhone = phone.String
	}
	if name.Valid {
		d.LeadName = &name.String
	}
	if email.Valid {
		d.LeadEmail = &email.String
	}
	if serviceInterest.Valid {
		d.ServiceInterest = &serviceInterest.String
	}
	if patientType.Valid {
		d.PatientType = &patientType.String
	}
	if preferredDays.Valid {
		d.PreferredDays = &preferredDays.String
	}
	if preferredTimes.Valid {
		d.PreferredTimes = &preferredTimes.String
	}
	if schedulingNotes.Valid {
		d.SchedulingNotes = &schedulingNotes.String
	}

	// Try to find associated conversation
	if d.LeadPhone != "" {
		conversationID := "sms:" + orgID + ":" + d.LeadPhone
		d.ConversationID = &conversationID
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(d)
}

// GetDepositStats returns aggregated deposit statistics.
// GET /admin/orgs/{orgID}/deposits/stats
func (h *AdminDepositsHandler) GetDepositStats(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		jsonError(w, "missing orgID", http.StatusBadRequest)
		return
	}

	stats := DepositStatsResponse{
		ByStatus: make(map[string]int),
	}

	now := time.Now()
	today := now.Truncate(24 * time.Hour)
	weekAgo := now.AddDate(0, 0, -7)
	monthAgo := now.AddDate(0, -1, 0)

	// Total deposits and amount
	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*), COALESCE(SUM(amount_cents), 0) FROM payments WHERE org_id = $1`, orgID,
	).Scan(&stats.TotalDeposits, &stats.TotalAmountCents)

	// Average amount
	if stats.TotalDeposits > 0 {
		stats.AverageAmountCents = int(stats.TotalAmountCents / int64(stats.TotalDeposits))
	}

	// By status
	rows, _ := h.db.QueryContext(r.Context(),
		`SELECT status, COUNT(*) FROM payments WHERE org_id = $1 GROUP BY status`, orgID,
	)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var status string
			var count int
			if rows.Scan(&status, &count) == nil {
				stats.ByStatus[status] = count
			}
		}
	}

	// Today
	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*), COALESCE(SUM(amount_cents), 0) FROM payments WHERE org_id = $1 AND created_at >= $2`,
		orgID, today,
	).Scan(&stats.TodayCount, &stats.TodayAmountCents)

	// This week
	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*), COALESCE(SUM(amount_cents), 0) FROM payments WHERE org_id = $1 AND created_at >= $2`,
		orgID, weekAgo,
	).Scan(&stats.WeekCount, &stats.WeekAmountCents)

	// This month
	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*), COALESCE(SUM(amount_cents), 0) FROM payments WHERE org_id = $1 AND created_at >= $2`,
		orgID, monthAgo,
	).Scan(&stats.MonthCount, &stats.MonthAmountCents)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}
