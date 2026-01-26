package leads

import (
	"strings"
	"time"
)

// Lead represents a lead submission from a web form or conversation
type Lead struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Phone     string    `json:"phone"`
	Message   string    `json:"message"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`

	// Scheduling preferences (captured during AI conversation)
	ServiceInterest string `json:"service_interest,omitempty"` // e.g., "Botox", "Filler", "Consultation"
	PatientType     string `json:"patient_type,omitempty"`     // "new" or "existing"
	PastServices    string `json:"past_services,omitempty"`    // Services patient received before (for existing patients)
	PreferredDays   string `json:"preferred_days,omitempty"`   // e.g., "weekdays", "weekends", "any"
	PreferredTimes  string `json:"preferred_times,omitempty"`  // e.g., "morning", "afternoon", "evening"
	SchedulingNotes string `json:"scheduling_notes,omitempty"` // free-form notes from conversation
	DepositStatus   string `json:"deposit_status,omitempty"`   // "pending", "paid", "refunded"
	PriorityLevel   string `json:"priority_level,omitempty"`   // "normal", "priority" (deposit paid)
}

// CreateLeadRequest represents the request body for creating a lead
type CreateLeadRequest struct {
	OrgID   string `json:"-"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Phone   string `json:"phone"`
	Message string `json:"message"`
	Source  string `json:"source"`
}

// Validate validates the create lead request
func (r *CreateLeadRequest) Validate() error {
	if strings.TrimSpace(r.OrgID) == "" {
		return ErrMissingOrgID
	}
	// Name is optional - will be extracted from conversation later
	if r.Email == "" && r.Phone == "" {
		return ErrMissingContact
	}
	return nil
}
