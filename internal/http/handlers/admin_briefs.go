package handlers

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/wolfman30/medspa-ai-platform/internal/briefs"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// AdminBriefsHandler serves morning brief data from Postgres (primary) or filesystem (fallback).
type AdminBriefsHandler struct {
	briefsDir string
	logger    *logging.Logger
	repo      *briefs.PostgresBriefsRepository
}

// NewAdminBriefsHandler creates a new briefs handler.
// briefsDir should point to the directory containing morning-brief-YYYY-MM-DD.md
// or sales-briefs/YYYY-MM-DD.md files.
func NewAdminBriefsHandler(researchDir string, logger *logging.Logger) *AdminBriefsHandler {
	if logger == nil {
		logger = logging.Default()
	}
	return &AdminBriefsHandler{
		briefsDir: researchDir,
		logger:    logger,
	}
}

// SetRepository sets the Postgres repository for database-backed briefs.
func (h *AdminBriefsHandler) SetRepository(repo *briefs.PostgresBriefsRepository) {
	h.repo = repo
}

// Brief represents a single morning brief.
type Brief struct {
	ID      string  `json:"id"`
	Title   string  `json:"title"`
	Date    string  `json:"date"`
	Content string  `json:"content"`
	Summary *string `json:"summary,omitempty"`
}

var briefFilePattern = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\.md$`)

// ListBriefs handles GET /admin/briefs
func (h *AdminBriefsHandler) ListBriefs(w http.ResponseWriter, r *http.Request) {
	// Try Postgres first
	if h.repo != nil {
		dbBriefs, err := h.repo.List(r.Context())
		if err != nil {
			h.logger.Error("failed to list briefs from DB", "error", err)
		} else {
			result := make([]Brief, len(dbBriefs))
			for i, b := range dbBriefs {
				result[i] = Brief{ID: b.Date, Title: b.Title, Date: b.Date, Content: b.Content, Summary: b.Summary}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"briefs": result})
			return
		}
	}

	// Filesystem fallback
	h.listBriefsFromFilesystem(w)
}

func (h *AdminBriefsHandler) listBriefsFromFilesystem(w http.ResponseWriter) {
	salesBriefsDir := filepath.Join(h.briefsDir, "sales-briefs")

	entries, err := os.ReadDir(salesBriefsDir)
	if err != nil {
		h.logger.Error("failed to read briefs directory", "error", err, "dir", salesBriefsDir)
	}

	var fsBriefs []Brief
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := briefFilePattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			continue
		}
		date := matches[1]
		title := extractTitle(filepath.Join(salesBriefsDir, entry.Name()))
		content, _ := os.ReadFile(filepath.Join(salesBriefsDir, entry.Name()))
		fsBriefs = append(fsBriefs, Brief{
			ID:      date,
			Title:   title,
			Date:    date,
			Content: string(content),
		})
	}

	// Also check for morning-brief-YYYY-MM-DD.md in the research root
	rootEntries, _ := os.ReadDir(h.briefsDir)
	morningPattern := regexp.MustCompile(`^morning-brief-(\d{4}-\d{2}-\d{2})\.md$`)
	for _, entry := range rootEntries {
		if entry.IsDir() {
			continue
		}
		matches := morningPattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			continue
		}
		date := matches[1]
		title := extractTitle(filepath.Join(h.briefsDir, entry.Name()))
		content, _ := os.ReadFile(filepath.Join(h.briefsDir, entry.Name()))
		fsBriefs = append(fsBriefs, Brief{
			ID:      date,
			Title:   title,
			Date:    date,
			Content: string(content),
		})
	}

	sort.Slice(fsBriefs, func(i, j int) bool {
		return fsBriefs[i].Date > fsBriefs[j].Date
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"briefs": fsBriefs})
}

// GetBrief handles GET /admin/briefs/{date}
func (h *AdminBriefsHandler) GetBrief(w http.ResponseWriter, r *http.Request) {
	date := chi.URLParam(r, "date")
	if date == "" {
		http.Error(w, "missing date parameter", http.StatusBadRequest)
		return
	}

	// Try Postgres first
	if h.repo != nil {
		b, err := h.repo.GetByDate(r.Context(), date)
		if err != nil {
			h.logger.Error("failed to get brief from DB", "error", err)
		} else if b != nil {
			brief := Brief{ID: b.Date, Title: b.Title, Date: b.Date, Content: b.Content, Summary: b.Summary}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(brief)
			return
		}
	}

	// Filesystem fallback
	candidates := []string{
		filepath.Join(h.briefsDir, "sales-briefs", date+".md"),
		filepath.Join(h.briefsDir, "morning-brief-"+date+".md"),
	}

	for _, path := range candidates {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		title := extractTitleFromContent(string(content))
		brief := Brief{
			ID:      date,
			Title:   title,
			Date:    date,
			Content: string(content),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(brief)
		return
	}

	http.Error(w, "brief not found", http.StatusNotFound)
}

// CreateBrief handles POST /admin/briefs
func (h *AdminBriefsHandler) CreateBrief(w http.ResponseWriter, r *http.Request) {
	if h.repo == nil {
		http.Error(w, "database not configured", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Date    string  `json:"date"`
		Title   string  `json:"title"`
		Content string  `json:"content"`
		Summary *string `json:"summary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Date == "" || req.Content == "" {
		http.Error(w, "date and content are required", http.StatusBadRequest)
		return
	}
	if req.Title == "" {
		req.Title = extractTitleFromContent(req.Content)
	}

	if err := h.repo.Upsert(r.Context(), req.Date, req.Title, req.Content, req.Summary); err != nil {
		h.logger.Error("failed to upsert brief", "error", err)
		http.Error(w, "failed to save brief", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "date": req.Date})
}

