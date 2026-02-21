package vagaro

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/booking"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// VagaroAdapter implements booking.BookingAdapter using the Vagaro REST client.
// It includes clinic-aware helpers for availability resolution and booking.
type VagaroAdapter struct {
	client *VagaroClient
	logger *logging.Logger
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

// CheckAvailability satisfies booking.BookingAdapter.
// This generic interface does not carry full clinic metadata, so this method is
// intentionally conservative and returns nil when required data is unavailable.
func (a *VagaroAdapter) CheckAvailability(_ context.Context, _ booking.LeadSummary) ([]booking.AvailabilitySlot, error) {
	return nil, nil
}

// ResolveAvailability finds bookable slots using clinic Vagaro config.
func (a *VagaroAdapter) ResolveAvailability(ctx context.Context, clinicConfig *clinic.Config, service string, preferences AvailabilityPreferences) ([]TimeSlot, error) {
	if a.client == nil {
		return nil, fmt.Errorf("vagaro client is required")
	}
	businessID := resolveBusinessID(clinicConfig)
	if businessID == "" {
		return nil, fmt.Errorf("missing Vagaro business alias/id in clinic config")
	}

	serviceID := service
	if clinicConfig != nil {
		serviceID = clinicConfig.ResolveServiceName(service)
	}
	if strings.TrimSpace(serviceID) == "" {
		return nil, fmt.Errorf("service id is required")
	}

	date := preferences.Date
	if date.IsZero() {
		date = time.Now().UTC()
	}

	slots, err := a.client.GetAvailableSlots(ctx, businessID, serviceID, preferences.ProviderID, date)
	if err != nil {
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

// CreateBooking attempts to create a booking from lead data.
// Because the lead summary does not include exact slot + provider identifiers,
// this method falls back to handoff guidance and should be paired with
// CreateBookingForClinic when a fully specified appointment request exists.
func (a *VagaroAdapter) CreateBooking(_ context.Context, lead booking.LeadSummary) (*booking.BookingResult, error) {
	return &booking.BookingResult{
		Booked:         false,
		HandoffMessage: a.GetHandoffMessage(lead.ClinicName),
	}, nil
}

// CreateBookingForClinic creates a Vagaro booking when full details are known.
func (a *VagaroAdapter) CreateBookingForClinic(ctx context.Context, clinicConfig *clinic.Config, req AppointmentRequest) (*booking.BookingResult, error) {
	if a.client == nil {
		return nil, fmt.Errorf("vagaro client is required")
	}
	if req.BusinessID == "" {
		req.BusinessID = resolveBusinessID(clinicConfig)
	}
	if req.BusinessID == "" {
		return nil, fmt.Errorf("missing Vagaro business alias/id in booking request and clinic config")
	}

	resp, err := a.client.CreateAppointment(ctx, req)
	if err != nil {
		return nil, err
	}

	result := &booking.BookingResult{
		Booked:              resp.OK,
		ConfirmationNumber:  resp.ConfirmationCode,
		HandoffMessage:      "",
	}
	if !req.Start.IsZero() {
		t := req.Start
		result.ScheduledFor = &t
	}
	if !resp.OK {
		result.HandoffMessage = a.GetHandoffMessage(clinicName(clinicConfig))
	}
	return result, nil
}

// GetHandoffMessage returns the patient-facing fallback message.
func (a *VagaroAdapter) GetHandoffMessage(clinicName string) string {
	if strings.TrimSpace(clinicName) == "" {
		clinicName = "the clinic"
	}
	return fmt.Sprintf("Thanks! I’ve shared your request with %s and the team will reach out shortly to confirm your appointment.", clinicName)
}

func resolveBusinessID(cfg *clinic.Config) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.VagaroBusinessAlias)
}

func clinicName(cfg *clinic.Config) string {
	if cfg == nil {
		return ""
	}
	return cfg.Name
}
