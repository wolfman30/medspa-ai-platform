package onboarding

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
)

// OnboardingStatus contains the 10DLC onboarding status for an org.
type OnboardingStatus struct {
	OrgID           string
	Complete        bool
	BrandID         string
	BrandStatus     string
	CampaignID      string
	CampaignStatus  string
	NumbersAssigned int
	Issues          []string
}

// ValidateOnboardingComplete checks if org is fully onboarded for 10DLC.
func (s *TenDLCOnboardingService) ValidateOnboardingComplete(ctx context.Context, orgID string) (*OnboardingStatus, error) {
	ctx, span := tenDLCTracer.Start(ctx, "10dlc.validate_complete")
	defer span.End()
	span.SetAttributes(attribute.String("medspa.org_id", orgID))

	status := &OnboardingStatus{
		OrgID:    orgID,
		Complete: false,
	}

	// Check brand
	brand, err := s.GetBrandByOrgID(ctx, orgID)
	if err != nil {
		status.BrandStatus = "NOT_REGISTERED"
		status.Issues = append(status.Issues, "Brand not registered")
		return status, nil
	}
	status.BrandID = brand.ID.String()
	status.BrandStatus = string(brand.Status)

	if brand.Status != BrandStatusVerified {
		status.Issues = append(status.Issues, fmt.Sprintf("Brand status: %s", brand.Status))
		return status, nil
	}

	// Check campaign
	campaign, err := s.GetActiveCampaignByOrgID(ctx, orgID)
	if err != nil {
		status.CampaignStatus = "NOT_REGISTERED"
		status.Issues = append(status.Issues, "Campaign not registered")
		return status, nil
	}
	status.CampaignID = campaign.ID.String()
	status.CampaignStatus = string(campaign.Status)

	if campaign.Status != CampaignStatusActive {
		status.Issues = append(status.Issues, fmt.Sprintf("Campaign status: %s", campaign.Status))
		return status, nil
	}

	// Check phone number assignment
	if campaign.NumbersAssigned == 0 {
		status.Issues = append(status.Issues, "No phone numbers assigned to campaign")
		return status, nil
	}
	status.NumbersAssigned = campaign.NumbersAssigned

	status.Complete = true
	return status, nil
}
