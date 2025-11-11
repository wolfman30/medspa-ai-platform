package leads

import (
	"encoding/json"
	"net/http"

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
