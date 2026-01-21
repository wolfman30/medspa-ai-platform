package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/onboarding"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// AdminOnboardingHandler handles clinic onboarding endpoints.
type AdminOnboardingHandler struct {
	db          onboardingDB
	redis       *redis.Client
	clinicStore *clinic.Store
	logger      *logging.Logger
}

type onboardingDB interface {
	QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row
}

// AdminOnboardingConfig configures the onboarding handler.
type AdminOnboardingConfig struct {
	DB          onboardingDB
	Redis       *redis.Client
	ClinicStore *clinic.Store
	Logger      *logging.Logger
}

// NewAdminOnboardingHandler creates a new onboarding handler.
func NewAdminOnboardingHandler(cfg AdminOnboardingConfig) *AdminOnboardingHandler {
	if cfg.Logger == nil {
		cfg.Logger = logging.Default()
	}
	return &AdminOnboardingHandler{
		db:          cfg.DB,
		redis:       cfg.Redis,
		clinicStore: cfg.ClinicStore,
		logger:      cfg.Logger,
	}
}

// CreateClinicRequest is the request body for creating a new clinic.
type CreateClinicRequest struct {
	Name       string `json:"name"`
	Email      string `json:"email,omitempty"`
	Phone      string `json:"phone,omitempty"`
	Address    string `json:"address,omitempty"`
	City       string `json:"city,omitempty"`
	State      string `json:"state,omitempty"`
	ZipCode    string `json:"zip_code,omitempty"`
	WebsiteURL string `json:"website_url,omitempty"`
	Timezone   string `json:"timezone,omitempty"`
}

// CreateClinicResponse is returned when a clinic is created.
type CreateClinicResponse struct {
	OrgID     string    `json:"org_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Message   string    `json:"message"`
}

// CreateClinic creates a new clinic with a generated org_id.
// POST /admin/clinics
func (h *AdminOnboardingHandler) CreateClinic(w http.ResponseWriter, r *http.Request) {
	var req CreateClinicRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error": "invalid JSON body"}`, http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, `{"error": "name is required"}`, http.StatusBadRequest)
		return
	}

	// Generate a new org ID
	orgID := uuid.New().String()

	// Create default config with the provided name
	cfg := clinic.DefaultConfig(orgID)
	cfg.Name = strings.TrimSpace(req.Name)
	if req.Timezone != "" {
		cfg.Timezone = req.Timezone
	}
	if req.Email != "" {
		cfg.Email = strings.TrimSpace(req.Email)
	}
	if req.Phone != "" {
		cfg.Phone = strings.TrimSpace(req.Phone)
	}
	if req.Address != "" {
		cfg.Address = strings.TrimSpace(req.Address)
	}
	if req.City != "" {
		cfg.City = strings.TrimSpace(req.City)
	}
	if req.State != "" {
		cfg.State = strings.TrimSpace(req.State)
	}
	if req.ZipCode != "" {
		cfg.ZipCode = strings.TrimSpace(req.ZipCode)
	}
	if req.WebsiteURL != "" {
		cfg.WebsiteURL = strings.TrimSpace(req.WebsiteURL)
	}
	cfg.ClinicInfoConfirmed = true

	if err := h.upsertOrganization(r.Context(), orgID, cfg.Name, strings.TrimSpace(req.Phone), strings.TrimSpace(req.Email), cfg.Timezone); err != nil {
		h.logger.Error("failed to persist organization", "org_id", orgID, "error", err)
		http.Error(w, `{"error": "failed to create clinic"}`, http.StatusInternalServerError)
		return
	}

	// Save to Redis
	if err := h.clinicStore.Set(r.Context(), cfg); err != nil {
		h.logger.Error("failed to create clinic config", "org_id", orgID, "error", err)
		http.Error(w, `{"error": "failed to create clinic"}`, http.StatusInternalServerError)
		return
	}

	h.logger.Info("clinic created", "org_id", orgID, "name", cfg.Name)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(CreateClinicResponse{
		OrgID:     orgID,
		Name:      cfg.Name,
		CreatedAt: time.Now().UTC(),
		Message:   "Clinic created. Next: confirm hours, services, and connect Square.",
	})
}

func (h *AdminOnboardingHandler) upsertOrganization(ctx context.Context, orgID, name, phone, email, timezone string) error {
	if h == nil || h.db == nil {
		return nil
	}
	row := h.db.QueryRow(ctx, `
		INSERT INTO organizations (id, name, operator_phone, contact_email, timezone, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE
		SET name = EXCLUDED.name,
			operator_phone = EXCLUDED.operator_phone,
			contact_email = EXCLUDED.contact_email,
			timezone = EXCLUDED.timezone,
			updated_at = NOW()
		RETURNING id
	`, orgID, name, phone, email, timezone)
	var insertedID string
	if err := row.Scan(&insertedID); err != nil {
		return err
	}
	return nil
}

// OnboardingStep represents a single onboarding step.
type OnboardingStep struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Completed   bool   `json:"completed"`
	Required    bool   `json:"required"`
}

// OnboardingStatusResponse returns the clinic's onboarding progress.
type OnboardingStatusResponse struct {
	OrgID           string           `json:"org_id"`
	ClinicName      string           `json:"clinic_name"`
	OverallProgress int              `json:"overall_progress"` // Percentage 0-100
	ReadyForLaunch  bool             `json:"ready_for_launch"`
	SetupComplete   bool             `json:"setup_complete"`
	Steps           []OnboardingStep `json:"steps"`
	NextAction      string           `json:"next_action,omitempty"`
	NextActionURL   string           `json:"next_action_url,omitempty"`
}

