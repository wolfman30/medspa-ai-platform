package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// AdminDashboardHandler handles the main dashboard overview endpoint.
type AdminDashboardHandler struct {
	db     *sql.DB
	logger *logging.Logger
}

// NewAdminDashboardHandler creates a new admin dashboard handler.
func NewAdminDashboardHandler(db *sql.DB, logger *logging.Logger) *AdminDashboardHandler {
	if logger == nil {
		logger = logging.Default()
	}
	return &AdminDashboardHandler{
		db:     db,
		logger: logger,
	}
}

// DashboardOverviewResponse contains the main dashboard metrics.
type DashboardOverviewResponse struct {
	OrgID          string              `json:"org_id"`
	OrgName        string              `json:"org_name"`
	Period         string              `json:"period"`
	Leads          LeadMetrics         `json:"leads"`
	Conversations  ConversationMetrics `json:"conversations"`
	Payments       PaymentMetrics      `json:"payments"`
	Bookings       BookingMetrics      `json:"bookings"`
	Compliance     ComplianceMetrics   `json:"compliance"`
	Onboarding     OnboardingMetrics   `json:"onboarding"`
	PendingActions []PendingAction     `json:"pending_actions"`
}

// LeadMetrics contains lead-related dashboard metrics.
type LeadMetrics struct {
	Total          int     `json:"total"`
	NewThisWeek    int     `json:"new_this_week"`
	ConversionRate float64 `json:"conversion_rate"`
	TopSources     []struct {
		Source string `json:"source"`
		Count  int    `json:"count"`
	} `json:"top_sources,omitempty"`
}

// ConversationMetrics contains conversation-related dashboard metrics.
type ConversationMetrics struct {
	UniqueConversations int `json:"unique_conversations"`
	TotalJobs           int `json:"total_jobs"`
	Today               int `json:"today"`
	ThisWeek            int `json:"this_week"`
}

// PaymentMetrics contains payment-related dashboard metrics.
type PaymentMetrics struct {
	TotalCollected  int `json:"total_collected_cents"`
	ThisWeek        int `json:"this_week_cents"`
	PendingDeposits int `json:"pending_deposits"`
	RefundedAmount  int `json:"refunded_cents"`
	DisputeCount    int `json:"dispute_count"`
}

// BookingMetrics contains booking-related dashboard metrics.
type BookingMetrics struct {
	Total          int `json:"total"`
	Upcoming       int `json:"upcoming"`
	ThisWeek       int `json:"this_week"`
	CancelledCount int `json:"cancelled_count"`
}

// ComplianceMetrics contains compliance-related dashboard metrics.
type ComplianceMetrics struct {
	AuditEventsToday        int `json:"audit_events_today"`
	SupervisorInterventions int `json:"supervisor_interventions"`
	PHIDetections           int `json:"phi_detections"`
	DisclaimersSent         int `json:"disclaimers_sent"`
}

// OnboardingMetrics contains 10DLC onboarding status.
type OnboardingMetrics struct {
	BrandStatus    string `json:"brand_status"`
	CampaignStatus string `json:"campaign_status"`
	NumbersActive  int    `json:"numbers_active"`
	FullyCompliant bool   `json:"fully_compliant"`
}

// PendingAction represents an action requiring staff attention.
type PendingAction struct {
	Type        string `json:"type"`
	Priority    string `json:"priority"`
	Description string `json:"description"`
	Count       int    `json:"count"`
	Link        string `json:"link,omitempty"`
}

