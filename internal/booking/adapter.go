// Package booking provides a unified booking adapter interface for different
// clinic booking platforms (Moxie, Square, manual handoff, Boulevard, etc.).
package booking

import (
	"context"
	"time"
)

// LeadSummary contains the qualified lead information collected during the
// AI conversation. This is used by adapters to either automate booking or
// generate a handoff summary for the clinic.
type LeadSummary struct {
	OrgID              string
	LeadID             string
	ConversationID     string
	ClinicName         string
	PatientName        string
	PatientPhone       string
	PatientEmail       string
	ServiceRequested   string
	PatientType        string // "new" or "returning"
	SchedulePreference string // e.g. "weekday mornings", "Tuesday 3pm"
	PreferredDays      string
	PreferredTimes     string
	ConversationNotes  string // free-form summary from the AI conversation
	CollectedAt        time.Time
}

// BookingResult is returned by CreateBooking and contains the outcome.
type BookingResult struct {
	// Booked indicates whether an automated booking was created.
	Booked bool
	// HandoffMessage is the message to send to the patient when the adapter
	// cannot automate booking (e.g. manual handoff).
	HandoffMessage string
	// ConfirmationNumber is set when automated booking succeeds.
	ConfirmationNumber string
	// ScheduledFor is set when a specific time was booked.
	ScheduledFor *time.Time
}

// AvailabilitySlot represents a single available appointment slot.
type AvailabilitySlot struct {
	DateTime time.Time
	Provider string
}

// BookingAdapter is the interface that all booking platform adapters implement.
// This allows the conversation worker to handle Moxie, Square, manual handoff,
// and future platforms (Boulevard, etc.) uniformly.
type BookingAdapter interface {
	// Name returns the adapter identifier (e.g. "moxie", "manual", "boulevard").
	Name() string

	// CheckAvailability returns available slots for the given service and date range.
	// Adapters that don't support availability checks (e.g. manual handoff) return nil, nil.
	CheckAvailability(ctx context.Context, lead LeadSummary) ([]AvailabilitySlot, error)

	// CreateBooking attempts to create a booking. For automated adapters this
	// creates the appointment; for manual handoff it generates a lead summary
	// and notifies the clinic, returning a HandoffMessage for the patient.
	CreateBooking(ctx context.Context, lead LeadSummary) (*BookingResult, error)

	// GetHandoffMessage returns the patient-facing message when booking is
	// handled manually (e.g. "We've shared your info with the clinic…").
	GetHandoffMessage(clinicName string) string
}