// GetOnboardingStatus returns the onboarding progress for a clinic.
// GET /admin/clinics/{orgID}/onboarding-status
func (h *AdminOnboardingHandler) GetOnboardingStatus(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		http.Error(w, `{"error": "org_id required"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Get clinic config
	cfg, err := h.clinicStore.Get(ctx, orgID)
	if err != nil {
		h.logger.Error("failed to get clinic config", "org_id", orgID, "error", err)
		http.Error(w, `{"error": "internal server error"}`, http.StatusInternalServerError)
		return
	}

	// Check each onboarding step
	steps := []OnboardingStep{
		h.checkClinicConfig(cfg),
		h.checkSquareConnected(ctx, orgID),
		h.checkPhoneConfigured(ctx, orgID),
		h.checkKnowledgeSeeded(ctx, orgID),
	}

	// Calculate progress
	completedRequired := 0
	totalRequired := 0
	for _, step := range steps {
		if step.Required {
			totalRequired++
			if step.Completed {
				completedRequired++
			}
		}
	}

	progress := 0
	if totalRequired > 0 {
		progress = (completedRequired * 100) / totalRequired
	}

	setupComplete := isSetupComplete(cfg)

	// Determine next action
	nextAction := ""
	nextActionURL := ""
	for _, step := range steps {
		if step.Required && !step.Completed {
			switch step.ID {
			case "clinic_config":
				nextAction = "Configure clinic details (name, hours, services)"
				nextActionURL = "/admin/clinics/" + orgID + "/config"
			case "square_connected":
				nextAction = "Connect Square account for payments"
				nextActionURL = "/admin/clinics/" + orgID + "/square/connect"
			case "phone_configured":
				nextAction = "Set SMS phone number"
				nextActionURL = "/admin/clinics/" + orgID + "/phone"
			case "knowledge_seeded":
				nextAction = "Add clinic knowledge (services, policies)"
				nextActionURL = "/knowledge/" + orgID
			}
			break
		}
	}

	resp := OnboardingStatusResponse{
		OrgID:           orgID,
		ClinicName:      cfg.Name,
		OverallProgress: progress,
		ReadyForLaunch:  progress == 100,
		SetupComplete:   setupComplete,
		Steps:           steps,
		NextAction:      nextAction,
		NextActionURL:   nextActionURL,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *AdminOnboardingHandler) checkClinicConfig(cfg *clinic.Config) OnboardingStep {
	// Check if clinic has been configured beyond defaults
	isConfigured := cfg.ClinicInfoConfirmed
	if !isConfigured {
		isConfigured = cfg.Name != "MedSpa" && cfg.Name != ""
	}

	return OnboardingStep{
		ID:          "clinic_config",
		Name:        "Clinic Configuration",
		Description: "Set clinic name, timezone, business hours, and services",
		Completed:   isConfigured,
		Required:    true,
	}
}

func isSetupComplete(cfg *clinic.Config) bool {
	if cfg == nil {
		return false
	}
	return cfg.ClinicInfoConfirmed &&
		cfg.BusinessHoursConfirmed &&
		cfg.ServicesConfirmed &&
		cfg.ContactInfoConfirmed
}

func (h *AdminOnboardingHandler) checkSquareConnected(ctx context.Context, orgID string) OnboardingStep {
	step := OnboardingStep{
		ID:          "square_connected",
		Name:        "Square Payments",
		Description: "Connect Square account to accept deposits",
		Completed:   false,
		Required:    true,
	}

	if h.db == nil {
		return step
	}

	var merchantID string
	err := h.db.QueryRow(ctx,
		"SELECT merchant_id FROM clinic_square_credentials WHERE org_id = $1",
		orgID,
	).Scan(&merchantID)

	if err == nil && merchantID != "" {
		step.Completed = true
	}

	return step
}

func (h *AdminOnboardingHandler) checkPhoneConfigured(ctx context.Context, orgID string) OnboardingStep {
	step := OnboardingStep{
		ID:          "phone_configured",
		Name:        "SMS Phone Number",
		Description: "Configure clinic phone number for SMS",
		Completed:   false,
		Required:    true,
	}

	if h.db == nil {
		return step
	}

	var phoneNumber *string
	err := h.db.QueryRow(ctx,
		"SELECT phone_number FROM clinic_square_credentials WHERE org_id = $1",
		orgID,
	).Scan(&phoneNumber)

	if err == nil && phoneNumber != nil && *phoneNumber != "" {
		step.Completed = true
	}

	return step
}

func (h *AdminOnboardingHandler) checkKnowledgeSeeded(ctx context.Context, orgID string) OnboardingStep {
	step := OnboardingStep{
		ID:          "knowledge_seeded",
		Name:        "Clinic Knowledge",
		Description: "Add service descriptions, policies, and FAQs for AI",
		Completed:   false,
		Required:    true,
	}

	if h.redis == nil {
		return step
	}

	// Check if any knowledge documents exist
	key := "rag:docs:" + orgID
	count, err := h.redis.LLen(ctx, key).Result()
	if err == nil && count > 0 {
		step.Completed = true
	}

	return step
}

// PrefillFromWebsite scrapes a public website to prefill onboarding data.
// POST /onboarding/prefill or POST /admin/onboarding/prefill
func (h *AdminOnboardingHandler) PrefillFromWebsite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WebsiteURL string `json:"website_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	websiteURL := strings.TrimSpace(req.WebsiteURL)
	if websiteURL == "" {
		jsonError(w, "website_url is required", http.StatusBadRequest)
		return
	}

	result, err := onboarding.ScrapeClinicPrefill(r.Context(), websiteURL)
	if err != nil {
		message := err.Error()
		if strings.Contains(message, "website_url") || strings.Contains(message, "invalid") {
			jsonError(w, message, http.StatusBadRequest)
			return
		}
		jsonError(w, message, http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
