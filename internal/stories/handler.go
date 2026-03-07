package stories

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
)

const internalServerErrorMessage = "internal server error"

var validStatuses = map[string]bool{
	"backlog":     true,
	"todo":        true,
	"in_progress": true,
	"review":      true,
	"done":        true,
}

var validPriorities = map[string]bool{
	"critical": true,
	"high":     true,
	"medium":   true,
	"low":      true,
}

func validateStatus(s string) error {
	if s != "" && !validStatuses[s] {
		return fmt.Errorf("invalid status %q: must be one of backlog, todo, in_progress, review, done", s)
	}
	return nil
}

func validatePriority(p string) error {
	if p != "" && !validPriorities[p] {
		return fmt.Errorf("invalid priority %q: must be one of critical, high, medium, low", p)
	}
	return nil
}

type Handler struct {
	repo *Repository
}

func NewHandler(repo *Repository) *Handler {
	return &Handler{repo: repo}
}

// GET /admin/stories
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	priority := r.URL.Query().Get("priority")
	label := r.URL.Query().Get("label")

	stories, err := h.repo.List(r.Context(), status, priority, label)
	if err != nil {
		log.Printf("stories.List error: %v", err)
		http.Error(w, internalServerErrorMessage, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"stories": stories})
}

// POST /admin/stories
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateStoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}
	if err := validateStatus(req.Status); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validatePriority(req.Priority); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	story, err := h.repo.Create(r.Context(), req)
	if err != nil {
		log.Printf("stories.Create error: %v", err)
		http.Error(w, internalServerErrorMessage, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(story)
}

// PUT /admin/stories/{id}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "missing story id", http.StatusBadRequest)
		return
	}

	var req UpdateStoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Status != nil {
		if err := validateStatus(*req.Status); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if req.Priority != nil {
		if err := validatePriority(*req.Priority); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	story, err := h.repo.Update(r.Context(), id, req)
	if err != nil {
		log.Printf("stories.Update id=%s error: %v", id, err)
		http.Error(w, internalServerErrorMessage, http.StatusInternalServerError)
		return
	}
	if story == nil {
		http.Error(w, "story not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(story)
}

// DELETE /admin/stories/{id}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "missing story id", http.StatusBadRequest)
		return
	}

	if err := h.repo.Delete(r.Context(), id); err != nil {
		log.Printf("stories.Delete id=%s error: %v", id, err)
		http.Error(w, internalServerErrorMessage, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GET /admin/stories/{id}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, "missing story id", http.StatusBadRequest)
		return
	}

	story, err := h.repo.Get(r.Context(), id)
	if err != nil {
		log.Printf("stories.Get id=%s error: %v", id, err)
		http.Error(w, internalServerErrorMessage, http.StatusInternalServerError)
		return
	}
	if story == nil {
		http.Error(w, "story not found", http.StatusNotFound)
		return
	}

	// Also fetch sub-tasks
	subTasks, err := h.repo.GetSubTasks(r.Context(), id)
	if err != nil {
		log.Printf("stories.GetSubTasks id=%s error: %v", id, err)
		http.Error(w, internalServerErrorMessage, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"story":    story,
		"subTasks": subTasks,
	})
}
