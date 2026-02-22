package rebooking

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// Handler provides HTTP endpoints for the rebooking admin dashboard.
type Handler struct {
	store  *Store
	logger *logging.Logger
}

// NewHandler creates a rebooking HTTP handler.
func NewHandler(store *Store, logger *logging.Logger) *Handler {
	if logger == nil {
		logger = logging.Default()
	}
	return &Handler{store: store, logger: logger}
}

// RegisterRoutes mounts rebooking endpoints under a chi router.
// Expected to be mounted under /api/v1/orgs/{orgID}/rebooking
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/reminders", h.listReminders)
	r.Get("/stats", h.getStats)
}

func (h *Handler) listReminders(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		http.Error(w, "missing org_id", http.StatusBadRequest)
		return
	}

	var statusFilter *ReminderStatus
	if s := r.URL.Query().Get("status"); s != "" {
		st := ReminderStatus(s)
		statusFilter = &st
	}

	reminders, err := h.store.ListByOrg(r.Context(), orgID, statusFilter, 100)
	if err != nil {
		h.logger.Error("rebooking handler: list reminders", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"reminders": reminders,
		"count":     len(reminders),
	})
}

func (h *Handler) getStats(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		http.Error(w, "missing org_id", http.StatusBadRequest)
		return
	}

	stats, err := h.store.Stats(r.Context(), orgID)
	if err != nil {
		h.logger.Error("rebooking handler: stats", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}
