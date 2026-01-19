package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// ClientRegistrationHandler handles client self-service registration.
type ClientRegistrationHandler struct {
	db          *sql.DB
	clinicStore *clinic.Store
	logger      *logging.Logger
}

// NewClientRegistrationHandler creates a new client registration handler.
func NewClientRegistrationHandler(db *sql.DB, redis *redis.Client, logger *logging.Logger) *ClientRegistrationHandler {
	if logger == nil {
		logger = logging.Default()
	}
	var clinicStore *clinic.Store
	if redis != nil {
		clinicStore = clinic.NewStore(redis)
	}
	return &ClientRegistrationHandler{
		db:          db,
		clinicStore: clinicStore,
		logger:      logger,
	}
}

// RegisterClinicRequest is the request body for client self-registration.
type RegisterClinicRequest struct {
	ClinicName string `json:"clinic_name"`
	OwnerEmail string `json:"owner_email"`
	OwnerPhone string `json:"owner_phone,omitempty"`
	Timezone   string `json:"timezone,omitempty"`
}

// RegisterClinicResponse is returned after successful registration.
type RegisterClinicResponse struct {
	OrgID      string    `json:"org_id"`
	ClinicName string    `json:"clinic_name"`
	OwnerEmail string    `json:"owner_email"`
	CreatedAt  time.Time `json:"created_at"`
	Message    string    `json:"message"`
}

// LookupOrgResponse is returned when looking up org by email.
type LookupOrgResponse struct {
	OrgID      string `json:"org_id"`
	ClinicName string `json:"clinic_name"`
	OwnerEmail string `json:"owner_email"`
}

// RegisterClinic creates a new clinic for a client user.
// POST /api/client/register
func (h *ClientRegistrationHandler) RegisterClinic(w http.ResponseWriter, r *http.Request) {
	var req RegisterClinicRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	req.ClinicName = strings.TrimSpace(req.ClinicName)
	req.OwnerEmail = strings.ToLower(strings.TrimSpace(req.OwnerEmail))
	req.OwnerPhone = strings.TrimSpace(req.OwnerPhone)

	if req.ClinicName == "" {
		jsonError(w, "clinic_name is required", http.StatusBadRequest)
		return
	}
	if req.OwnerEmail == "" {
		jsonError(w, "owner_email is required", http.StatusBadRequest)
		return
	}
	if !strings.Contains(req.OwnerEmail, "@") {
		jsonError(w, "invalid email format", http.StatusBadRequest)
		return
	}

	// Check if email already has an org
	existingOrg, err := h.getOrgByEmail(r.Context(), req.OwnerEmail)
	if err != nil && err != sql.ErrNoRows {
		h.logger.Error("failed to check existing org", "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if existingOrg != nil {
		jsonError(w, "an organization already exists for this email", http.StatusConflict)
		return
	}

	// Generate org ID
	orgID := uuid.New().String()

	// Set default timezone
	timezone := req.Timezone
	if timezone == "" {
		timezone = "America/New_York"
	}

	// Create org in database
	if err := h.createOrganization(r.Context(), orgID, req.ClinicName, req.OwnerEmail, req.OwnerPhone, timezone); err != nil {
		h.logger.Error("failed to create organization", "error", err)
		jsonError(w, "failed to create organization", http.StatusInternalServerError)
		return
	}

	// Create default clinic config in Redis
	if h.clinicStore != nil {
		cfg := clinic.DefaultConfig(orgID)
		cfg.Name = req.ClinicName
		cfg.Timezone = timezone
		if err := h.clinicStore.Set(r.Context(), cfg); err != nil {
			h.logger.Warn("failed to create clinic config in Redis", "error", err, "org_id", orgID)
			// Don't fail the request - the org is created in DB
		}
	}

	h.logger.Info("client registered new clinic", "org_id", orgID, "clinic_name", req.ClinicName, "owner_email", req.OwnerEmail)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(RegisterClinicResponse{
		OrgID:      orgID,
		ClinicName: req.ClinicName,
		OwnerEmail: req.OwnerEmail,
		CreatedAt:  time.Now().UTC(),
		Message:    "Clinic registered successfully. Please complete onboarding to activate SMS.",
	})
}

// LookupOrgByEmail returns the org_id for a given owner email.
// GET /api/client/org?email={email}
func (h *ClientRegistrationHandler) LookupOrgByEmail(w http.ResponseWriter, r *http.Request) {
	email := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("email")))
	if email == "" {
		jsonError(w, "email parameter required", http.StatusBadRequest)
		return
	}

	org, err := h.getOrgByEmail(r.Context(), email)
	if err == sql.ErrNoRows || org == nil {
		jsonError(w, "no organization found for this email", http.StatusNotFound)
		return
	}
	if err != nil {
		h.logger.Error("failed to lookup org", "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(LookupOrgResponse{
		OrgID:      org.ID,
		ClinicName: org.Name,
		OwnerEmail: org.OwnerEmail,
	})
}

type orgRecord struct {
	ID         string
	Name       string
	OwnerEmail string
}

func (h *ClientRegistrationHandler) getOrgByEmail(ctx context.Context, email string) (*orgRecord, error) {
	if h.db == nil {
		return nil, sql.ErrNoRows
	}
	var org orgRecord
	err := h.db.QueryRowContext(ctx,
		`SELECT id, name, COALESCE(owner_email, '') FROM organizations WHERE owner_email = $1`,
		email,
	).Scan(&org.ID, &org.Name, &org.OwnerEmail)
	if err != nil {
		return nil, err
	}
	return &org, nil
}

func (h *ClientRegistrationHandler) createOrganization(ctx context.Context, orgID, name, ownerEmail, phone, timezone string) error {
	if h.db == nil {
		return nil
	}
	_, err := h.db.ExecContext(ctx, `
		INSERT INTO organizations (id, name, owner_email, operator_phone, contact_email, timezone, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
	`, orgID, name, ownerEmail, phone, ownerEmail, timezone)
	return err
}
