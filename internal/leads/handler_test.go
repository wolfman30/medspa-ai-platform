package leads

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestCreateWebLead_Success(t *testing.T) {
	repo := NewInMemoryRepository()
	logger := logging.Default()
	handler := NewHandler(repo, logger)

	reqBody := CreateLeadRequest{
		Name:    "John Doe",
		Email:   "john@example.com",
		Phone:   "+1234567890",
		Message: "Interested in botox treatment",
		Source:  "website",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/leads/web", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.CreateWebLead(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, w.Code)
	}

	var lead Lead
	if err := json.NewDecoder(w.Body).Decode(&lead); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if lead.Name != reqBody.Name {
		t.Errorf("expected name %s, got %s", reqBody.Name, lead.Name)
	}

	if lead.Email != reqBody.Email {
		t.Errorf("expected email %s, got %s", reqBody.Email, lead.Email)
	}
}

func TestCreateWebLead_InvalidRequest(t *testing.T) {
	repo := NewInMemoryRepository()
	logger := logging.Default()
	handler := NewHandler(repo, logger)

	reqBody := CreateLeadRequest{
		Name: "", // Missing required name
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/leads/web", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.CreateWebLead(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestCreateWebLead_MissingContact(t *testing.T) {
	repo := NewInMemoryRepository()
	logger := logging.Default()
	handler := NewHandler(repo, logger)

	reqBody := CreateLeadRequest{
		Name: "John Doe",
		// Missing both email and phone
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/leads/web", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.CreateWebLead(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestCreateWebLead_InvalidJSON(t *testing.T) {
	repo := NewInMemoryRepository()
	logger := logging.Default()
	handler := NewHandler(repo, logger)

	req := httptest.NewRequest(http.MethodPost, "/leads/web", strings.NewReader("{"))
	w := httptest.NewRecorder()

	handler.CreateWebLead(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

type failingRepository struct{}

func (f failingRepository) Create(context.Context, *CreateLeadRequest) (*Lead, error) {
	return nil, errors.New("boom")
}

func (f failingRepository) GetByID(context.Context, string) (*Lead, error) {
	return nil, ErrLeadNotFound
}

func TestCreateWebLead_RepositoryError(t *testing.T) {
	logger := logging.Default()
	handler := NewHandler(failingRepository{}, logger)

	payload := CreateLeadRequest{
		Name:  "Failing Repo",
		Email: "fail@example.com",
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/leads/web", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.CreateWebLead(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestRepository_Create(t *testing.T) {
	repo := NewInMemoryRepository()
	ctx := context.Background()

	req := &CreateLeadRequest{
		Name:    "Jane Smith",
		Email:   "jane@example.com",
		Phone:   "+1987654321",
		Message: "Looking for consultation",
		Source:  "facebook",
	}

	lead, err := repo.Create(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if lead.ID == "" {
		t.Error("expected lead ID to be set")
	}

	if lead.Name != req.Name {
		t.Errorf("expected name %s, got %s", req.Name, lead.Name)
	}

	if lead.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestRepository_GetByID(t *testing.T) {
	repo := NewInMemoryRepository()
	ctx := context.Background()

	req := &CreateLeadRequest{
		Name:  "Test User",
		Email: "test@example.com",
	}

	created, err := repo.Create(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if found.ID != created.ID {
		t.Errorf("expected ID %s, got %s", created.ID, found.ID)
	}
}

func TestRepository_GetByID_NotFound(t *testing.T) {
	repo := NewInMemoryRepository()
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "nonexistent")
	if err != ErrLeadNotFound {
		t.Errorf("expected ErrLeadNotFound, got %v", err)
	}
}
