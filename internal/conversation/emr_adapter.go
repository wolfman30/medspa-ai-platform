package conversation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/emr"
)

// EMRClient abstracts EMR operations needed by the conversation engine.
// This interface is a subset of emr.Client focused on booking flows.
type EMRClient interface {
	// GetAvailability retrieves available slots for a date range
	GetAvailability(ctx context.Context, req emr.AvailabilityRequest) ([]emr.Slot, error)
	// CreatePatient creates or finds a patient record
	CreatePatient(ctx context.Context, patient emr.Patient) (*emr.Patient, error)
	// SearchPatients finds existing patients by phone/email
	SearchPatients(ctx context.Context, query emr.PatientSearchQuery) ([]emr.Patient, error)
	// CreateAppointment books an appointment
	CreateAppointment(ctx context.Context, req emr.AppointmentRequest) (*emr.Appointment, error)
}

// AvailabilitySlot is a simplified slot representation for LLM context.
type AvailabilitySlot struct {
	ID           string
	ProviderName string
	StartTime    time.Time
	EndTime      time.Time
	ServiceType  string
}

// EMRAdapter wraps an EMR client for conversation use.
type EMRAdapter struct {
	client   EMRClient
	clinicID string // default clinic ID for this org
}

// NewEMRAdapter creates an EMR adapter for the conversation engine.
func NewEMRAdapter(client EMRClient, clinicID string) *EMRAdapter {
	return &EMRAdapter{
		client:   client,
		clinicID: clinicID,
	}
}

// GetUpcomingAvailability returns available slots for the next N days.
func (a *EMRAdapter) GetUpcomingAvailability(ctx context.Context, days int, serviceType string) ([]AvailabilitySlot, error) {
	if a.client == nil {
		return nil, nil // No EMR configured, return empty
	}
	if days <= 0 {
		days = 7
	}
	if days > 30 {
		days = 30
	}

	now := time.Now()
	req := emr.AvailabilityRequest{
		ClinicID:     a.clinicID,
		StartDate:    now,
		EndDate:      now.AddDate(0, 0, days),
		ServiceType:  serviceType,
		DurationMins: 30, // default appointment length
	}

	slots, err := a.client.GetAvailability(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("emr availability: %w", err)
	}

	result := make([]AvailabilitySlot, 0, len(slots))
	for _, s := range slots {
		if s.Status != "free" {
			continue
		}
		result = append(result, AvailabilitySlot{
			ID:           s.ID,
			ProviderName: s.ProviderName,
			StartTime:    s.StartTime,
			EndTime:      s.EndTime,
			ServiceType:  s.ServiceType,
		})
	}
	return result, nil
}

// FormatSlotsForLLM converts slots to a human-readable string for LLM context.
func FormatSlotsForLLM(slots []AvailabilitySlot, maxSlots int) string {
	if len(slots) == 0 {
		return "No available appointments found for the requested timeframe."
	}

	if maxSlots <= 0 {
		maxSlots = 5
	}
	if len(slots) > maxSlots {
		slots = slots[:maxSlots]
	}

	var sb strings.Builder
	sb.WriteString("Available appointments:\n")

	for i, slot := range slots {
		// Format: "1. Monday, Jan 15 at 2:00 PM with Dr. Smith (Consultation)"
		sb.WriteString(fmt.Sprintf("%d. %s at %s",
			i+1,
			slot.StartTime.Format("Monday, Jan 2"),
			slot.StartTime.Format("3:04 PM"),
		))
		if slot.ProviderName != "" {
			sb.WriteString(fmt.Sprintf(" with %s", slot.ProviderName))
		}
		if slot.ServiceType != "" {
			sb.WriteString(fmt.Sprintf(" (%s)", slot.ServiceType))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// FindOrCreatePatient searches for existing patient by phone, creates if not found.
func (a *EMRAdapter) FindOrCreatePatient(ctx context.Context, firstName, lastName, phone, email string) (*emr.Patient, error) {
	if a.client == nil {
		return nil, fmt.Errorf("emr client not configured")
	}

	// First, try to find existing patient by phone
	if phone != "" {
		patients, err := a.client.SearchPatients(ctx, emr.PatientSearchQuery{Phone: phone})
		if err == nil && len(patients) > 0 {
			return &patients[0], nil
		}
	}

	// Try by email
	if email != "" {
		patients, err := a.client.SearchPatients(ctx, emr.PatientSearchQuery{Email: email})
		if err == nil && len(patients) > 0 {
			return &patients[0], nil
		}
	}

	// Create new patient
	return a.client.CreatePatient(ctx, emr.Patient{
		FirstName: firstName,
		LastName:  lastName,
		Phone:     phone,
		Email:     email,
	})
}

// BookAppointment creates an appointment in the EMR.
func (a *EMRAdapter) BookAppointment(ctx context.Context, patientID, slotID, providerID string, startTime, endTime time.Time, serviceType, notes string) (*emr.Appointment, error) {
	if a.client == nil {
		return nil, fmt.Errorf("emr client not configured")
	}

	return a.client.CreateAppointment(ctx, emr.AppointmentRequest{
		ClinicID:    a.clinicID,
		PatientID:   patientID,
		ProviderID:  providerID,
		SlotID:      slotID,
		StartTime:   startTime,
		EndTime:     endTime,
		ServiceType: serviceType,
		Notes:       notes,
		Status:      "booked",
	})
}

// IsConfigured returns true if an EMR client is available.
func (a *EMRAdapter) IsConfigured() bool {
	return a != nil && a.client != nil
}
