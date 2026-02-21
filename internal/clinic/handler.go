package clinic

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// Handler provides HTTP endpoints for clinic configuration management.
type Handler struct {
	store  *Store
	logger *logging.Logger
}

// NewHandler creates a new clinic config HTTP handler.
func NewHandler(store *Store, logger *logging.Logger) *Handler {
	if logger == nil {
		logger = logging.Default()
	}
	return &Handler{
		store:  store,
		logger: logger,
	}
}

// Routes returns a chi router with clinic admin routes.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/{orgID}/config", h.GetConfig)
	r.Put("/{orgID}/config", h.UpdateConfig)
	r.Post("/{orgID}/config", h.UpdateConfig) // Allow POST as well
	return r
}

// GetConfig returns the clinic configuration for an org.
// GET /admin/clinics/{orgID}/config
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		http.Error(w, `{"error": "org_id required"}`, http.StatusBadRequest)
		return
	}

	cfg, err := h.store.Get(r.Context(), orgID)
	if err != nil {
		h.logger.Error("failed to get clinic config", "org_id", orgID, "error", err)
		http.Error(w, `{"error": "internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(cfg); err != nil {
		h.logger.Error("failed to encode clinic config", "org_id", orgID, "error", err)
	}
}

// UpdateConfigRequest is the request body for updating clinic config.
type UpdateConfigRequest struct {
	Name                      string              `json:"name,omitempty"`
	Email                     string              `json:"email,omitempty"`
	Phone                     string              `json:"phone,omitempty"`
	Address                   string              `json:"address,omitempty"`
	City                      string              `json:"city,omitempty"`
	State                     string              `json:"state,omitempty"`
	ZipCode                   string              `json:"zip_code,omitempty"`
	WebsiteURL                string              `json:"website_url,omitempty"`
	Timezone                  string              `json:"timezone,omitempty"`
	ClinicInfoConfirmed       *bool               `json:"clinic_info_confirmed,omitempty"`
	BusinessHoursConfirmed    *bool               `json:"business_hours_confirmed,omitempty"`
	ServicesConfirmed         *bool               `json:"services_confirmed,omitempty"`
	ContactInfoConfirmed      *bool               `json:"contact_info_confirmed,omitempty"`
	BusinessHours             *BusinessHours      `json:"business_hours,omitempty"`
	CallbackSLAHours          *int                `json:"callback_sla_hours,omitempty"`
	DepositAmountCents        *int                `json:"deposit_amount_cents,omitempty"`
	Services                  []string            `json:"services,omitempty"`
	BookingURL                string              `json:"booking_url,omitempty"`
	BookingPlatform           string              `json:"booking_platform,omitempty"`
	VagaroBusinessAlias       string              `json:"vagaro_business_alias,omitempty"`
	Notifications             *NotificationPrefs  `json:"notifications,omitempty"`
	AIPersona                 *AIPersona          `json:"ai_persona,omitempty"`
	ServiceAliases            map[string]string   `json:"service_aliases,omitempty"`
	MoxieConfig               *MoxieConfig        `json:"moxie_config,omitempty"`
	PaymentProvider           string              `json:"payment_provider,omitempty"`
	StripeAccountID           string              `json:"stripe_account_id,omitempty"`
	BookingPolicies           []string            `json:"booking_policies,omitempty"`
	ServicePriceText          map[string]string   `json:"service_price_text,omitempty"`
	ServiceDepositAmountCents map[string]int      `json:"service_deposit_amount_cents,omitempty"`
	ServiceVariants           map[string][]string `json:"service_variants,omitempty"`
}

// UpdateConfig creates or updates the clinic configuration for an org.
// PUT /admin/clinics/{orgID}/config
func (h *Handler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		http.Error(w, `{"error": "org_id required"}`, http.StatusBadRequest)
		return
	}

	// Debug: read and log the request body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("failed to read request body", "org_id", orgID, "error", err)
		http.Error(w, `{"error": "failed to read body"}`, http.StatusBadRequest)
		return
	}
	h.logger.Info("received config update request", "org_id", orgID, "body", string(bodyBytes))

	var req UpdateConfigRequest
	if err := json.NewDecoder(bytes.NewReader(bodyBytes)).Decode(&req); err != nil {
		h.logger.Error("JSON decode failed", "org_id", orgID, "error", err, "body", string(bodyBytes))
		http.Error(w, `{"error": "invalid JSON body"}`, http.StatusBadRequest)
		return
	}

	// Get existing config (or default)
	cfg, err := h.store.Get(r.Context(), orgID)
	if err != nil {
		h.logger.Error("failed to get clinic config", "org_id", orgID, "error", err)
		http.Error(w, `{"error": "internal server error"}`, http.StatusInternalServerError)
		return
	}

	// Apply updates (partial update support)
	if req.Name != "" {
		cfg.Name = req.Name
	}
	if req.Email != "" {
		cfg.Email = req.Email
	}
	if req.Phone != "" {
		cfg.Phone = req.Phone
	}
	if req.Address != "" {
		cfg.Address = req.Address
	}
	if req.City != "" {
		cfg.City = req.City
	}
	if req.State != "" {
		cfg.State = req.State
	}
	if req.ZipCode != "" {
		cfg.ZipCode = req.ZipCode
	}
	if req.WebsiteURL != "" {
		cfg.WebsiteURL = req.WebsiteURL
	}
	if req.Timezone != "" {
		cfg.Timezone = req.Timezone
	}
	if req.ClinicInfoConfirmed != nil {
		cfg.ClinicInfoConfirmed = *req.ClinicInfoConfirmed
	}
	if req.BusinessHoursConfirmed != nil {
		cfg.BusinessHoursConfirmed = *req.BusinessHoursConfirmed
	}
	if req.ServicesConfirmed != nil {
		cfg.ServicesConfirmed = *req.ServicesConfirmed
	}
	if req.ContactInfoConfirmed != nil {
		cfg.ContactInfoConfirmed = *req.ContactInfoConfirmed
	}
	if req.BusinessHours != nil {
		cfg.BusinessHours = *req.BusinessHours
	}
	if req.CallbackSLAHours != nil {
		cfg.CallbackSLAHours = *req.CallbackSLAHours
	}
	if req.DepositAmountCents != nil {
		cfg.DepositAmountCents = *req.DepositAmountCents
	}
	if req.Services != nil {
		cfg.Services = req.Services
	}
	if req.BookingURL != "" {
		cfg.BookingURL = req.BookingURL
	}
	if req.BookingPlatform != "" {
		cfg.BookingPlatform = req.BookingPlatform
	}
	if req.VagaroBusinessAlias != "" {
		cfg.VagaroBusinessAlias = req.VagaroBusinessAlias
	}
	if req.Notifications != nil {
		cfg.Notifications = *req.Notifications
	}
	if req.AIPersona != nil {
		cfg.AIPersona = *req.AIPersona
	}
	if req.ServiceAliases != nil {
		cfg.ServiceAliases = req.ServiceAliases
	}
	if req.MoxieConfig != nil {
		cfg.MoxieConfig = req.MoxieConfig
	}
	if req.PaymentProvider != "" {
		cfg.PaymentProvider = req.PaymentProvider
	}
	if req.StripeAccountID != "" {
		cfg.StripeAccountID = req.StripeAccountID
	}
	if req.BookingPolicies != nil {
		cfg.BookingPolicies = req.BookingPolicies
	}
	if req.ServicePriceText != nil {
		cfg.ServicePriceText = req.ServicePriceText
	}
	if req.ServiceDepositAmountCents != nil {
		cfg.ServiceDepositAmountCents = req.ServiceDepositAmountCents
	}
	if req.ServiceVariants != nil {
		cfg.ServiceVariants = req.ServiceVariants
	}

	// Save updated config
	if err := h.store.Set(r.Context(), cfg); err != nil {
		h.logger.Error("failed to save clinic config", "org_id", orgID, "error", err)
		http.Error(w, `{"error": "failed to save config"}`, http.StatusInternalServerError)
		return
	}

	h.logger.Info("clinic config updated", "org_id", orgID, "name", cfg.Name)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(cfg); err != nil {
		h.logger.Error("failed to encode clinic config", "org_id", orgID, "error", err)
	}
}
