package telnyxclient

import (
	"errors"
	"strings"
	"time"
)

// SendMessageRequest describes an outbound SMS/MMS payload.
type SendMessageRequest struct {
	From               string
	To                 string
	Body               string
	MediaURLs          []string
	MessagingProfileID string
}

func (r SendMessageRequest) validate() error {
	if strings.TrimSpace(r.From) == "" || strings.TrimSpace(r.To) == "" {
		return errors.New("telnyxclient: from and to numbers required")
	}
	if strings.TrimSpace(r.Body) == "" && len(r.MediaURLs) == 0 {
		return errors.New("telnyxclient: body or media required")
	}
	return nil
}

// MessageResponse represents the Telnyx message resource.
type MessageResponse struct {
	ID             string    `json:"id"`
	Status         string    `json:"status"`
	From           string    `json:"from"`
	To             string    `json:"to"`
	Text           string    `json:"text"`
	CreatedAt      time.Time `json:"created_at"`
	CompletedAt    time.Time `json:"completed_at"`
	Direction      string    `json:"direction"`
	Parts          int       `json:"parts"`
	Payload        string    `json:"payload"`
	Media          []string  `json:"media_urls,omitempty"`
	CarrierMessage string    `json:"carrier_status"`
}

// HostedOrderRequest initiates a hosted messaging order.
type HostedOrderRequest struct {
	ClinicID           string `json:"clinic_id"`
	PhoneNumber        string `json:"phone_number"`
	BillingNumber      string `json:"billing_number"`
	AuthorizedContact  string `json:"authorized_contact"`
	AuthorizedEmail    string `json:"authorized_email"`
	AuthorizedPhone    string `json:"authorized_phone"`
	SignedLOA          bool   `json:"signed_loa"`
	UtilityBillPresent bool   `json:"utility_bill_present"`
}

func (r HostedOrderRequest) validate() error {
	if strings.TrimSpace(r.ClinicID) == "" {
		return errors.New("telnyxclient: clinic_id required")
	}
	if strings.TrimSpace(r.PhoneNumber) == "" {
		return errors.New("telnyxclient: phone_number required")
	}
	if strings.TrimSpace(r.AuthorizedContact) == "" {
		return errors.New("telnyxclient: authorized contact required")
	}
	return nil
}

// DocumentType enumerates hosted messaging document uploads.
type DocumentType string

const (
	DocumentTypeLOA    DocumentType = "letter_of_authorization"
	DocumentTypeBill   DocumentType = "utility_bill"
	DocumentTypeCSR    DocumentType = "csr"
	DocumentTypeCustom DocumentType = "custom"
)

// HostedOrder mirrors Telnyx hosted messaging order responses.
type HostedOrder struct {
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	PhoneNumber string    `json:"phone_number"`
	ClinicID    string    `json:"clinic_id"`
	LastError   string    `json:"last_error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// HostedEligibilityResponse indicates whether a number can be hosted.
type HostedEligibilityResponse struct {
	PhoneNumber string `json:"phone_number"`
	Eligible    bool   `json:"eligible"`
	Reason      string `json:"reason,omitempty"`
}

// BrandRequest models inputs for Telnyx 10DLC brand creation.
type BrandRequest struct {
	ClinicID     string `json:"clinic_id"`
	LegalName    string `json:"legal_name"`
	EIN          string `json:"ein,omitempty"`
	Website      string `json:"website"`
	AddressLine  string `json:"address_line"`
	City         string `json:"city"`
	State        string `json:"state"`
	PostalCode   string `json:"postal_code"`
	Country      string `json:"country"`
	ContactName  string `json:"contact_name"`
	ContactEmail string `json:"contact_email"`
	ContactPhone string `json:"contact_phone"`
	Vertical     string `json:"vertical"`
}

func (r BrandRequest) validate() error {
	switch {
	case strings.TrimSpace(r.ClinicID) == "":
		return errors.New("telnyxclient: clinic_id required")
	case strings.TrimSpace(r.LegalName) == "":
		return errors.New("telnyxclient: legal_name required")
	case strings.TrimSpace(r.Website) == "":
		return errors.New("telnyxclient: website required")
	case strings.TrimSpace(r.AddressLine) == "", strings.TrimSpace(r.City) == "", strings.TrimSpace(r.State) == "", strings.TrimSpace(r.PostalCode) == "":
		return errors.New("telnyxclient: address fields required")
	case strings.TrimSpace(r.ContactName) == "", strings.TrimSpace(r.ContactEmail) == "":
		return errors.New("telnyxclient: contact info required")
	}
	return nil
}

// Brand captures Telnyx brand records.
type Brand struct {
	ID        string    `json:"id"`
	BrandID   string    `json:"brand_id"`
	Status    string    `json:"status"`
	ClinicID  string    `json:"clinic_id"`
	CreatedAt time.Time `json:"created_at"`
}

// CampaignRequest models Telnyx campaign provisioning payload.
type CampaignRequest struct {
	BrandID        string   `json:"brand_id"`
	UseCase        string   `json:"use_case"`
	SampleMessages []string `json:"sample_messages"`
	HelpMessage    string   `json:"help_message"`
	StopMessage    string   `json:"stop_message"`
}

func (r CampaignRequest) validate() error {
	if strings.TrimSpace(r.BrandID) == "" {
		return errors.New("telnyxclient: brand_id required")
	}
	if strings.TrimSpace(r.UseCase) == "" {
		return errors.New("telnyxclient: use_case required")
	}
	if len(r.SampleMessages) == 0 {
		return errors.New("telnyxclient: sample_messages required")
	}
	if strings.TrimSpace(r.HelpMessage) == "" || strings.TrimSpace(r.StopMessage) == "" {
		return errors.New("telnyxclient: help/stop messages required")
	}
	return nil
}

// Campaign represents Telnyx campaign metadata.
type Campaign struct {
	ID             string    `json:"id"`
	CampaignID     string    `json:"campaign_id"`
	BrandID        string    `json:"brand_id"`
	Status         string    `json:"status"`
	UseCase        string    `json:"use_case"`
	SampleMessages []string  `json:"sample_messages,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}
