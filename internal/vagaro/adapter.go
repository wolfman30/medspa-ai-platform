package vagaro

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// VagaroAdapter connects clinic config + booking orchestration to VagaroClient.
type VagaroAdapter struct {
	client *VagaroClient
	logger *logging.Logger
}

// BookingRequest is the adapter-level booking request shape.
type BookingRequest struct {
	ServiceID    string
	ProviderID   string
	Start        time.Time
	End          time.Time
	PatientName  string
	PatientEmail string
	PatientPhone string
	Notes        string
}

// BookingResult is returned by CreateBooking.
type BookingResult struct {
	Booked              bool
	ConfirmationNumber  string
	AppointmentID       string
	ScheduledFor        *time.Time
	PatientFacingReason string
}

// NewVagaroAdapter creates a Vagaro booking adapter.
func NewVagaroAdapter(client *VagaroClient, logger *logging.Logger) *VagaroAdapter {
	if logger == nil {
		logger = logging.Default()
	}
	return &VagaroAdapter{client: client, logger: logger}
}

// Name returns the booking adapter identifier.
func (a *VagaroAdapter) Name() string { return "vagaro" }

// ResolveAvailability returns available Vagaro slots for a service.
func (a *VagaroAdapter) ResolveAvailability(ctx context.Context, clinicConfig *clinic.Config, service string, preferences AvailabilityPreferences) ([]TimeSlot, error) {
	if a.client == nil {
		return nil, fmt.Errorf("vagaro client is required")
	}
	if clinicConfig == nil || !clinicConfig.UsesVagaroBooking() {
		return nil, fmt.Errorf("clinic is not configured for Vagaro booking platform")
	}
	businessID := strings.TrimSpace(clinicConfig.VagaroBusinessAlias)
	if businessID == "" {
		return nil, fmt.Errorf("missing VagaroBusinessAlias in clinic config")
	}

	serviceID := strings.TrimSpace(service)
	if clinicConfig != nil {
		serviceID = strings.TrimSpace(clinicConfig.ResolveServiceName(service))
	}
	if serviceID == "" {
		return nil, fmt.Errorf("service id is required")
	}

	date := preferences.Date
	if date.IsZero() {
		date = time.Now().UTC()
	}

	slots, err := a.client.GetAvailableSlots(ctx, businessID, serviceID, preferences.ProviderID, date)
	if err != nil {
		a.logger.Error("vagaro availability lookup failed", "error", err, "org_id", clinicConfig.OrgID)
		return nil, err
	}

	available := make([]TimeSlot, 0, len(slots))
	for _, slot := range slots {
		if slot.Available {
			available = append(available, slot)
		}
	}
	return available, nil
}

// CreateBooking creates an appointment via Vagaro REST API.
func (a *VagaroAdapter) CreateBooking(ctx context.Context, clinicConfig *clinic.Config, req BookingRequest) (*BookingResult, error) {
	if a.client == nil {
		return nil, fmt.Errorf("vagaro client is required")
	}
	if clinicConfig == nil || !clinicConfig.UsesVagaroBooking() {
		return nil, fmt.Errorf("clinic is not configured for Vagaro booking platform")
	}

	businessID := strings.TrimSpace(clinicConfig.VagaroBusinessAlias)
	if businessID == "" {
		return nil, fmt.Errorf("missing VagaroBusinessAlias in clinic config")
	}

	resp, err := a.client.CreateAppointment(ctx, AppointmentRequest{
		BusinessID:   businessID,
		ServiceID:    req.ServiceID,
		ProviderID:   req.ProviderID,
		Start:        req.Start,
		End:          req.End,
		PatientName:  req.PatientName,
		PatientEmail: req.PatientEmail,
		PatientPhone: req.PatientPhone,
		Notes:        req.Notes,
	})
	if err != nil {
		a.logger.Error("vagaro booking failed", "error", err, "org_id", clinicConfig.OrgID)
		return nil, err
	}

	result := &BookingResult{
		Booked:             resp.OK,
		ConfirmationNumber: resp.ConfirmationCode,
		AppointmentID:      resp.AppointmentID,
	}
	if !req.Start.IsZero() {
		t := req.Start
		result.ScheduledFor = &t
	}
	if !resp.OK {
		result.PatientFacingReason = "Thanks — we received your request and the clinic will follow up to confirm the appointment."
	}
	return result, nil
}
