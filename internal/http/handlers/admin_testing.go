package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// S3Uploader is an interface for S3 PutObject (testable).
type S3Uploader interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

// AdminTestingHandler handles manual test result CRUD.
type AdminTestingHandler struct {
	db       *sql.DB
	logger   *logging.Logger
	s3Client S3Uploader
	s3Bucket string
	s3Region string
}

// NewAdminTestingHandler creates a new testing handler.
func NewAdminTestingHandler(db *sql.DB, logger *logging.Logger, s3Client S3Uploader, s3Bucket, s3Region string) *AdminTestingHandler {
	if logger == nil {
		logger = logging.Default()
	}
	return &AdminTestingHandler{db: db, logger: logger, s3Client: s3Client, s3Bucket: s3Bucket, s3Region: s3Region}
}

// TestResult represents a single manual test result.
type TestResult struct {
	ID             int        `json:"id"`
	ScenarioID     string     `json:"scenario_id"`
	ScenarioName   string     `json:"scenario_name"`
	Description    string     `json:"description"`
	Clinic         string     `json:"clinic"`
	Category       string     `json:"category"`
	Status         string     `json:"status"`
	TestedAt       *time.Time `json:"tested_at"`
	TestedBy       string     `json:"tested_by"`
	Notes          string     `json:"notes"`
	ConversationID string     `json:"conversation_id"`
	EvidenceURLs   []string   `json:"evidence_urls"`
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

	query := `SELECT id, scenario_id, scenario_name, description, clinic, category, status, tested_at, tested_by, notes, conversation_id, evidence_urls, created_at, updated_at
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
		if err := rows.Scan(&tr.ID, &tr.ScenarioID, &tr.ScenarioName, &tr.Description, &tr.Clinic, &tr.Category,
			&tr.Status, &testedAt, &tr.TestedBy, &tr.Notes, &tr.ConversationID,
			pq.Array(&tr.EvidenceURLs), &tr.CreatedAt, &tr.UpdatedAt); err != nil {
			h.logger.Error("failed to scan test result", "error", err)
			continue
		}
		if testedAt.Valid {
			tr.TestedAt = &testedAt.Time
		}
		if tr.EvidenceURLs == nil {
			tr.EvidenceURLs = []string{}
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
	s.ReadyForOutreach = s.MustPassPassed == s.MustPassTotal && s.MustPassTotal > 0 &&
		s.SmokePassed >= 3 && s.Failed == 0
	return s
}

// UpdateTestResult updates a test result's status.
// PUT /admin/testing/{id}
func (h *AdminTestingHandler) UpdateTestResult(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var body struct {
		Status         string   `json:"status"`
		TestedBy       string   `json:"tested_by"`
		Notes          string   `json:"notes"`
		ConversationID string   `json:"conversation_id"`
		EvidenceURLs   []string `json:"evidence_urls"`
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

	if body.EvidenceURLs == nil {
		// Don't overwrite evidence if not provided — only update if explicitly sent
		_, err := h.db.ExecContext(r.Context(),
			`UPDATE manual_test_results SET status=$1, tested_at=$2, tested_by=$3, notes=$4, conversation_id=$5, updated_at=NOW()
			 WHERE id=$6`,
			body.Status, testedAt, body.TestedBy, body.Notes, body.ConversationID, id)
		if err != nil {
			h.logger.Error("failed to update test result", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	} else {
		_, err := h.db.ExecContext(r.Context(),
			`UPDATE manual_test_results SET status=$1, tested_at=$2, tested_by=$3, notes=$4, conversation_id=$5, evidence_urls=$6, updated_at=NOW()
			 WHERE id=$7`,
			body.Status, testedAt, body.TestedBy, body.Notes, body.ConversationID, pq.Array(body.EvidenceURLs), id)
		if err != nil {
			h.logger.Error("failed to update test result", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// UploadEvidence handles screenshot/image uploads for test evidence.
// POST /admin/testing/{id}/evidence
// Content-Type: multipart/form-data, field name: "file"
func (h *AdminTestingHandler) UploadEvidence(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if h.s3Client == nil || h.s3Bucket == "" {
		http.Error(w, "evidence upload not configured (no S3)", http.StatusServiceUnavailable)
		return
	}

	// 10MB max
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "file too large (max 10MB)", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate content type
	ext := strings.ToLower(filepath.Ext(header.Filename))
	contentTypes := map[string]string{
		".png":  "image/png",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".gif":  "image/gif",
		".webp": "image/webp",
	}
	ct, ok := contentTypes[ext]
	if !ok {
		http.Error(w, "unsupported file type (png, jpg, gif, webp only)", http.StatusBadRequest)
		return
	}

	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "failed to read file", http.StatusInternalServerError)
		return
	}

	// Upload to S3
	s3Key := fmt.Sprintf("testing-evidence/%s/%d-%s%s", id, time.Now().UnixMilli(), strings.TrimSuffix(header.Filename, ext), ext)
	_, err = h.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      aws.String(h.s3Bucket),
		Key:         aws.String(s3Key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(ct),
	})
	if err != nil {
		h.logger.Error("failed to upload evidence to S3", "error", err)
		http.Error(w, "upload failed", http.StatusInternalServerError)
		return
	}

	evidenceURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", h.s3Bucket, h.s3Region, s3Key)

	// Append to evidence_urls array
	_, err = h.db.ExecContext(r.Context(),
		`UPDATE manual_test_results SET evidence_urls = array_append(evidence_urls, $1), updated_at=NOW() WHERE id=$2`,
		evidenceURL, id)
	if err != nil {
		h.logger.Error("failed to update evidence_urls", "error", err)
		http.Error(w, "saved to S3 but failed to update DB", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": evidenceURL})
}

// DeleteEvidence removes an evidence URL from a test result.
// DELETE /admin/testing/{id}/evidence
func (h *AdminTestingHandler) DeleteEvidence(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" {
		http.Error(w, "url required", http.StatusBadRequest)
		return
	}

	_, err := h.db.ExecContext(r.Context(),
		`UPDATE manual_test_results SET evidence_urls = array_remove(evidence_urls, $1), updated_at=NOW() WHERE id=$2`,
		body.URL, id)
	if err != nil {
		h.logger.Error("failed to remove evidence URL", "error", err)
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
		Description  string `json:"description"`
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
		`INSERT INTO manual_test_results (scenario_id, scenario_name, description, clinic, category) VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (scenario_id, clinic) DO UPDATE SET scenario_name=EXCLUDED.scenario_name, description=EXCLUDED.description, updated_at=NOW()
		 RETURNING id`,
		body.ScenarioID, body.ScenarioName, body.Description, body.Clinic, body.Category).Scan(&id)
	if err != nil {
		h.logger.Error("failed to create test result", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]int{"id": id})
}