// GetDashboardOverview returns the main dashboard overview.
// GET /admin/orgs/{orgID}/dashboard
func (h *AdminDashboardHandler) GetDashboardOverview(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		http.Error(w, "missing orgID", http.StatusBadRequest)
		return
	}

	period := r.URL.Query().Get("period")
	if period == "" {
		period = "week"
	}

	dashboard := DashboardOverviewResponse{
		OrgID:  orgID,
		Period: period,
	}

	// Get org name
	h.db.QueryRowContext(r.Context(),
		`SELECT name FROM organizations WHERE id = $1`, orgID,
	).Scan(&dashboard.OrgName)

	// Calculate date ranges
	now := time.Now()
	weekAgo := now.AddDate(0, 0, -7)
	today := now.Truncate(24 * time.Hour)

	// Lead metrics
	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM leads WHERE org_id = $1`, orgID,
	).Scan(&dashboard.Leads.Total)

	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM leads WHERE org_id = $1 AND created_at >= $2`, orgID, weekAgo,
	).Scan(&dashboard.Leads.NewThisWeek)

	var converted int
	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(DISTINCT l.id) FROM leads l
		 JOIN payments p ON l.id = p.lead_id
		 WHERE l.org_id = $1 AND p.status = 'succeeded'`, orgID,
	).Scan(&converted)
	if dashboard.Leads.Total > 0 {
		dashboard.Leads.ConversionRate = float64(converted) / float64(dashboard.Leads.Total) * 100
	}

	// Conversation metrics - using conversation_jobs table
	conversationIDPattern := "sms:" + orgID + ":%"

	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(DISTINCT conversation_id) FROM conversation_jobs WHERE conversation_id LIKE $1`, conversationIDPattern,
	).Scan(&dashboard.Conversations.UniqueConversations)

	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM conversation_jobs WHERE conversation_id LIKE $1`, conversationIDPattern,
	).Scan(&dashboard.Conversations.TotalJobs)

	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(DISTINCT conversation_id) FROM conversation_jobs WHERE conversation_id LIKE $1 AND created_at >= $2`, conversationIDPattern, today,
	).Scan(&dashboard.Conversations.Today)

	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(DISTINCT conversation_id) FROM conversation_jobs WHERE conversation_id LIKE $1 AND created_at >= $2`, conversationIDPattern, weekAgo,
	).Scan(&dashboard.Conversations.ThisWeek)

	// Payment metrics
	h.db.QueryRowContext(r.Context(),
		`SELECT COALESCE(SUM(amount_cents), 0) FROM payments WHERE org_id = $1 AND status = 'succeeded'`, orgID,
	).Scan(&dashboard.Payments.TotalCollected)

	h.db.QueryRowContext(r.Context(),
		`SELECT COALESCE(SUM(amount_cents), 0) FROM payments WHERE org_id = $1 AND status = 'succeeded' AND created_at >= $2`, orgID, weekAgo,
	).Scan(&dashboard.Payments.ThisWeek)

	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM payments WHERE org_id = $1 AND status = 'pending'`, orgID,
	).Scan(&dashboard.Payments.PendingDeposits)

	h.db.QueryRowContext(r.Context(),
		`SELECT COALESCE(SUM(amount_cents), 0) FROM payments WHERE org_id = $1 AND status = 'refunded'`, orgID,
	).Scan(&dashboard.Payments.RefundedAmount)

	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM payment_disputes WHERE org_id = $1 AND state NOT IN ('WON', 'LOST', 'ACCEPTED')`, orgID,
	).Scan(&dashboard.Payments.DisputeCount)

	// Booking metrics
	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM bookings WHERE org_id = $1`, orgID,
	).Scan(&dashboard.Bookings.Total)

	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM bookings WHERE org_id = $1 AND scheduled_at > $2`, orgID, now,
	).Scan(&dashboard.Bookings.Upcoming)

	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM bookings WHERE org_id = $1 AND scheduled_at >= $2 AND scheduled_at < $3`, orgID, weekAgo, now,
	).Scan(&dashboard.Bookings.ThisWeek)

	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM bookings WHERE org_id = $1 AND status = 'cancelled'`, orgID,
	).Scan(&dashboard.Bookings.CancelledCount)

	// Compliance metrics
	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM compliance_audit_events WHERE org_id = $1 AND created_at >= $2`, orgID, today,
	).Scan(&dashboard.Compliance.AuditEventsToday)

	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM compliance_audit_events WHERE org_id = $1 AND event_type = 'compliance.supervisor_review' AND created_at >= $2`, orgID, weekAgo,
	).Scan(&dashboard.Compliance.SupervisorInterventions)

	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM compliance_audit_events WHERE org_id = $1 AND event_type = 'compliance.phi_detected' AND created_at >= $2`, orgID, weekAgo,
	).Scan(&dashboard.Compliance.PHIDetections)

	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM compliance_audit_events WHERE org_id = $1 AND event_type = 'compliance.disclaimer_sent' AND created_at >= $2`, orgID, weekAgo,
	).Scan(&dashboard.Compliance.DisclaimersSent)

	// Onboarding status
	var brandStatus, campaignStatus sql.NullString
	h.db.QueryRowContext(r.Context(),
		`SELECT status FROM ten_dlc_brands WHERE org_id = $1 ORDER BY created_at DESC LIMIT 1`, orgID,
	).Scan(&brandStatus)
	dashboard.Onboarding.BrandStatus = brandStatus.String
	if dashboard.Onboarding.BrandStatus == "" {
		dashboard.Onboarding.BrandStatus = "NOT_REGISTERED"
	}

	h.db.QueryRowContext(r.Context(),
		`SELECT status FROM ten_dlc_campaigns WHERE org_id = $1 ORDER BY created_at DESC LIMIT 1`, orgID,
	).Scan(&campaignStatus)
	dashboard.Onboarding.CampaignStatus = campaignStatus.String
	if dashboard.Onboarding.CampaignStatus == "" {
		dashboard.Onboarding.CampaignStatus = "NOT_REGISTERED"
	}

	h.db.QueryRowContext(r.Context(),
		`SELECT COALESCE(SUM(numbers_assigned), 0) FROM ten_dlc_campaigns WHERE org_id = $1 AND status = 'ACTIVE'`, orgID,
	).Scan(&dashboard.Onboarding.NumbersActive)

	dashboard.Onboarding.FullyCompliant = dashboard.Onboarding.BrandStatus == "VERIFIED" &&
		dashboard.Onboarding.CampaignStatus == "ACTIVE" &&
		dashboard.Onboarding.NumbersActive > 0

	// Pending actions
	dashboard.PendingActions = h.getPendingActions(r, orgID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(dashboard)
}

func (h *AdminDashboardHandler) getPendingActions(r *http.Request, orgID string) []PendingAction {
	var actions []PendingAction

	// Pending escalations
	var pendingEscalations int
	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM escalations WHERE org_id = $1 AND status = 'PENDING'`, orgID,
	).Scan(&pendingEscalations)
	if pendingEscalations > 0 {
		actions = append(actions, PendingAction{
			Type:        "escalation",
			Priority:    "high",
			Description: "Pending escalations require attention",
			Count:       pendingEscalations,
			Link:        "/admin/escalations",
		})
	}

	// Pending callback promises
	var pendingCallbacks int
	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM callback_promises WHERE org_id = $1 AND status IN ('PENDING', 'REMINDED') AND due_at < NOW()`, orgID,
	).Scan(&pendingCallbacks)
	if pendingCallbacks > 0 {
		actions = append(actions, PendingAction{
			Type:        "callback",
			Priority:    "medium",
			Description: "Overdue callback promises",
			Count:       pendingCallbacks,
			Link:        "/admin/callbacks",
		})
	}

	// Active disputes
	var activeDisputes int
	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM payment_disputes WHERE org_id = $1 AND state IN ('INQUIRY_EVIDENCE_REQUIRED', 'EVIDENCE_REQUIRED')`, orgID,
	).Scan(&activeDisputes)
	if activeDisputes > 0 {
		actions = append(actions, PendingAction{
			Type:        "dispute",
			Priority:    "high",
			Description: "Disputes requiring evidence submission",
			Count:       activeDisputes,
			Link:        "/admin/disputes",
		})
	}

	// Incomplete onboarding
	var brandStatus, campaignStatus sql.NullString
	h.db.QueryRowContext(r.Context(),
		`SELECT status FROM ten_dlc_brands WHERE org_id = $1 ORDER BY created_at DESC LIMIT 1`, orgID,
	).Scan(&brandStatus)
	h.db.QueryRowContext(r.Context(),
		`SELECT status FROM ten_dlc_campaigns WHERE org_id = $1 ORDER BY created_at DESC LIMIT 1`, orgID,
	).Scan(&campaignStatus)

	if !brandStatus.Valid || brandStatus.String != "VERIFIED" {
		actions = append(actions, PendingAction{
			Type:        "onboarding",
			Priority:    "medium",
			Description: "10DLC brand registration incomplete",
			Count:       1,
			Link:        "/admin/onboarding",
		})
	} else if !campaignStatus.Valid || campaignStatus.String != "ACTIVE" {
		actions = append(actions, PendingAction{
			Type:        "onboarding",
			Priority:    "medium",
			Description: "10DLC campaign registration incomplete",
			Count:       1,
			Link:        "/admin/onboarding",
		})
	}

	return actions
}

