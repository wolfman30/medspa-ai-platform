package prospects

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type Handler struct {
	repo *Repository
}

func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
}

// GET /admin/prospects
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	prospects, err := h.repo.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// Attach timeline to each prospect
	for i := range prospects {
		events, err := h.repo.ListEvents(r.Context(), prospects[i].ID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		prospects[i].Timeline = events
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"lastUpdated": time.Now().UTC(),
		"prospects":   prospects,
	})
}

// GET /admin/prospects/{id}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := extractProspectID(r)
	if id == "" {
		http.Error(w, "missing prospect id", 400)
		return
	}

	p, err := h.repo.Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if p == nil {
		http.Error(w, "not found", 404)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(p)
}

// PUT /admin/prospects/{id}
func (h *Handler) Upsert(w http.ResponseWriter, r *http.Request) {
	id := extractProspectID(r)
	if id == "" {
		http.Error(w, "missing prospect id", 400)
		return
	}

	var p Prospect
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "invalid json: "+err.Error(), 400)
		return
	}
	p.ID = id
	if p.Providers == nil {
		p.Providers = []string{}
	}

	if err := h.repo.Upsert(r.Context(), &p); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

// DELETE /admin/prospects/{id}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := extractProspectID(r)
	if id == "" {
		http.Error(w, "missing prospect id", 400)
		return
	}

	if err := h.repo.Delete(r.Context(), id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

// POST /admin/prospects/{id}/events
func (h *Handler) AddEvent(w http.ResponseWriter, r *http.Request) {
	id := extractProspectID(r)
	if id == "" {
		http.Error(w, "missing prospect id", 400)
		return
	}

	var e Event
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		http.Error(w, "invalid json: "+err.Error(), 400)
		return
	}
	e.ProspectID = id
	if e.Date.IsZero() {
		e.Date = time.Now().UTC()
	}

	if err := h.repo.AddEvent(r.Context(), &e); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(e)
}

func extractProspectID(r *http.Request) string {
	// Expects path like /admin/prospects/{id} or /admin/prospects/{id}/events
	path := r.URL.Path
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	// ["admin", "prospects", "{id}", ...]
	for i, p := range parts {
		if p == "prospects" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
