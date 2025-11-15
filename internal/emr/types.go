package emr

import (
	"context"
	"time"
)

// Client defines the interface that all EMR integrations must implement
type Client interface {
	// GetAvailability retrieves available appointment slots for a given date range
	GetAvailability(ctx context.Context, req AvailabilityRequest) ([]Slot, error)

	// CreateAppointment books an appointment in the EMR system
	CreateAppointment(ctx context.Context, req AppointmentRequest) (*Appointment, error)

	// GetAppointment retrieves an appointment by ID
	GetAppointment(ctx context.Context, appointmentID string) (*Appointment, error)

	// CancelAppointment cancels an existing appointment
	CancelAppointment(ctx context.Context, appointmentID string) error

	// CreatePatient creates a new patient record in the EMR
	CreatePatient(ctx context.Context, patient Patient) (*Patient, error)

	// GetPatient retrieves a patient by ID
	GetPatient(ctx context.Context, patientID string) (*Patient, error)

	// SearchPatients searches for patients by phone, email, or name
	SearchPatients(ctx context.Context, query PatientSearchQuery) ([]Patient, error)
}

// AvailabilityRequest represents a request for available appointment slots
type AvailabilityRequest struct {
	ClinicID     string    // EMR-specific clinic/location identifier
	ProviderID   string    // Optional: specific provider
	ServiceType  string    // Optional: type of service (e.g., "consultation", "botox")
	StartDate    time.Time // Start of availability window
	EndDate      time.Time // End of availability window
	DurationMins int       // Required appointment duration in minutes
}

// Slot represents an available appointment time slot
type Slot struct {
	ID           string    // EMR-specific slot identifier
	ProviderID   string    // Provider offering this slot
	ProviderName string    // Human-readable provider name
	StartTime    time.Time // Slot start time
	EndTime      time.Time // Slot end time
	Status       string    // "free", "busy", "busy-unavailable", "busy-tentative"
	ServiceType  string    // Type of service this slot is for
}

// AppointmentRequest represents a request to book an appointment
type AppointmentRequest struct {
	ClinicID    string    // EMR-specific clinic/location identifier
	PatientID   string    // EMR patient ID (from CreatePatient or SearchPatients)
	ProviderID  string    // Provider ID
	SlotID      string    // Slot ID from GetAvailability
	StartTime   time.Time // Appointment start time
	EndTime     time.Time // Appointment end time
	ServiceType string    // Type of service
	Notes       string    // Optional appointment notes
	Status      string    // "proposed", "pending", "booked", "arrived", "fulfilled", "cancelled"
}

// Appointment represents a booked appointment in the EMR
type Appointment struct {
	ID           string    // EMR-specific appointment identifier
	ClinicID     string    // Clinic/location identifier
	PatientID    string    // Patient identifier
	ProviderID   string    // Provider identifier
	ProviderName string    // Human-readable provider name
	StartTime    time.Time // Appointment start time
	EndTime      time.Time // Appointment end time
	ServiceType  string    // Type of service
	Status       string    // Appointment status
	Notes        string    // Appointment notes
	CreatedAt    time.Time // When appointment was created
	UpdatedAt    time.Time // When appointment was last updated
}

// Patient represents a patient record in the EMR
type Patient struct {
	ID          string    // EMR-specific patient identifier
	FirstName   string    // Patient first name
	LastName    string    // Patient last name
	Email       string    // Patient email
	Phone       string    // Patient phone (E.164 format recommended)
	DateOfBirth time.Time // Patient date of birth
	Gender      string    // "male", "female", "other", "unknown"
	Address     Address   // Patient address
	CreatedAt   time.Time // When patient record was created
	UpdatedAt   time.Time // When patient record was last updated
}

// Address represents a physical address
type Address struct {
	Line1      string // Street address line 1
	Line2      string // Street address line 2 (optional)
	City       string // City
	State      string // State/province code
	PostalCode string // Postal/ZIP code
	Country    string // Country code (ISO 3166-1 alpha-2)
}

// PatientSearchQuery represents search criteria for finding patients
type PatientSearchQuery struct {
	Phone     string // Search by phone number
	Email     string // Search by email
	FirstName string // Search by first name
	LastName  string // Search by last name
	// At least one field must be provided
}
