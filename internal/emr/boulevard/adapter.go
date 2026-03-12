package boulevard

import (
	"context"
	"fmt"
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// BoulevardAdapter bridges Boulevard into the booking flow.
type BoulevardAdapter struct {
	client *BoulevardClient
	dryRun bool
	logger *logging.Logger
}

// NewBoulevardAdapter creates a new Boulevard adapter.
// In dry-run mode, read-only operations (availability lookup) still hit the real API,
// but writes (reserve, checkout) are skipped and return fake results.
func NewBoulevardAdapter(client *BoulevardClient, dryRun bool, logger *logging.Logger) *BoulevardAdapter {
	if logger == nil {
		logger = logging.Default()
	}
	return &BoulevardAdapter{client: client, dryRun: dryRun, logger: logger}
}

func (a *BoulevardAdapter) Name() string { return "boulevard" }

// IsDryRun returns true if the adapter is in dry-run mode.
func (a *BoulevardAdapter) IsDryRun() bool { return a.dryRun }

// SetClient replaces the underlying client (used for per-clinic client creation).
func (a *BoulevardAdapter) SetClient(client *BoulevardClient) {
	a.client = client
}

// ResolveAvailability checks Boulevard for available slots using the real public API.
// Works in both dry-run and live mode — availability is read-only.
func (a *BoulevardAdapter) ResolveAvailability(ctx context.Context, serviceName, providerName string, date time.Time) ([]TimeSlot, error) {
	slots, _, err := a.ResolveAvailabilityWithCart(ctx, serviceName, providerName, date)
	return slots, err
}

// ResolveAvailabilityWithCart is like ResolveAvailability but also returns the cart ID
// for later booking operations.
func (a *BoulevardAdapter) ResolveAvailabilityWithCart(ctx context.Context, serviceName, providerName string, date time.Time) ([]TimeSlot, string, error) {
	if a == nil || a.client == nil {
		return nil, "", fmt.Errorf("boulevard adapter: client not configured")
	}

	tz := "America/New_York" // TODO: get from clinic config
	a.logger.Info("Boulevard: fetching real availability",
		"service", serviceName, "provider", providerName, "dry_run", a.dryRun)

	slots, cartID, err := a.client.GetAvailableSlots(ctx, serviceName, providerName, tz)
	if err != nil {
		return nil, "", fmt.Errorf("boulevard availability: %w", err)
	}

	a.logger.Info("Boulevard: availability fetched", "slots", len(slots), "service", serviceName, "cart_id", cartID)
	return slots, cartID, nil
}

// CreateBooking runs the full Boulevard cart-based booking flow.
// In dry-run mode, logs the request and returns a fake result.
func (a *BoulevardAdapter) CreateBooking(ctx context.Context, req CreateBookingRequest) (*BookingResult, error) {
	if a.dryRun {
		a.logger.Info("BOULEVARD DRY RUN: CreateBooking (NOT actually booking)",
			"service_id", req.ServiceID,
			"bookable_time_id", req.BookableTimeID,
			"client_name", req.Client.FirstName+" "+req.Client.LastName,
		)
		return &BookingResult{
			BookingID: "dry-run-" + time.Now().Format("20060102150405"),
			CartID:    "dry-run-cart",
			Status:    "DRY_RUN",
		}, nil
	}
	if a == nil || a.client == nil {
		return nil, fmt.Errorf("boulevard adapter: client not configured")
	}
	// Full flow: CreateCart → AddItem → Reserve → SetClient → Checkout
	// TODO: implement when going live
	return nil, fmt.Errorf("boulevard live booking not yet implemented")
}
