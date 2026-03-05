package boulevard

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// BoulevardAdapter bridges Boulevard into the booking flow.
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

// ResolveAvailability checks Boulevard for available slots.
func (a *BoulevardAdapter) ResolveAvailability(ctx context.Context, serviceID, providerID string, date time.Time) ([]TimeSlot, error) {
	if a == nil || a.client == nil {
		return nil, nil
	}
	return a.client.GetAvailableSlots(ctx, serviceID, providerID, date)
}

// CreateBooking runs the full Boulevard cart-based booking flow.
func (a *BoulevardAdapter) CreateBooking(ctx context.Context, req CreateBookingRequest) (*BookingResult, error) {
	if a == nil || a.client == nil {
		return nil, fmt.Errorf("boulevard adapter: client not configured")
	}
	return a.client.CreateBooking(ctx, req)
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
