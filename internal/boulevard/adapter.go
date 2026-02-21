package boulevard

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/booking"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// BoulevardAdapter bridges Boulevard into the shared booking adapter interface.
type BoulevardAdapter struct {
	client *BoulevardClient
	logger *logging.Logger
}

func NewBoulevardAdapter(client *BoulevardClient, logger *logging.Logger) *BoulevardAdapter {
	if logger == nil {
		logger = logging.Default()
	}
	return &BoulevardAdapter{client: client, logger: logger}
}

func (a *BoulevardAdapter) Name() string { return "boulevard" }

// ResolveAvailability is a Boulevard-specific helper for service/provider/date.
func (a *BoulevardAdapter) ResolveAvailability(ctx context.Context, serviceID, providerID string, date time.Time) ([]TimeSlot, error) {
	if a == nil || a.client == nil {
		return nil, nil
	}
	return a.client.GetAvailableSlots(ctx, serviceID, providerID, date)
}

// CheckAvailability maps the generic lead object to a same-day Boulevard lookup.
// The lead's ServiceRequested value is expected to be resolvable to a Boulevard service ID.
func (a *BoulevardAdapter) CheckAvailability(ctx context.Context, lead booking.LeadSummary) ([]booking.AvailabilitySlot, error) {
	if a == nil || a.client == nil {
		return nil, nil
	}
	serviceID := strings.TrimSpace(lead.ServiceRequested)
	if serviceID == "" {
		return nil, nil
	}
	providerID := ""
	slots, err := a.client.GetAvailableSlots(ctx, serviceID, providerID, time.Now())
	if err != nil {
		return nil, err
	}

	result := make([]booking.AvailabilitySlot, 0, len(slots))
	for _, s := range slots {
		result = append(result, booking.AvailabilitySlot{DateTime: s.StartAt})
	}
	return result, nil
}

// CreateBooking maps the qualified lead into Boulevard's cart booking flow.
func (a *BoulevardAdapter) CreateBooking(ctx context.Context, lead booking.LeadSummary) (*booking.BookingResult, error) {
	if a == nil || a.client == nil {
		return nil, fmt.Errorf("boulevard adapter: client not configured")
	}
	first, last := splitName(lead.PatientName)
	if first == "" {
		first = "Patient"
	}
	if last == "" {
		last = "Unknown"
	}

	bookingReq := CreateBookingRequest{
		ServiceID: strings.TrimSpace(lead.ServiceRequested),
		// Upstream flow currently does not pass explicit provider/time IDs in LeadSummary.
		StartAt: time.Now().Add(24 * time.Hour),
		Client: Client{
			FirstName: first,
			LastName:  last,
			Email:     lead.PatientEmail,
			Phone:     lead.PatientPhone,
		},
		Notes: lead.ConversationNotes,
	}

	res, err := a.client.CreateBooking(ctx, bookingReq)
	if err != nil {
		return nil, err
	}

	var scheduled *time.Time
	s := bookingReq.StartAt
	scheduled = &s

	return &booking.BookingResult{
		Booked:             true,
		ConfirmationNumber: res.BookingID,
		ScheduledFor:       scheduled,
	}, nil
}

func (a *BoulevardAdapter) GetHandoffMessage(clinicName string) string {
	if clinicName == "" {
		clinicName = "the clinic"
	}
	return fmt.Sprintf("Thanks — your booking request has been sent to %s.", clinicName)
}

func splitName(full string) (first, last string) {
	parts := strings.Fields(strings.TrimSpace(full))
	if len(parts) == 0 {
		return "", ""
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], strings.Join(parts[1:], " ")
}
