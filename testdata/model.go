package onboarding

// RegistrationRequest captures the initial onboarding payload for 10DLC setup.
type RegistrationRequest struct {
	ClinicID         string           `json:"clinic_id"`
	BusinessProfile  BusinessProfile  `json:"business_profile"`
	Contact          Contact          `json:"contact"`
	MessagingProfile MessagingProfile `json:"messaging_profile,omitempty"`
}

// BusinessProfile describes the clinic business identity.
type BusinessProfile struct {
	LegalBusinessName string  `json:"legal_business_name"`
	DoingBusinessAs   string  `json:"doing_business_as,omitempty"`
	WebsiteURL        string  `json:"website_url,omitempty"`
	TaxID             string  `json:"tax_id,omitempty"`
	Address           Address `json:"address,omitempty"`
}

// Address captures a business address for compliance submissions.
type Address struct {
	Line1      string `json:"line1,omitempty"`
	Line2      string `json:"line2,omitempty"`
	City       string `json:"city,omitempty"`
	State      string `json:"state,omitempty"`
	PostalCode string `json:"postal_code,omitempty"`
	Country    string `json:"country,omitempty"`
}

// Contact is the primary onboarding contact.
type Contact struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	Phone string `json:"phone,omitempty"`
}

// MessagingProfile captures the intended messaging use case.
type MessagingProfile struct {
	UseCase        string   `json:"use_case,omitempty"`
	SampleMessages []string `json:"sample_messages,omitempty"`
}
