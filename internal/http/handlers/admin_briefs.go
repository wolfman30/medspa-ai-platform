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
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// AdminBriefsHandler serves morning brief markdown files from a research directory.
type AdminBriefsHandler struct {
	briefsDir string
	logger    *logging.Logger
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

// Brief represents a single morning brief.
type Brief struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Date    string `json:"date"`
	Content string `json:"content"`
}

var briefFilePattern = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\.md$`)

// ListBriefs handles GET /admin/briefs
func (h *AdminBriefsHandler) ListBriefs(w http.ResponseWriter, r *http.Request) {
	salesBriefsDir := filepath.Join(h.briefsDir, "sales-briefs")

	entries, err := os.ReadDir(salesBriefsDir)
	if err != nil {
		h.logger.Error("failed to read briefs directory", "error", err, "dir", salesBriefsDir)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"briefs": []Brief{}})
		return
	}

	var briefs []Brief
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
		briefs = append(briefs, Brief{
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
		briefs = append(briefs, Brief{
			ID:      date,
			Title:   title,
			Date:    date,
			Content: string(content),
		})
	}

	// Sort most recent first
	sort.Slice(briefs, func(i, j int) bool {
		return briefs[i].Date > briefs[j].Date
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"briefs": briefs})
}

// GetBrief handles GET /admin/briefs/{date}
func (h *AdminBriefsHandler) GetBrief(w http.ResponseWriter, r *http.Request) {
	date := chi.URLParam(r, "date")
	if date == "" {
		http.Error(w, "missing date parameter", http.StatusBadRequest)
		return
	}

	// Try sales-briefs/DATE.md first, then morning-brief-DATE.md
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
