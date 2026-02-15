package conversation

import "time"

// StructuredKnowledge holds section-based clinic knowledge.
type StructuredKnowledge struct {
	OrgID     string            `json:"org_id"`
	Version   int64             `json:"version"`
	Sections  KnowledgeSections `json:"sections"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// KnowledgeSections groups clinic knowledge into typed sections.
type KnowledgeSections struct {
	Services  ServiceSection  `json:"services"`
	Providers ProviderSection `json:"providers"`
	Policies  PolicySection   `json:"policies"`
	Custom    []CustomDoc     `json:"custom,omitempty"`
}

// ServiceSection holds all bookable services.
type ServiceSection struct {
	Items []ServiceItem `json:"items"`
}

// ServiceItem represents a single bookable service.
type ServiceItem struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Category           string   `json:"category"`
	Price              string   `json:"price"`
	PriceType          string   `json:"price_type"` // fixed, variable, free, starting_at
	DurationMinutes    int      `json:"duration_minutes"`
	Description        string   `json:"description"`
	ProviderIDs        []string `json:"provider_ids"`
	BookingID          string   `json:"booking_id"`
	Aliases            []string `json:"aliases"`
	DepositAmountCents int      `json:"deposit_amount_cents,omitempty"`
	IsAddon            bool     `json:"is_addon"`
	Order              int      `json:"order"`
}

// ProviderSection holds all providers.
type ProviderSection struct {
	Items []ProviderItem `json:"items"`
}

// ProviderItem represents a single provider.
type ProviderItem struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Title       string   `json:"title,omitempty"`
	Bio         string   `json:"bio,omitempty"`
	Specialties []string `json:"specialties,omitempty"`
	Order       int      `json:"order"`
}

// PolicySection holds clinic policies.
type PolicySection struct {
	Cancellation    string   `json:"cancellation"`
	Deposit         string   `json:"deposit"`
	AgeRequirement  string   `json:"age_requirement"`
	TermsURL        string   `json:"terms_url,omitempty"`
	BookingPolicies []string `json:"booking_policies"`
	Custom          []string `json:"custom,omitempty"`
}

// CustomDoc is a freeform knowledge document.
type CustomDoc struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}
