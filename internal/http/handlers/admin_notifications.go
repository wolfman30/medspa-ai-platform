package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// AdminNotificationsHandler handles admin API endpoints for notification settings.
type AdminNotificationsHandler struct {
	clinicStore *clinic.Store
	logger      *logging.Logger
}

// NewAdminNotificationsHandler creates a new admin notifications handler.
func NewAdminNotificationsHandler(clinicStore *clinic.Store, logger *logging.Logger) *AdminNotificationsHandler {
	if logger == nil {
		logger = logging.Default()
	}
	return &AdminNotificationsHandler{
		clinicStore: clinicStore,
		logger:      logger,
	}
}

// NotificationSettingsResponse represents notification settings for an org.
type NotificationSettingsResponse struct {
	EmailEnabled    bool     `json:"email_enabled"`
	EmailRecipients []string `json:"email_recipients"`
	SMSEnabled      bool     `json:"sms_enabled"`
	SMSRecipients   []string `json:"sms_recipients"`
	NotifyOnPayment bool     `json:"notify_on_payment"`
	NotifyOnNewLead bool     `json:"notify_on_new_lead"`
}

// UpdateNotificationSettingsRequest represents a request to update notification settings.
type UpdateNotificationSettingsRequest struct {
	EmailEnabled    *bool    `json:"email_enabled,omitempty"`
	EmailRecipients []string `json:"email_recipients,omitempty"`
	SMSEnabled      *bool    `json:"sms_enabled,omitempty"`
	SMSRecipients   []string `json:"sms_recipients,omitempty"`
	NotifyOnPayment *bool    `json:"notify_on_payment,omitempty"`
	NotifyOnNewLead *bool    `json:"notify_on_new_lead,omitempty"`
}

// GetNotificationSettings returns the notification settings for an org.
// GET /admin/orgs/{orgID}/notifications
func (h *AdminNotificationsHandler) GetNotificationSettings(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		jsonError(w, "missing orgID", http.StatusBadRequest)
		return
	}

	cfg, err := h.clinicStore.Get(r.Context(), orgID)
	if err != nil {
		h.logger.Error("failed to get clinic config", "error", err, "org_id", orgID)
		jsonError(w, "failed to get settings", http.StatusInternalServerError)
		return
	}

	// Build response with combined SMS recipients
	response := NotificationSettingsResponse{
		EmailEnabled:    cfg.Notifications.EmailEnabled,
		EmailRecipients: cfg.Notifications.EmailRecipients,
		SMSEnabled:      cfg.Notifications.SMSEnabled,
		SMSRecipients:   cfg.Notifications.GetSMSRecipients(),
		NotifyOnPayment: cfg.Notifications.NotifyOnPayment,
		NotifyOnNewLead: cfg.Notifications.NotifyOnNewLead,
	}

	// Ensure arrays are not nil
	if response.EmailRecipients == nil {
		response.EmailRecipients = []string{}
	}
	if response.SMSRecipients == nil {
		response.SMSRecipients = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// UpdateNotificationSettings updates the notification settings for an org.
// PUT /admin/orgs/{orgID}/notifications
func (h *AdminNotificationsHandler) UpdateNotificationSettings(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		jsonError(w, "missing orgID", http.StatusBadRequest)
		return
	}

	var req UpdateNotificationSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Get existing config
	cfg, err := h.clinicStore.Get(r.Context(), orgID)
	if err != nil {
		h.logger.Error("failed to get clinic config", "error", err, "org_id", orgID)
		jsonError(w, "failed to get settings", http.StatusInternalServerError)
		return
	}

	// Update fields if provided
	if req.EmailEnabled != nil {
		cfg.Notifications.EmailEnabled = *req.EmailEnabled
	}
	if req.EmailRecipients != nil {
		cfg.Notifications.EmailRecipients = req.EmailRecipients
	}
	if req.SMSEnabled != nil {
		cfg.Notifications.SMSEnabled = *req.SMSEnabled
	}
	if req.SMSRecipients != nil {
		// Store in the new SMSRecipients field and clear legacy field
		cfg.Notifications.SMSRecipients = req.SMSRecipients
		cfg.Notifications.SMSRecipient = "" // Clear legacy field
	}
	if req.NotifyOnPayment != nil {
		cfg.Notifications.NotifyOnPayment = *req.NotifyOnPayment
	}
	if req.NotifyOnNewLead != nil {
		cfg.Notifications.NotifyOnNewLead = *req.NotifyOnNewLead
	}

	// Save updated config
	if err := h.clinicStore.Set(r.Context(), cfg); err != nil {
		h.logger.Error("failed to save clinic config", "error", err, "org_id", orgID)
		jsonError(w, "failed to save settings", http.StatusInternalServerError)
		return
	}

	h.logger.Info("notification settings updated", "org_id", orgID,
		"email_enabled", cfg.Notifications.EmailEnabled,
		"sms_enabled", cfg.Notifications.SMSEnabled,
		"sms_recipients_count", len(cfg.Notifications.GetSMSRecipients()),
		"email_recipients_count", len(cfg.Notifications.EmailRecipients))

	// Return updated settings
	response := NotificationSettingsResponse{
		EmailEnabled:    cfg.Notifications.EmailEnabled,
		EmailRecipients: cfg.Notifications.EmailRecipients,
		SMSEnabled:      cfg.Notifications.SMSEnabled,
		SMSRecipients:   cfg.Notifications.GetSMSRecipients(),
		NotifyOnPayment: cfg.Notifications.NotifyOnPayment,
		NotifyOnNewLead: cfg.Notifications.NotifyOnNewLead,
	}

	if response.EmailRecipients == nil {
		response.EmailRecipients = []string{}
	}
	if response.SMSRecipients == nil {
		response.SMSRecipients = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
