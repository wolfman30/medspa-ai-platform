package conversation

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/wolfman30/medspa-ai-platform/internal/bookings"
)

// BookingServiceAdapter wires the bookings.Service into the worker bookingConfirmer contract.
type BookingServiceAdapter struct {
	Service *bookings.Service
}

// ConfirmBooking proxies booking confirmations if the service is configured.
func (a BookingServiceAdapter) ConfirmBooking(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID, scheduledFor *time.Time) error {
	if a.Service == nil {
		return nil
	}
	_, err := a.Service.ConfirmBooking(ctx, orgID, leadID, scheduledFor)
	return err
}
