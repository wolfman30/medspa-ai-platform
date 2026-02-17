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

	"github.com/go-chi/chi/v5"
	"github.com/wolfman30/medspa-ai-platform/internal/tenancy"
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
	req = req.WithContext(tenancy.WithOrgID(req.Context(), "org-test"))
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
	req = req.WithContext(tenancy.WithOrgID(req.Context(), "org-test"))
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
	req = req.WithContext(tenancy.WithOrgID(req.Context(), "org-test"))
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
	req = req.WithContext(tenancy.WithOrgID(req.Context(), "org-test"))
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

func (f failingRepository) GetByID(context.Context, string, string) (*Lead, error) {
	return nil, ErrLeadNotFound
}

func (f failingRepository) GetOrCreateByPhone(context.Context, string, string, string, string) (*Lead, error) {
	return nil, errors.New("boom")
}

func (f failingRepository) UpdateSchedulingPreferences(context.Context, string, SchedulingPreferences) error {
	return errors.New("boom")
}

func (f failingRepository) UpdateDepositStatus(context.Context, string, string, string) error {
	return errors.New("boom")
}

func (f failingRepository) ListByOrg(context.Context, string, ListLeadsFilter) ([]*Lead, error) {
	return nil, errors.New("boom")
}

func (f failingRepository) UpdateSelectedAppointment(context.Context, string, SelectedAppointment) error {
	return errors.New("boom")
}

func (f failingRepository) UpdateBookingSession(context.Context, string, BookingSessionUpdate) error {
	return errors.New("boom")
}

func (f failingRepository) GetByBookingSessionID(context.Context, string) (*Lead, error) {
	return nil, errors.New("boom")
}

func (f failingRepository) UpdateEmail(context.Context, string, string) error {
	return errors.New("boom")
}

func (f failingRepository) ClearSelectedAppointment(context.Context, string) error { return nil }

func TestCreateWebLead_RepositoryError(t *testing.T) {
	logger := logging.Default()
	handler := NewHandler(failingRepository{}, logger)

	payload := CreateLeadRequest{
		Name:  "Failing Repo",
		Email: "fail@example.com",
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/leads/web", bytes.NewReader(body))
	req = req.WithContext(tenancy.WithOrgID(req.Context(), "org-test"))
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
		OrgID:   "org-test",
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
		OrgID: "org-test",
		Name:  "Test User",
		Email: "test@example.com",
	}

	created, err := repo.Create(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found, err := repo.GetByID(ctx, "org-test", created.ID)
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

	_, err := repo.GetByID(ctx, "org-test", "nonexistent")
	if err != ErrLeadNotFound {
		t.Errorf("expected ErrLeadNotFound, got %v", err)
	}
}

func TestListLeads_Success(t *testing.T) {
	repo := NewInMemoryRepository()
	logger := logging.Default()
	handler := NewHandler(repo, logger)
	ctx := context.Background()
	orgID := "org-123"

	// Create some leads
	for i := 0; i < 3; i++ {
		_, err := repo.Create(ctx, &CreateLeadRequest{
			OrgID: orgID,
			Name:  "Lead " + string(rune('A'+i)),
			Phone: "+123456789" + string(rune('0'+i)),
		})
		if err != nil {
			t.Fatalf("failed to create lead: %v", err)
		}
	}

	// Create lead for different org (shouldn't appear)
	_, _ = repo.Create(ctx, &CreateLeadRequest{
		OrgID: "other-org",
		Name:  "Other Lead",
		Phone: "+19999999999",
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/clinics/"+orgID+"/leads", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("orgID", orgID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.ListLeads(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp ListLeadsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Count != 3 {
		t.Errorf("expected 3 leads, got %d", resp.Count)
	}
}

func TestListLeads_WithPagination(t *testing.T) {
	repo := NewInMemoryRepository()
	logger := logging.Default()
	handler := NewHandler(repo, logger)
	ctx := context.Background()
	orgID := "org-456"

	// Create 5 leads
	for i := 0; i < 5; i++ {
		_, _ = repo.Create(ctx, &CreateLeadRequest{
			OrgID: orgID,
			Name:  "Lead " + string(rune('A'+i)),
			Phone: "+123456789" + string(rune('0'+i)),
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/clinics/"+orgID+"/leads?limit=2&offset=1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("orgID", orgID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.ListLeads(w, req)

	var resp ListLeadsResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp.Count != 2 {
		t.Errorf("expected 2 leads with limit, got %d", resp.Count)
	}
	if resp.Limit != 2 {
		t.Errorf("expected limit 2, got %d", resp.Limit)
	}
	if resp.Offset != 1 {
		t.Errorf("expected offset 1, got %d", resp.Offset)
	}
}

func TestListLeads_FilterByDepositStatus(t *testing.T) {
	repo := NewInMemoryRepository()
	logger := logging.Default()
	handler := NewHandler(repo, logger)
	ctx := context.Background()
	orgID := "org-789"

	// Create leads with different deposit statuses
	lead1, _ := repo.Create(ctx, &CreateLeadRequest{
		OrgID: orgID,
		Name:  "Paid Lead",
		Phone: "+11111111111",
	})
	_ = repo.UpdateDepositStatus(ctx, lead1.ID, "paid", "priority")

	lead2, _ := repo.Create(ctx, &CreateLeadRequest{
		OrgID: orgID,
		Name:  "Pending Lead",
		Phone: "+12222222222",
	})
	_ = repo.UpdateDepositStatus(ctx, lead2.ID, "pending", "normal")

	// Filter for paid only
	req := httptest.NewRequest(http.MethodGet, "/admin/clinics/"+orgID+"/leads?deposit_status=paid", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("orgID", orgID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.ListLeads(w, req)

	var resp ListLeadsResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp.Count != 1 {
		t.Errorf("expected 1 paid lead, got %d", resp.Count)
	}
	if resp.Leads[0].DepositStatus != "paid" {
		t.Errorf("expected paid status, got %s", resp.Leads[0].DepositStatus)
	}
}

func TestRepository_ListByOrg(t *testing.T) {
	repo := NewInMemoryRepository()
	ctx := context.Background()
	orgID := "org-list-test"

	// Create leads
	for i := 0; i < 3; i++ {
		_, _ = repo.Create(ctx, &CreateLeadRequest{
			OrgID: orgID,
			Name:  "Test Lead",
			Phone: "+1234567890" + string(rune('0'+i)),
		})
	}

	leads, err := repo.ListByOrg(ctx, orgID, ListLeadsFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(leads) != 3 {
		t.Errorf("expected 3 leads, got %d", len(leads))
	}
}