// SeedBriefs handles PUT /admin/briefs/seed — backfills from filesystem to Postgres.
func (h *AdminBriefsHandler) SeedBriefs(w http.ResponseWriter, r *http.Request) {
	if h.repo == nil {
		http.Error(w, "database not configured", http.StatusServiceUnavailable)
		return
	}

	var seeded int
	morningPattern := regexp.MustCompile(`^morning-brief-(\d{4}-\d{2}-\d{2})\.md$`)

	// Seed from research root (morning-brief-*.md)
	rootEntries, _ := os.ReadDir(h.briefsDir)
	for _, entry := range rootEntries {
		if entry.IsDir() {
			continue
		}
		matches := morningPattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			continue
		}
		date := matches[1]
		content, err := os.ReadFile(filepath.Join(h.briefsDir, entry.Name()))
		if err != nil {
			continue
		}
		title := extractTitleFromContent(string(content))
		if err := h.repo.Upsert(r.Context(), date, title, string(content), nil); err != nil {
			h.logger.Error("failed to seed brief", "date", date, "error", err)
			continue
		}
		seeded++
	}

	// Seed from sales-briefs/
	salesDir := filepath.Join(h.briefsDir, "sales-briefs")
	salesEntries, _ := os.ReadDir(salesDir)
	for _, entry := range salesEntries {
		if entry.IsDir() {
			continue
		}
		matches := briefFilePattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			continue
		}
		date := matches[1]
		content, err := os.ReadFile(filepath.Join(salesDir, entry.Name()))
		if err != nil {
			continue
		}
		title := extractTitleFromContent(string(content))
		if err := h.repo.Upsert(r.Context(), date, title, string(content), nil); err != nil {
			h.logger.Error("failed to seed brief", "date", date, "error", err)
			continue
		}
		seeded++
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "seeded": seeded})
}

func extractTitle(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "Morning Brief"
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# ")
		}
	}
	return "Morning Brief"
}

func extractTitleFromContent(content string) string {
	for _, line := range strings.SplitN(content, "\n", 10) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# ")
		}
	}
	return "Morning Brief"
}
