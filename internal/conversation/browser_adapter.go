package conversation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/browser"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// BrowserAvailabilityProvider abstracts the browser sidecar client for testability.
type BrowserAvailabilityProvider interface {
	// GetAvailability fetches availability for a single date
	GetAvailability(ctx context.Context, req browser.AvailabilityRequest) (*browser.AvailabilityResponse, error)
	// GetBatchAvailability fetches availability for multiple dates in a single browser session
	GetBatchAvailability(ctx context.Context, req browser.BatchAvailabilityRequest) (*browser.BatchAvailabilityResponse, error)
	// IsReady checks if the sidecar is ready
	IsReady(ctx context.Context) bool
}

// BrowserAdapter wraps the browser sidecar client for conversation use.
// It provides real-time availability lookup by scraping booking pages.
type BrowserAdapter struct {
	client BrowserAvailabilityProvider
	logger *logging.Logger
}

// NewBrowserAdapter creates a browser adapter for the conversation engine.
func NewBrowserAdapter(client BrowserAvailabilityProvider, logger *logging.Logger) *BrowserAdapter {
	if logger == nil {
		logger = logging.Default()
	}
	return &BrowserAdapter{
		client: client,
		logger: logger,
	}
}

// GetUpcomingAvailability fetches available slots for the next N days by scraping the booking URL.
func (a *BrowserAdapter) GetUpcomingAvailability(ctx context.Context, bookingURL string, days int) ([]AvailabilitySlot, error) {
	if a.client == nil {
		return nil, nil // No client configured
	}
	if bookingURL == "" {
		return nil, fmt.Errorf("browser adapter: booking URL is required")
	}
	if days <= 0 {
		days = 7
	}
	if days > 14 {
		days = 14 // Browser scraping is resource-intensive, limit to 2 weeks
	}

	// Get availability for today
	now := time.Now()
	dateStr := now.Format("2006-01-02")

	a.logger.Debug("fetching browser availability", "url", bookingURL, "date", dateStr)

	resp, err := a.client.GetAvailability(ctx, browser.AvailabilityRequest{
		BookingURL: bookingURL,
		Date:       dateStr,
		Timeout:    30000, // 30 seconds
	})
	if err != nil {
		return nil, fmt.Errorf("browser adapter: %w", err)
	}

	if !resp.Success {
		a.logger.Warn("browser availability fetch failed", "error", resp.Error, "url", bookingURL)
		return nil, fmt.Errorf("browser adapter: %s", resp.Error)
	}

	// Convert browser TimeSlots to AvailabilitySlots
	result := make([]AvailabilitySlot, 0, len(resp.Slots))
	for _, slot := range resp.Slots {
		if !slot.Available {
			continue
		}

		// Parse the time string to create proper datetime
		slotTime, err := parseTimeSlot(dateStr, slot.Time)
		if err != nil {
			a.logger.Warn("failed to parse slot time", "time", slot.Time, "error", err)
			continue
		}

		duration := 30 // default 30 min
		if slot.Duration > 0 {
			duration = slot.Duration
		}

		result = append(result, AvailabilitySlot{
			ID:           fmt.Sprintf("browser-%s-%s", dateStr, slot.Time),
			ProviderName: slot.Provider,
			StartTime:    slotTime,
			EndTime:      slotTime.Add(time.Duration(duration) * time.Minute),
			ServiceType:  resp.Service,
		})
	}

	a.logger.Info("browser availability fetched", "url", bookingURL, "date", dateStr, "slots", len(result))
	return result, nil
}

// parseTimeSlot attempts to parse time strings like "10:00 AM", "2:30 PM", "14:30"
func parseTimeSlot(dateStr, timeStr string) (time.Time, error) {
	// Try common time formats
	formats := []string{
		"3:04 PM",
		"3:04PM",
		"15:04",
		"3 PM",
		"15",
	}

	timeStr = strings.TrimSpace(timeStr)

	// Normalize am/pm to uppercase — Go's time.Parse requires "AM"/"PM"
	timeStr = strings.Replace(timeStr, "am", "AM", 1)
	timeStr = strings.Replace(timeStr, "pm", "PM", 1)

	for _, format := range formats {
		t, err := time.Parse(format, timeStr)
		if err == nil {
			// Parse the date
			date, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				return time.Time{}, err
			}
			// Combine date and time
			return time.Date(date.Year(), date.Month(), date.Day(), t.Hour(), t.Minute(), 0, 0, time.Local), nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse time: %s", timeStr)
}

// IsConfigured returns true if a browser client is available.
func (a *BrowserAdapter) IsConfigured() bool {
	return a != nil && a.client != nil
}

// IsReady checks if the browser sidecar is ready to accept requests.
func (a *BrowserAdapter) IsReady(ctx context.Context) bool {
	if a == nil || a.client == nil {
		return false
	}
	return a.client.IsReady(ctx)
}

// FormatBrowserSlotsForLLM converts browser slots to a human-readable string for LLM context.
func FormatBrowserSlotsForLLM(slots []AvailabilitySlot, maxSlots int) string {
	// Reuse the existing FormatSlotsForLLM function
	return FormatSlotsForLLM(slots, maxSlots)
}