// OrgListItem represents an organization in the list response.
type OrgListItem struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	OwnerEmail *string `json:"owner_email,omitempty"`
	CreatedAt  string  `json:"created_at"`
}

// ListOrganizationsResponse contains the list of all organizations.
type ListOrganizationsResponse struct {
	Organizations []OrgListItem `json:"organizations"`
	Total         int           `json:"total"`
}

// ListOrganizations returns all organizations for admin users.
// GET /admin/orgs
func (h *AdminDashboardHandler) ListOrganizations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	query := `
		SELECT id, name, owner_email, created_at
		FROM organizations
		ORDER BY name ASC
	`

	rows, err := h.db.QueryContext(ctx, query)
	if err != nil {
		h.logger.Error("failed to query organizations", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var orgs []OrgListItem
	for rows.Next() {
		var org OrgListItem
		var ownerEmail sql.NullString
		var createdAt time.Time

		if err := rows.Scan(&org.ID, &org.Name, &ownerEmail, &createdAt); err != nil {
			h.logger.Error("failed to scan organization row", "error", err)
			continue
		}

		if ownerEmail.Valid {
			org.OwnerEmail = &ownerEmail.String
		}
		org.CreatedAt = createdAt.Format(time.RFC3339)
		orgs = append(orgs, org)
	}

	if err := rows.Err(); err != nil {
		h.logger.Error("error iterating organization rows", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := ListOrganizationsResponse{
		Organizations: orgs,
		Total:         len(orgs),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("failed to encode organizations response", "error", err)
	}
}

// RegisterAdminRoutes registers all admin dashboard routes.
func RegisterAdminRoutes(r chi.Router, db *sql.DB, transcriptStore *conversation.SMSTranscriptStore, clinicStore *clinic.Store, logger *logging.Logger) {
	dashboardHandler := NewAdminDashboardHandler(db, logger)
	leadsHandler := NewAdminLeadsHandler(db, logger)
	conversationsHandler := NewAdminConversationsHandler(db, transcriptStore, logger)
	depositsHandler := NewAdminDepositsHandler(db, logger)
	notificationsHandler := NewAdminNotificationsHandler(clinicStore, logger)

	// List all organizations (admin only)
	r.Get("/orgs", dashboardHandler.ListOrganizations)

	r.Route("/orgs/{orgID}", func(r chi.Router) {
		// Dashboard
		r.Get("/dashboard", dashboardHandler.GetDashboardOverview)

		// Leads
		r.Get("/leads", leadsHandler.ListLeads)
		r.Get("/leads/stats", leadsHandler.GetLeadStats)
		r.Get("/leads/{leadID}", leadsHandler.GetLead)
		r.Patch("/leads/{leadID}", leadsHandler.UpdateLead)

		// Conversations
		r.Get("/conversations", conversationsHandler.ListConversations)
		r.Get("/conversations/stats", conversationsHandler.GetConversationStats)
		r.Get("/conversations/{conversationID}", conversationsHandler.GetConversation)
		r.Get("/conversations/{conversationID}/export", conversationsHandler.ExportTranscript)

		// Deposits
		r.Get("/deposits", depositsHandler.ListDeposits)
		r.Get("/deposits/stats", depositsHandler.GetDepositStats)
		r.Get("/deposits/{depositID}", depositsHandler.GetDeposit)

		// Notifications
		r.Get("/notifications", notificationsHandler.GetNotificationSettings)
		r.Put("/notifications", notificationsHandler.UpdateNotificationSettings)
	})
}
