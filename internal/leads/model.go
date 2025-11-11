package leads

import (
	"strings"
	"time"
)

// Lead represents a lead submission from a web form
type Lead struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Phone     string    `json:"phone"`
	Message   string    `json:"message"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
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
	if r.Name == "" {
		return ErrInvalidName
	}
	if r.Email == "" && r.Phone == "" {
		return ErrMissingContact
	}
	return nil
}
