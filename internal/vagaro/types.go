// Package vagaro contains Vagaro booking platform client and adapter types.
package vagaro

import "time"

// Service represents a bookable Vagaro service.
type Service struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	DurationMinutes int    `json:"durationMinutes,omitempty"`
	PriceCents      int    `json:"priceCents,omitempty"`
	Currency        string `json:"currency,omitempty"`
	Active          bool   `json:"active"`
}

// Provider represents a Vagaro staff member/provider.
type Provider struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Title  string `json:"title,omitempty"`
	Active bool   `json:"active"`
}

// TimeSlot represents a single available appointment slot.
type TimeSlot struct {
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	ProviderID string    `json:"providerId,omitempty"`
	Available  bool      `json:"available"`
}

// AppointmentRequest contains fields needed to create an appointment.
type AppointmentRequest struct {
	BusinessID   string    `json:"businessId"`
	ServiceID    string    `json:"serviceId"`
	ProviderID   string    `json:"providerId,omitempty"`
	Start        time.Time `json:"start"`
	End          time.Time `json:"end"`
	PatientName  string    `json:"patientName"`
	PatientEmail string    `json:"patientEmail,omitempty"`
	PatientPhone string    `json:"patientPhone"`
	Notes        string    `json:"notes,omitempty"`
}

// AppointmentResponse contains booking creation results.
type AppointmentResponse struct {
	OK               bool   `json:"ok"`
	AppointmentID    string `json:"appointmentId,omitempty"`
	ConfirmationCode string `json:"confirmationCode,omitempty"`
	Message          string `json:"message,omitempty"`
}

// AvailabilityPreferences are optional filters when finding appointment slots.
type AvailabilityPreferences struct {
	ProviderID string
	Date       time.Time
}
