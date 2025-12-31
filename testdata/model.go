package knowledge

import (
	"strings"
)

// ClinicKnowledge represents the root of the knowledge base JSON.
type ClinicKnowledge struct {
	ClinicID   string     `json:"clinic_id"`
	ClinicName string     `json:"clinic_name"`
	Documents  []Document `json:"documents"`
}

// Document represents a section of the knowledge base.
// It uses pointer fields for structured data because not all documents have all fields.
type Document struct {
	Title               string                `json:"title"`
	Category            string                `json:"category"`
	Content             string                `json:"content"`
	StructuredServices  []Service             `json:"structured_services,omitempty"`
	StructuredPolicy    *Policy               `json:"structured_policy,omitempty"`
	StructuredProviders []Provider            `json:"structured_providers,omitempty"`
	StructuredLocation  *Location             `json:"structured_location,omitempty"`
	StructuredHours     map[string]*OpenHours `json:"structured_hours,omitempty"`
}

type Service struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	PriceType       string  `json:"price_type"`
	PriceUSD        float64 `json:"price_usd"`
	DurationMinutes int     `json:"duration_minutes"`
	Description     string  `json:"description"`
}

type Policy struct {
	DepositAmountUSD        float64 `json:"deposit_amount_usd"`
	DepositRequiredFor      string  `json:"deposit_required_for"`
	CancellationNoticeHours int     `json:"cancellation_notice_hours"`
	FinancingAvailable      bool    `json:"financing_available"`
}

type Provider struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Role        string              `json:"role"`
	Specialties []string            `json:"specialties"`
	Schedule    map[string]TimeSlot `json:"schedule"`
}

type TimeSlot struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type Location struct {
	AddressLine1 string `json:"address_line1"`
	City         string `json:"city"`
	State        string `json:"state"`
	Zip          string `json:"zip"`
	Timezone     string `json:"timezone"`
}

type OpenHours struct {
	Open  string `json:"open"`
	Close string `json:"close"`
}

// --- The "Brain" Logic ---

// GetProviderSchedule returns the working hours for a specific provider on a specific day.
// Returns nil if the provider is not working or not found.
func (c *ClinicKnowledge) GetProviderSchedule(providerID string, day string) *TimeSlot {
	day = strings.ToLower(day)
	for _, doc := range c.Documents {
		for _, p := range doc.StructuredProviders {
			if p.ID == providerID {
				if slot, ok := p.Schedule[day]; ok {
					return &slot
				}
			}
		}
	}
	return nil
}

// GetService finds a service by ID to retrieve price and duration.
func (c *ClinicKnowledge) GetService(serviceID string) *Service {
	for _, doc := range c.Documents {
		for _, s := range doc.StructuredServices {
			if s.ID == serviceID {
				return &s
			}
		}
	}
	return nil
}
