package onboarding

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/google/uuid"
)

// storeBrand persists a new brand registration to the database.
func (s *TenDLCOnboardingService) storeBrand(ctx context.Context, b *Brand) error {
	query := `
		INSERT INTO ten_dlc_brands (
			id, org_id, telnyx_brand_id, business_name, ein, status,
			verification_score, rejection_reason, submitted_at, verified_at,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`
	_, err := s.db.ExecContext(ctx, query,
		b.ID, b.OrgID, b.TelnyxBrandID, b.BusinessName, b.EIN, b.Status,
		b.VerificationScore, b.RejectionReason, b.SubmittedAt, b.VerifiedAt,
		b.CreatedAt, b.UpdatedAt,
	)
	return err
}

// storeCampaign persists a new campaign registration to the database.
func (s *TenDLCOnboardingService) storeCampaign(ctx context.Context, c *Campaign) error {
	sampleMsgs, _ := json.Marshal(c.SampleMessages)
	query := `
		INSERT INTO ten_dlc_campaigns (
			id, org_id, brand_id, telnyx_campaign_id, use_case, description,
			sample_messages, status, rejection_reason, numbers_assigned,
			submitted_at, approved_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`
	_, err := s.db.ExecContext(ctx, query,
		c.ID, c.OrgID, c.BrandID, c.TelnyxCampaignID, c.UseCase, c.Description,
		sampleMsgs, c.Status, c.RejectionReason, c.NumbersAssigned,
		c.SubmittedAt, c.ApprovedAt, c.CreatedAt, c.UpdatedAt,
	)
	return err
}

// updateBrandStatus updates brand status fields in the database.
func (s *TenDLCOnboardingService) updateBrandStatus(ctx context.Context, b *Brand) error {
	query := `
		UPDATE ten_dlc_brands
		SET status = $1, verification_score = $2, rejection_reason = $3, verified_at = $4, updated_at = $5
		WHERE id = $6
	`
	_, err := s.db.ExecContext(ctx, query,
		b.Status, b.VerificationScore, b.RejectionReason, b.VerifiedAt, b.UpdatedAt, b.ID,
	)
	return err
}

// updateCampaignStatus updates campaign status fields in the database.
func (s *TenDLCOnboardingService) updateCampaignStatus(ctx context.Context, c *Campaign) error {
	query := `
		UPDATE ten_dlc_campaigns
		SET status = $1, rejection_reason = $2, numbers_assigned = $3, approved_at = $4, updated_at = $5
		WHERE id = $6
	`
	_, err := s.db.ExecContext(ctx, query,
		c.Status, c.RejectionReason, c.NumbersAssigned, c.ApprovedAt, c.UpdatedAt, c.ID,
	)
	return err
}

// GetBrandByID retrieves a brand registration by its primary key.
func (s *TenDLCOnboardingService) GetBrandByID(ctx context.Context, id uuid.UUID) (*Brand, error) {
	query := `
		SELECT id, org_id, telnyx_brand_id, business_name, ein, status,
			   verification_score, rejection_reason, submitted_at, verified_at,
			   created_at, updated_at
		FROM ten_dlc_brands WHERE id = $1
	`
	var b Brand
	var verifiedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&b.ID, &b.OrgID, &b.TelnyxBrandID, &b.BusinessName, &b.EIN, &b.Status,
		&b.VerificationScore, &b.RejectionReason, &b.SubmittedAt, &verifiedAt,
		&b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if verifiedAt.Valid {
		b.VerifiedAt = &verifiedAt.Time
	}
	return &b, nil
}

// GetBrandByOrgID retrieves the most recent brand registration for an organization.
func (s *TenDLCOnboardingService) GetBrandByOrgID(ctx context.Context, orgID string) (*Brand, error) {
	query := `
		SELECT id, org_id, telnyx_brand_id, business_name, ein, status,
			   verification_score, rejection_reason, submitted_at, verified_at,
			   created_at, updated_at
		FROM ten_dlc_brands WHERE org_id = $1 ORDER BY created_at DESC LIMIT 1
	`
	var b Brand
	var verifiedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, query, orgID).Scan(
		&b.ID, &b.OrgID, &b.TelnyxBrandID, &b.BusinessName, &b.EIN, &b.Status,
		&b.VerificationScore, &b.RejectionReason, &b.SubmittedAt, &verifiedAt,
		&b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if verifiedAt.Valid {
		b.VerifiedAt = &verifiedAt.Time
	}
	return &b, nil
}

// GetCampaignByID retrieves a campaign registration by its primary key.
func (s *TenDLCOnboardingService) GetCampaignByID(ctx context.Context, id uuid.UUID) (*Campaign, error) {
	query := `
		SELECT id, org_id, brand_id, telnyx_campaign_id, use_case, description,
			   sample_messages, status, rejection_reason, numbers_assigned,
			   submitted_at, approved_at, created_at, updated_at
		FROM ten_dlc_campaigns WHERE id = $1
	`
	var c Campaign
	var sampleMsgs []byte
	var approvedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&c.ID, &c.OrgID, &c.BrandID, &c.TelnyxCampaignID, &c.UseCase, &c.Description,
		&sampleMsgs, &c.Status, &c.RejectionReason, &c.NumbersAssigned,
		&c.SubmittedAt, &approvedAt, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(sampleMsgs, &c.SampleMessages)
	if approvedAt.Valid {
		c.ApprovedAt = &approvedAt.Time
	}
	return &c, nil
}

// GetActiveCampaignByOrgID retrieves the most recent active campaign for an organization.
func (s *TenDLCOnboardingService) GetActiveCampaignByOrgID(ctx context.Context, orgID string) (*Campaign, error) {
	query := `
		SELECT id, org_id, brand_id, telnyx_campaign_id, use_case, description,
			   sample_messages, status, rejection_reason, numbers_assigned,
			   submitted_at, approved_at, created_at, updated_at
		FROM ten_dlc_campaigns WHERE org_id = $1 AND status = 'ACTIVE' ORDER BY created_at DESC LIMIT 1
	`
	var c Campaign
	var sampleMsgs []byte
	var approvedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, query, orgID).Scan(
		&c.ID, &c.OrgID, &c.BrandID, &c.TelnyxCampaignID, &c.UseCase, &c.Description,
		&sampleMsgs, &c.Status, &c.RejectionReason, &c.NumbersAssigned,
		&c.SubmittedAt, &approvedAt, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(sampleMsgs, &c.SampleMessages)
	if approvedAt.Valid {
		c.ApprovedAt = &approvedAt.Time
	}
	return &c, nil
}

// getPendingBrands retrieves all brands with PENDING status.
func (s *TenDLCOnboardingService) getPendingBrands(ctx context.Context) ([]*Brand, error) {
	query := `SELECT id FROM ten_dlc_brands WHERE status = 'PENDING'`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var brands []*Brand
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		brand, err := s.GetBrandByID(ctx, id)
		if err != nil {
			continue
		}
		brands = append(brands, brand)
	}
	return brands, rows.Err()
}

// getPendingCampaigns retrieves all campaigns with PENDING status.
func (s *TenDLCOnboardingService) getPendingCampaigns(ctx context.Context) ([]*Campaign, error) {
	query := `SELECT id FROM ten_dlc_campaigns WHERE status = 'PENDING'`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var campaigns []*Campaign
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		campaign, err := s.GetCampaignByID(ctx, id)
		if err != nil {
			continue
		}
		campaigns = append(campaigns, campaign)
	}
	return campaigns, rows.Err()
}
