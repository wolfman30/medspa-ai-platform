package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// UpdateLead updates a lead's information.
// PATCH /admin/orgs/{orgID}/leads/{leadID}
func (h *AdminLeadsHandler) UpdateLead(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	leadID := chi.URLParam(r, "leadID")

	if orgID == "" || leadID == "" {
		http.Error(w, "missing orgID or leadID", http.StatusBadRequest)
		return
	}

	leadUUID, err := uuid.Parse(leadID)
	if err != nil {
		http.Error(w, "invalid leadID", http.StatusBadRequest)
		return
	}

	var req UpdateLeadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Build update query dynamically
	updates := []string{}
	args := []any{}
	argNum := 1

	if req.Name != nil {
		updates = append(updates, "name = $"+strconv.Itoa(argNum))
		args = append(args, *req.Name)
		argNum++
	}
	if req.Email != nil {
		updates = append(updates, "email = $"+strconv.Itoa(argNum))
		args = append(args, *req.Email)
		argNum++
	}
	if req.Status != nil {
		updates = append(updates, "status = $"+strconv.Itoa(argNum))
		args = append(args, *req.Status)
		argNum++
	}
	if req.Notes != nil {
		updates = append(updates, "notes = $"+strconv.Itoa(argNum))
		args = append(args, *req.Notes)
		argNum++
	}
	if req.Tags != nil {
		tagsJSON, err := json.Marshal(req.Tags)
		if err != nil {
			h.logger.Error("failed to marshal tags", "error", err)
			http.Error(w, "invalid tags", http.StatusBadRequest)
			return
		}
		updates = append(updates, "tags = $"+strconv.Itoa(argNum))
		args = append(args, tagsJSON)
		argNum++
	}
	if req.InterestedServices != nil {
		servicesJSON, err := json.Marshal(req.InterestedServices)
		if err != nil {
			h.logger.Error("failed to marshal interested services", "error", err)
			http.Error(w, "invalid interested_services", http.StatusBadRequest)
			return
		}
		updates = append(updates, "interested_services = $"+strconv.Itoa(argNum))
		args = append(args, servicesJSON)
		argNum++
	}

	if len(updates) == 0 {
		http.Error(w, "no fields to update", http.StatusBadRequest)
		return
	}

	updates = append(updates, "updated_at = $"+strconv.Itoa(argNum))
	args = append(args, time.Now())
	argNum++

	// argNum continues from SET placeholders into WHERE placeholders to keep
	// parameter positions aligned with a single args slice.
	args = append(args, leadUUID, orgID)

	query := "UPDATE leads SET " + strings.Join(updates, ", ") +
		" WHERE id = $" + strconv.Itoa(argNum) + " AND org_id = $" + strconv.Itoa(argNum+1)

	result, err := h.db.ExecContext(r.Context(), query, args...)
	if err != nil {
		h.logger.Error("failed to update lead", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	rows, err := result.RowsAffected()
	if err != nil {
		h.logger.Error("failed to read rows affected", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if rows == 0 {
		http.Error(w, "lead not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
