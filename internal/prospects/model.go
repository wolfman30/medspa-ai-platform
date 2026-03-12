package prospects

import "time"

// Prospect captures CRM and operational readiness data for a single clinic lead.
type Prospect struct {
	ID            string    `json:"id"`
	ClinicName    string    `json:"clinic"`
	OwnerName     string    `json:"owner"`
	OwnerTitle    string    `json:"title"`
	Location      string    `json:"location"`
	Phone         string    `json:"phone"`
	Email         string    `json:"email"`
	Website       string    `json:"website"`
	EMR           string    `json:"emr"`
	Status        string    `json:"status"`
	Configured    bool      `json:"configured"`
	TelnyxNumber  string    `json:"telnyxNumber"`
	TenDLC        bool      `json:"10dlc"`
	SMSWorking    bool      `json:"smsWorking"`
	OrgID         string    `json:"orgId"`
	ServicesCount int       `json:"services"`
	Providers     []string  `json:"providers"`
	NextAction    string    `json:"nextAction"`
	Notes         string    `json:"notes"`
	Timeline      []Event   `json:"timeline"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// Event records a dated timeline entry for a prospect.
type Event struct {
	ID         int       `json:"id"`
	ProspectID string    `json:"prospectId"`
	Type       string    `json:"type"`
	Date       time.Time `json:"date"`
	Note       string    `json:"note"`
}
