package clinic

import (
	"encoding/json"
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
	Name               string         `json:"name,omitempty"`
	Timezone           string         `json:"timezone,omitempty"`
	BusinessHours      *BusinessHours `json:"business_hours,omitempty"`
	CallbackSLAHours   *int           `json:"callback_sla_hours,omitempty"`
	DepositAmountCents *int           `json:"deposit_amount_cents,omitempty"`
	Services           []string       `json:"services,omitempty"`
}

// UpdateConfig creates or updates the clinic configuration for an org.
// PUT /admin/clinics/{orgID}/config
func (h *Handler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		http.Error(w, `{"error": "org_id required"}`, http.StatusBadRequest)
		return
	}

	var req UpdateConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
	if req.Timezone != "" {
		cfg.Timezone = req.Timezone
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
