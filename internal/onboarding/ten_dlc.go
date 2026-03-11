package onboarding

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

var tenDLCTracer = otel.Tracer("medspa/10dlc-onboarding")

// BrandStatus represents the status of a 10DLC brand registration.
type BrandStatus string

const (
	BrandStatusPending  BrandStatus = "PENDING"
	BrandStatusVerified BrandStatus = "VERIFIED"
	BrandStatusRejected BrandStatus = "REJECTED"
	BrandStatusFailed   BrandStatus = "FAILED"
)

// CampaignStatus represents the status of a 10DLC campaign.
type CampaignStatus string

const (
	CampaignStatusPending   CampaignStatus = "PENDING"
	CampaignStatusActive    CampaignStatus = "ACTIVE"
	CampaignStatusRejected  CampaignStatus = "REJECTED"
	CampaignStatusSuspended CampaignStatus = "SUSPENDED"
	CampaignStatusExpired   CampaignStatus = "EXPIRED"
)

// Brand represents a 10DLC brand registration.
type Brand struct {
	ID                uuid.UUID
	OrgID             string
	TelnyxBrandID     string
	BusinessName      string
	EIN               string
	Status            BrandStatus
	VerificationScore int
	RejectionReason   string
	SubmittedAt       time.Time
	VerifiedAt        *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Campaign represents a 10DLC messaging campaign.
type Campaign struct {
	ID               uuid.UUID
	OrgID            string
	BrandID          uuid.UUID
	TelnyxCampaignID string
	UseCase          string // e.g., "HEALTHCARE", "APPOINTMENT_REMINDERS"
	Description      string
	SampleMessages   []string
	Status           CampaignStatus
	RejectionReason  string
	NumbersAssigned  int
	SubmittedAt      time.Time
	ApprovedAt       *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// TenDLCOnboardingService manages 10DLC brand and campaign registration.
type TenDLCOnboardingService struct {
	db         *sql.DB
	logger     *logging.Logger
	httpClient *http.Client
	apiKey     string
	baseURL    string
}

// NewTenDLCOnboardingService creates a new 10DLC onboarding service.
func NewTenDLCOnboardingService(db *sql.DB, apiKey string, logger *logging.Logger) *TenDLCOnboardingService {
	if logger == nil {
		logger = logging.Default()
	}
	return &TenDLCOnboardingService{
		db:         db,
		logger:     logger,
		apiKey:     apiKey,
		baseURL:    "https://api.telnyx.com/10dlc",
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// BrandRegistrationRequest contains details for registering a brand.
type BrandRegistrationRequest struct {
	OrgID            string
	BusinessName     string
	EIN              string
	BusinessType     string // PRIVATE_PROFIT, PUBLIC_PROFIT, NON_PROFIT
	BusinessIndustry string // HEALTHCARE
	WebsiteURL       string
	SupportEmail     string
	SupportPhone     string
	Street           string
	City             string
	State            string
	PostalCode       string
	Country          string
}

// RegisterBrand submits a brand for 10DLC registration.
func (s *TenDLCOnboardingService) RegisterBrand(ctx context.Context, req BrandRegistrationRequest) (*Brand, error) {
	ctx, span := tenDLCTracer.Start(ctx, "10dlc.register_brand")
	defer span.End()
	span.SetAttributes(
		attribute.String("medspa.org_id", req.OrgID),
		attribute.String("business.name", req.BusinessName),
	)

	// Submit to Telnyx
	telnyxReq := map[string]any{
		"entity_type":   "PRIVATE_PROFIT",
		"display_name":  req.BusinessName,
		"company_name":  req.BusinessName,
		"ein":           req.EIN,
		"vertical":      "HEALTHCARE",
		"website":       req.WebsiteURL,
		"support_email": req.SupportEmail,
		"support_phone": req.SupportPhone,
		"street":        req.Street,
		"city":          req.City,
		"state":         req.State,
		"postal_code":   req.PostalCode,
		"country":       req.Country,
	}

	respBody, err := s.telnyxRequest(ctx, "POST", "/brands", telnyxReq)
	if err != nil {
		return nil, fmt.Errorf("onboarding: register brand: %w", err)
	}

	var result struct {
		Data struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("onboarding: parse brand response: %w", err)
	}

	brand := &Brand{
		ID:            uuid.New(),
		OrgID:         req.OrgID,
		TelnyxBrandID: result.Data.ID,
		BusinessName:  req.BusinessName,
		EIN:           req.EIN,
		Status:        BrandStatus(result.Data.Status),
		SubmittedAt:   time.Now(),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := s.storeBrand(ctx, brand); err != nil {
		return nil, fmt.Errorf("onboarding: store brand: %w", err)
	}

	s.logger.Info("brand registered",
		"brand_id", brand.ID,
		"telnyx_id", brand.TelnyxBrandID,
		"business_name", brand.BusinessName,
	)

	return brand, nil
}

// CampaignRegistrationRequest contains details for registering a campaign.
type CampaignRegistrationRequest struct {
	OrgID          string
	BrandID        uuid.UUID
	UseCase        string
	Description    string
	SampleMessages []string
	WebhookURL     string
	TermsURL       string
	PrivacyURL     string
}

// RegisterCampaign submits a campaign for 10DLC registration.
func (s *TenDLCOnboardingService) RegisterCampaign(ctx context.Context, req CampaignRegistrationRequest) (*Campaign, error) {
	ctx, span := tenDLCTracer.Start(ctx, "10dlc.register_campaign")
	defer span.End()
	span.SetAttributes(
		attribute.String("medspa.org_id", req.OrgID),
		attribute.String("campaign.use_case", req.UseCase),
	)

	// Get brand's Telnyx ID
	brand, err := s.GetBrandByID(ctx, req.BrandID)
	if err != nil {
		return nil, fmt.Errorf("onboarding: get brand for campaign: %w", err)
	}
	if brand.Status != BrandStatusVerified {
		return nil, fmt.Errorf("onboarding: brand not verified, status: %s", brand.Status)
	}

	telnyxReq := map[string]any{
		"brand_id":         brand.TelnyxBrandID,
		"use_case":         req.UseCase,
		"description":      req.Description,
		"sample_messages":  req.SampleMessages,
		"message_flow":     "Two-way messaging for appointment scheduling and reminders",
		"opt_in_keywords":  []string{"START", "YES", "UNSTOP"},
		"opt_out_keywords": []string{"STOP", "CANCEL", "UNSUBSCRIBE"},
		"help_keywords":    []string{"HELP", "INFO"},
		"webhook_url":      req.WebhookURL,
		"terms_url":        req.TermsURL,
		"privacy_url":      req.PrivacyURL,
	}

	respBody, err := s.telnyxRequest(ctx, "POST", "/campaigns", telnyxReq)
	if err != nil {
		return nil, fmt.Errorf("onboarding: register campaign: %w", err)
	}

	var result struct {
		Data struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("onboarding: parse campaign response: %w", err)
	}

	campaign := &Campaign{
		ID:               uuid.New(),
		OrgID:            req.OrgID,
		BrandID:          req.BrandID,
		TelnyxCampaignID: result.Data.ID,
		UseCase:          req.UseCase,
		Description:      req.Description,
		SampleMessages:   req.SampleMessages,
		Status:           CampaignStatus(result.Data.Status),
		SubmittedAt:      time.Now(),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	if err := s.storeCampaign(ctx, campaign); err != nil {
		return nil, fmt.Errorf("onboarding: store campaign: %w", err)
	}

	s.logger.Info("campaign registered",
		"campaign_id", campaign.ID,
		"telnyx_id", campaign.TelnyxCampaignID,
		"use_case", campaign.UseCase,
	)

	return campaign, nil
}

// PollBrandStatus checks and updates brand registration status.
func (s *TenDLCOnboardingService) PollBrandStatus(ctx context.Context, brandID uuid.UUID) (*Brand, error) {
	ctx, span := tenDLCTracer.Start(ctx, "10dlc.poll_brand_status")
	defer span.End()

	brand, err := s.GetBrandByID(ctx, brandID)
	if err != nil {
		return nil, err
	}

	// Skip if already in terminal state
	if brand.Status == BrandStatusVerified || brand.Status == BrandStatusRejected {
		return brand, nil
	}

	// Poll Telnyx
	respBody, err := s.telnyxRequest(ctx, "GET", fmt.Sprintf("/brands/%s", brand.TelnyxBrandID), nil)
	if err != nil {
		return nil, fmt.Errorf("onboarding: poll brand: %w", err)
	}

	var result struct {
		Data struct {
			Status            string `json:"status"`
			VerificationScore int    `json:"verification_score"`
			RejectionReason   string `json:"rejection_reason"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("onboarding: parse brand status: %w", err)
	}

	// Update if status changed
	newStatus := BrandStatus(result.Data.Status)
	if newStatus != brand.Status {
		brand.Status = newStatus
		brand.VerificationScore = result.Data.VerificationScore
		brand.RejectionReason = result.Data.RejectionReason
		brand.UpdatedAt = time.Now()

		if newStatus == BrandStatusVerified {
			now := time.Now()
			brand.VerifiedAt = &now
		}

		if err := s.updateBrandStatus(ctx, brand); err != nil {
			return nil, fmt.Errorf("onboarding: update brand status: %w", err)
		}

		s.logger.Info("brand status updated",
			"brand_id", brand.ID,
			"old_status", brand.Status,
			"new_status", newStatus,
		)
	}

	return brand, nil
}

// PollCampaignStatus checks and updates campaign registration status.
func (s *TenDLCOnboardingService) PollCampaignStatus(ctx context.Context, campaignID uuid.UUID) (*Campaign, error) {
	ctx, span := tenDLCTracer.Start(ctx, "10dlc.poll_campaign_status")
	defer span.End()

	campaign, err := s.GetCampaignByID(ctx, campaignID)
	if err != nil {
		return nil, err
	}

	// Skip if already in terminal state
	if campaign.Status == CampaignStatusActive || campaign.Status == CampaignStatusRejected {
		return campaign, nil
	}

	// Poll Telnyx
	respBody, err := s.telnyxRequest(ctx, "GET", fmt.Sprintf("/campaigns/%s", campaign.TelnyxCampaignID), nil)
	if err != nil {
		return nil, fmt.Errorf("onboarding: poll campaign: %w", err)
	}

	var result struct {
		Data struct {
			Status          string `json:"status"`
			RejectionReason string `json:"rejection_reason"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("onboarding: parse campaign status: %w", err)
	}

	// Update if status changed
	newStatus := CampaignStatus(result.Data.Status)
	if newStatus != campaign.Status {
		campaign.Status = newStatus
		campaign.RejectionReason = result.Data.RejectionReason
		campaign.UpdatedAt = time.Now()

		if newStatus == CampaignStatusActive {
			now := time.Now()
			campaign.ApprovedAt = &now
		}

		if err := s.updateCampaignStatus(ctx, campaign); err != nil {
			return nil, fmt.Errorf("onboarding: update campaign status: %w", err)
		}

		s.logger.Info("campaign status updated",
			"campaign_id", campaign.ID,
			"old_status", campaign.Status,
			"new_status", newStatus,
		)
	}

	return campaign, nil
}

// PollAllPendingStatuses polls all pending brands and campaigns.
func (s *TenDLCOnboardingService) PollAllPendingStatuses(ctx context.Context) error {
	ctx, span := tenDLCTracer.Start(ctx, "10dlc.poll_all_pending")
	defer span.End()

	// Poll pending brands
	brands, err := s.getPendingBrands(ctx)
	if err != nil {
		return fmt.Errorf("onboarding: get pending brands: %w", err)
	}
	for _, brand := range brands {
		if _, err := s.PollBrandStatus(ctx, brand.ID); err != nil {
			s.logger.Error("failed to poll brand status", "error", err, "brand_id", brand.ID)
		}
	}

	// Poll pending campaigns
	campaigns, err := s.getPendingCampaigns(ctx)
	if err != nil {
		return fmt.Errorf("onboarding: get pending campaigns: %w", err)
	}
	for _, campaign := range campaigns {
		if _, err := s.PollCampaignStatus(ctx, campaign.ID); err != nil {
			s.logger.Error("failed to poll campaign status", "error", err, "campaign_id", campaign.ID)
		}
	}

	span.SetAttributes(
		attribute.Int("brands_polled", len(brands)),
		attribute.Int("campaigns_polled", len(campaigns)),
	)

	return nil
}

// AssignNumberToCampaign assigns a phone number to a campaign.
func (s *TenDLCOnboardingService) AssignNumberToCampaign(ctx context.Context, campaignID uuid.UUID, phoneNumber string) error {
	ctx, span := tenDLCTracer.Start(ctx, "10dlc.assign_number")
	defer span.End()
	span.SetAttributes(
		attribute.String("phone_number", phoneNumber),
	)

	campaign, err := s.GetCampaignByID(ctx, campaignID)
	if err != nil {
		return fmt.Errorf("onboarding: AssignNumberToCampaign: %w", err)
	}

	if campaign.Status != CampaignStatusActive {
		return fmt.Errorf("onboarding: campaign not active, status: %s", campaign.Status)
	}

	telnyxReq := map[string]any{
		"phone_number": phoneNumber,
	}

	_, err = s.telnyxRequest(ctx, "POST", fmt.Sprintf("/campaigns/%s/phone_numbers", campaign.TelnyxCampaignID), telnyxReq)
	if err != nil {
		return fmt.Errorf("onboarding: assign number: %w", err)
	}

	// Update assigned count
	campaign.NumbersAssigned++
	if err := s.updateCampaignStatus(ctx, campaign); err != nil {
		s.logger.Error("failed to update campaign after number assignment", "error", err)
	}

	s.logger.Info("number assigned to campaign",
		"campaign_id", campaign.ID,
		"phone_number", phoneNumber,
	)

	return nil
}
