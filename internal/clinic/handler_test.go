package clinic

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

type mockStore struct {
	configs map[string]*Config
}

func newMockStore() *mockStore {
	return &mockStore{configs: make(map[string]*Config)}
}

func (m *mockStore) Get(ctx context.Context, orgID string) (*Config, error) {
	if cfg, ok := m.configs[orgID]; ok {
		return cfg, nil
	}
	return DefaultConfig(orgID), nil
}

func (m *mockStore) Set(ctx context.Context, cfg *Config) error {
	m.configs[cfg.OrgID] = cfg
	return nil
}

// testHandler wraps Handler but uses mockStore
type testHandler struct {
	store  *mockStore
	logger *logging.Logger
}

func newTestHandler() *testHandler {
	return &testHandler{
		store:  newMockStore(),
		logger: logging.Default(),
	}
}

func (h *testHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		http.Error(w, `{"error": "org_id required"}`, http.StatusBadRequest)
		return
	}

	cfg, err := h.store.Get(r.Context(), orgID)
	if err != nil {
		http.Error(w, `{"error": "internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cfg)
}

func (h *testHandler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		http.Error(w, `{"error": "org_id required"}`, http.StatusBadRequest)
		return
	}

	var req UpdateConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error": "invalid JSON body"}`, http.StatusBadRequest)
		return
	}

	cfg, _ := h.store.Get(r.Context(), orgID)

	if req.Name != "" {
		cfg.Name = req.Name
	}
	if req.Timezone != "" {
		cfg.Timezone = req.Timezone
	}
	if req.BusinessHours != nil {
		cfg.BusinessHours = *req.BusinessHours
	}
	if req.CallbackSLAHours != nil {
		cfg.CallbackSLAHours = *req.CallbackSLAHours
	}
	if req.DepositAmountCents != nil {
		cfg.DepositAmountCents = *req.DepositAmountCents
	}
	if req.Services != nil {
		cfg.Services = req.Services
	}

	h.store.Set(r.Context(), cfg)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cfg)
}

func TestGetConfigReturnsDefault(t *testing.T) {
	h := newTestHandler()

	r := chi.NewRouter()
	r.Get("/clinics/{orgID}/config", h.GetConfig)

	req := httptest.NewRequest("GET", "/clinics/test-org-123/config", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var cfg Config
	if err := json.NewDecoder(w.Body).Decode(&cfg); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if cfg.OrgID != "test-org-123" {
		t.Errorf("expected org_id test-org-123, got %s", cfg.OrgID)
	}
	if cfg.CallbackSLAHours != 12 {
		t.Errorf("expected default callback SLA of 12, got %d", cfg.CallbackSLAHours)
	}
}

func TestUpdateConfigPartialUpdate(t *testing.T) {
	h := newTestHandler()

	r := chi.NewRouter()
	r.Put("/clinics/{orgID}/config", h.UpdateConfig)
	r.Get("/clinics/{orgID}/config", h.GetConfig)

	// Update just the name and SLA
	body := `{"name": "Glow MedSpa", "callback_sla_hours": 8}`
	req := httptest.NewRequest("PUT", "/clinics/test-org-456/config", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var cfg Config
	if err := json.NewDecoder(w.Body).Decode(&cfg); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if cfg.Name != "Glow MedSpa" {
		t.Errorf("expected name Glow MedSpa, got %s", cfg.Name)
	}
	if cfg.CallbackSLAHours != 8 {
		t.Errorf("expected callback SLA of 8, got %d", cfg.CallbackSLAHours)
	}
	// Default hours should still be preserved
	if cfg.BusinessHours.Monday == nil {
		t.Error("expected Monday hours to be preserved")
	}
}

func TestUpdateConfigWithBusinessHours(t *testing.T) {
	h := newTestHandler()

	r := chi.NewRouter()
	r.Put("/clinics/{orgID}/config", h.UpdateConfig)

	body := `{
		"name": "Weekend Spa",
		"business_hours": {
			"saturday": {"open": "10:00", "close": "16:00"},
			"sunday": {"open": "11:00", "close": "15:00"}
		}
	}`
	req := httptest.NewRequest("PUT", "/clinics/weekend-org/config", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var cfg Config
	if err := json.NewDecoder(w.Body).Decode(&cfg); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if cfg.BusinessHours.Saturday == nil {
		t.Fatal("expected Saturday hours to be set")
	}
	if cfg.BusinessHours.Saturday.Open != "10:00" {
		t.Errorf("expected Saturday open 10:00, got %s", cfg.BusinessHours.Saturday.Open)
	}
}
