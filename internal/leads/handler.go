package leads

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/wolfman30/medspa-ai-platform/internal/tenancy"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// Handler handles HTTP requests for leads
type Handler struct {
	repo   Repository
	logger *logging.Logger
}

// NewHandler creates a new leads handler
func NewHandler(repo Repository, logger *logging.Logger) *Handler {
	return &Handler{
		repo:   repo,
		logger: logger,
	}
}

// CreateWebLead handles POST /leads/web requests
func (h *Handler) CreateWebLead(w http.ResponseWriter, r *http.Request) {
	var req CreateLeadRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error("failed to decode request", "error", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	orgID, ok := tenancy.OrgIDFromContext(r.Context())
	if !ok {
		http.Error(w, "missing org context", http.StatusBadRequest)
		return
	}
	req.OrgID = orgID

	lead, err := h.repo.Create(r.Context(), &req)
	if err != nil {
		h.logger.Error("failed to create lead", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	h.logger.Info("lead created", "id", lead.ID, "name", lead.Name)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(lead)
}

// ListLeadsResponse is the response for listing leads
type ListLeadsResponse struct {
	Leads  []*Lead `json:"leads"`
	Count  int     `json:"count"`
	Offset int     `json:"offset"`
	Limit  int     `json:"limit"`
}

// ListLeads handles GET /admin/clinics/{orgID}/leads requests
func (h *Handler) ListLeads(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		http.Error(w, "missing org_id", http.StatusBadRequest)
		return
	}

	// Parse query params
	filter := ListLeadsFilter{
		Limit:  50,
		Offset: 0,
	}

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 && limit <= 100 {
			filter.Limit = limit
		}
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil && offset >= 0 {
			filter.Offset = offset
		}
	}

	if status := r.URL.Query().Get("deposit_status"); status != "" {
		filter.DepositStatus = status
	}

	leads, err := h.repo.ListByOrg(r.Context(), orgID, filter)
	if err != nil {
		h.logger.Error("failed to list leads", "error", err, "org_id", orgID)
		http.Error(w, "failed to list leads", http.StatusInternalServerError)
		return
	}

	response := ListLeadsResponse{
		Leads:  leads,
		Count:  len(leads),
		Offset: filter.Offset,
		Limit:  filter.Limit,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
