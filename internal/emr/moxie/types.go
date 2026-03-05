package moxie

// TimeSlot represents an available appointment slot from Moxie,
// with start and end times in ISO 8601 format including timezone offset.
type TimeSlot struct {
	Start string `json:"start"` // ISO 8601 with timezone, e.g. "2026-02-19T18:45:00-05:00"
	End   string `json:"end"`
}

// DateSlots groups available time slots under a specific calendar date.
type DateSlots struct {
	Date  string     `json:"date"` // YYYY-MM-DD
	Slots []TimeSlot `json:"slots"`
}

// AvailabilityResult holds the response from querying Moxie for available time slots,
// organized by date.
type AvailabilityResult struct {
	Dates []DateSlots `json:"dates"`
}

// AppointmentResult holds the response from creating an appointment via the Moxie API.
type AppointmentResult struct {
	OK                bool   `json:"ok"`
	Message           string `json:"message"`
	ClientAccessToken string `json:"clientAccessToken"`
	AppointmentID     string `json:"appointmentId"`
}

// ServiceInput describes a service selection for appointment creation,
// including the provider, service menu item, and the requested time window.
type ServiceInput struct {
	ServiceMenuItemID string `json:"serviceMenuItemId"`
	ProviderID        string `json:"providerId"`
	StartTime         string `json:"startTime"` // ISO 8601 UTC
	EndTime           string `json:"endTime"`   // ISO 8601 UTC
}

// CreateAppointmentRequest contains all data needed to create an appointment
// through Moxie's public booking API.
type CreateAppointmentRequest struct {
	MedspaID                 string
	FirstName                string
	LastName                 string
	Email                    string
	Phone                    string
	Note                     string
	Services                 []ServiceInput
	IsNewClient              bool
	NoPreferenceProviderUsed bool
}
