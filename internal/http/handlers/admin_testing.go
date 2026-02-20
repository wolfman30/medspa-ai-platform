package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// AdminTestingHandler handles manual test result CRUD.
type AdminTestingHandler struct {
	db     *sql.DB
	logger *logging.Logger
}

// NewAdminTestingHandler creates a new testing handler.
func NewAdminTestingHandler(db *sql.DB, logger *logging.Logger) *AdminTestingHandler {
	if logger == nil {
		logger = logging.Default()
	}
	return &AdminTestingHandler{db: db, logger: logger}
}

// TestResult represents a single manual test result.
type TestResult struct {
	ID             int        `json:"id"`
	ScenarioID     string     `json:"scenario_id"`
	ScenarioName   string     `json:"scenario_name"`
	Clinic         string     `json:"clinic"`
	Category       string     `json:"category"`
	Status         string     `json:"status"`
	TestedAt       *time.Time `json:"tested_at"`
	TestedBy       string     `json:"tested_by"`
	Notes          string     `json:"notes"`
	ConversationID string     `json:"conversation_id"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// TestResultsResponse is the list response.
type TestResultsResponse struct {
	Results []TestResult `json:"results"`
	Summary TestSummary  `json:"summary"`
}

// TestSummary gives aggregate counts.
type TestSummary struct {
	Total    int `json:"total"`
	Passed   int `json:"passed"`
	Failed   int `json:"failed"`
	Untested int `json:"untested"`
	Skipped  int `json:"skipped"`

	MustPassTotal  int `json:"must_pass_total"`
	MustPassPassed int `json:"must_pass_passed"`

	SmokeTotal  int `json:"smoke_total"`
	SmokePassed int `json:"smoke_passed"`

	AutoTotal  int `json:"auto_total"`
	AutoPassed int `json:"auto_passed"`

	ReadyForOutreach bool `json:"ready_for_outreach"`
}

// ListTestResults returns all test results.
// GET /admin/testing
func (h *AdminTestingHandler) ListTestResults(w http.ResponseWriter, r *http.Request) {
	clinic := r.URL.Query().Get("clinic")

	query := `SELECT id, scenario_id, scenario_name, clinic, category, status, tested_at, tested_by, notes, conversation_id, created_at, updated_at
		FROM manual_test_results`
	var args []any
	if clinic != "" {
		query += ` WHERE clinic = $1`
		args = append(args, clinic)
	}
	query += ` ORDER BY category, id`

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		h.logger.Error("failed to query test results", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var results []TestResult
	for rows.Next() {
		var tr TestResult
		var testedAt sql.NullTime
		if err := rows.Scan(&tr.ID, &tr.ScenarioID, &tr.ScenarioName, &tr.Clinic, &tr.Category,
			&tr.Status, &testedAt, &tr.TestedBy, &tr.Notes, &tr.ConversationID,
			&tr.CreatedAt, &tr.UpdatedAt); err != nil {
			h.logger.Error("failed to scan test result", "error", err)
			continue
		}
		if testedAt.Valid {
			tr.TestedAt = &testedAt.Time
		}
		results = append(results, tr)
	}
	if results == nil {
		results = []TestResult{}
	}

	summary := computeSummary(results)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TestResultsResponse{Results: results, Summary: summary})
}

func computeSummary(results []TestResult) TestSummary {
	s := TestSummary{Total: len(results)}
	for _, r := range results {
		switch r.Status {
		case "passed":
			s.Passed++
		case "failed":
			s.Failed++
		case "skipped":
			s.Skipped++
		default:
			s.Untested++
		}
		switch r.Category {
		case "must-pass":
			s.MustPassTotal++
			if r.Status == "passed" {
				s.MustPassPassed++
			}
		case "smoke-test":
			s.SmokeTotal++
			if r.Status == "passed" {
				s.SmokePassed++
			}
		case "automated":
			s.AutoTotal++
			if r.Status == "passed" {
				s.AutoPassed++
			}
		}
	}
	// Ready = all must-pass passed + at least 3 smoke tests passed + no failures
	s.ReadyForOutreach = s.MustPassPassed == s.MustPassTotal && s.MustPassTotal > 0 &&
		s.SmokePassed >= 3 && s.Failed == 0
	return s
}

// UpdateTestResult updates a test result's status.
// PUT /admin/testing/{id}
func (h *AdminTestingHandler) UpdateTestResult(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var body struct {
		Status         string `json:"status"`
		TestedBy       string `json:"tested_by"`
		Notes          string `json:"notes"`
		ConversationID string `json:"conversation_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	validStatuses := map[string]bool{"passed": true, "failed": true, "untested": true, "skipped": true}
	if !validStatuses[body.Status] {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}

	var testedAt *time.Time
	if body.Status == "passed" || body.Status == "failed" {
		now := time.Now()
		testedAt = &now
	}

	_, err := h.db.ExecContext(r.Context(),
		`UPDATE manual_test_results SET status=$1, tested_at=$2, tested_by=$3, notes=$4, conversation_id=$5, updated_at=NOW()
		 WHERE id=$6`,
		body.Status, testedAt, body.TestedBy, body.Notes, body.ConversationID, id)
	if err != nil {
		h.logger.Error("failed to update test result", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// CreateTestResult adds a new test scenario.
// POST /admin/testing
func (h *AdminTestingHandler) CreateTestResult(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ScenarioID   string `json:"scenario_id"`
		ScenarioName string `json:"scenario_name"`
		Clinic       string `json:"clinic"`
		Category     string `json:"category"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if body.ScenarioID == "" || body.ScenarioName == "" {
		http.Error(w, "scenario_id and scenario_name required", http.StatusBadRequest)
		return
	}
	if body.Clinic == "" {
		body.Clinic = "Forever 22 Med Spa"
	}
	if body.Category == "" {
		body.Category = "must-pass"
	}

	var id int
	err := h.db.QueryRowContext(r.Context(),
		`INSERT INTO manual_test_results (scenario_id, scenario_name, clinic, category) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (scenario_id, clinic) DO UPDATE SET scenario_name=EXCLUDED.scenario_name, updated_at=NOW()
		 RETURNING id`,
		body.ScenarioID, body.ScenarioName, body.Clinic, body.Category).Scan(&id)
	if err != nil {
		h.logger.Error("failed to create test result", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]int{"id": id})
}
