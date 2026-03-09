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
	dryRun bool
	logger *logging.Logger
}

// NewBoulevardAdapter creates a new Boulevard adapter.
// dryRun controls whether mutating operations (reserve, checkout) make real API calls.
// Read-only operations (create cart, get services, get dates/times, get staff) always work.
func NewBoulevardAdapter(client *BoulevardClient, dryRun bool, logger *logging.Logger) *BoulevardAdapter {
	if logger == nil {
		logger = logging.Default()
	}
	if client != nil {
		client.SetDryRun(dryRun)
	}
	return &BoulevardAdapter{client: client, dryRun: dryRun, logger: logger}
}

func (a *BoulevardAdapter) Name() string { return "boulevard" }

// IsDryRun returns true if the adapter is in dry-run mode.
func (a *BoulevardAdapter) IsDryRun() bool { return a.dryRun }

// ResolveAvailability checks Boulevard for available slots.
// In dry-run mode with no client, returns mock slots for the next 3 business days.
// With a client configured, always calls the real API (read-only operations are safe).
func (a *BoulevardAdapter) ResolveAvailability(ctx context.Context, serviceID, providerID string, date time.Time) ([]TimeSlot, error) {
	if a == nil || a.client == nil {
		if a != nil && a.dryRun {
			a.logger.Info("BOULEVARD DRY RUN: ResolveAvailability (mock — no client configured)",
				"service_id", serviceID, "provider_id", providerID, "date", date.Format("2006-01-02"))
			return a.generateMockSlots(date), nil
		}
		return nil, fmt.Errorf("boulevard adapter: client not configured")
	}

	a.logger.Info("Boulevard: fetching availability",
		"service_id", serviceID, "provider_id", providerID, "date", date.Format("2006-01-02"),
		"dry_run", a.dryRun)

	return a.client.GetAvailableSlots(ctx, serviceID, providerID, date)
}

// CreateBooking runs the full Boulevard cart-based booking flow.
// In dry-run mode, logs the booking details and returns a fake result.
func (a *BoulevardAdapter) CreateBooking(ctx context.Context, req CreateBookingRequest) (*BookingResult, error) {
	if a.dryRun && (a == nil || a.client == nil) {
		a.logger.Info("BOULEVARD DRY RUN: CreateBooking (NOT actually booking — no client)",
			"service_id", req.ServiceID,
			"provider_id", req.ProviderID,
			"start_at", req.StartAt.Format(time.RFC3339),
			"client_name", req.Client.FirstName+" "+req.Client.LastName,
			"client_email", req.Client.Email,
			"client_phone", req.Client.Phone,
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
	// Client-level dry run handles reserve/checkout skipping
	return a.client.CreateBooking(ctx, req)
}

// generateMockSlots produces fake availability for demo/dry-run mode.
func (a *BoulevardAdapter) generateMockSlots(startDate time.Time) []TimeSlot {
	var slots []TimeSlot
	day := startDate
	daysAdded := 0
	for daysAdded < 3 {
		if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
			day = day.AddDate(0, 0, 1)
			continue
		}
		for _, hour := range []int{10, 13, 15} {
			start := time.Date(day.Year(), day.Month(), day.Day(), hour, 0, 0, 0, day.Location())
			end := start.Add(60 * time.Minute)
			slots = append(slots, TimeSlot{StartAt: start, EndAt: end})
		}
		day = day.AddDate(0, 0, 1)
		daysAdded++
	}
	return slots
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
