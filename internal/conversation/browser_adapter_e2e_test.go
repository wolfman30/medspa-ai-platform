//go:build e2e
// +build e2e

package conversation

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/browser"
)

// E2E tests for the browser adapter integration.
// These tests require a running browser sidecar service.
//
// Run with: go test -tags=e2e -v ./internal/conversation/...
//
// Environment variables:
//   BROWSER_SIDECAR_URL - URL of the running sidecar (default: http://localhost:3000)
//   TEST_BOOKING_URL - URL of a test booking page (required for full tests)

func TestBrowserAdapter_E2E_Integration(t *testing.T) {
	sidecarURL := os.Getenv("BROWSER_SIDECAR_URL")
	if sidecarURL == "" {
		sidecarURL = "http://localhost:3000"
	}

	bookingURL := os.Getenv("TEST_BOOKING_URL")

	// Create real browser client
	client := browser.NewClient(sidecarURL)

	t.Run("sidecar health check", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		health, err := client.Health(ctx)
		if err != nil {
			t.Skipf("Browser sidecar not available at %s: %v", sidecarURL, err)
		}

		if health.Status != "ok" {
			t.Errorf("expected status ok, got %s", health.Status)
		}
		if !health.BrowserReady {
			t.Error("expected browser to be ready")
		}
	})

	t.Run("sidecar readiness", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if !client.IsReady(ctx) {
			t.Skip("Browser sidecar not ready")
		}
	})

	t.Run("adapter integration", func(t *testing.T) {
		if bookingURL == "" {
			t.Skip("TEST_BOOKING_URL not set, skipping full integration test")
		}

		adapter := NewBrowserAdapter(client, nil)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		slots, err := adapter.GetUpcomingAvailability(ctx, bookingURL, 7)
		if err != nil {
			t.Logf("Note: GetUpcomingAvailability returned error (may be expected for some pages): %v", err)
		}

		t.Logf("Found %d available slots", len(slots))

		for i, slot := range slots {
			if i >= 5 {
				t.Logf("  ... and %d more", len(slots)-5)
				break
			}
			t.Logf("  Slot %d: %s - %s (Provider: %s)",
				i+1,
				slot.StartTime.Format("Mon Jan 2 3:04 PM"),
				slot.EndTime.Format("3:04 PM"),
				slot.ProviderName,
			)
		}
	})

	t.Run("format slots for llm", func(t *testing.T) {
		if bookingURL == "" {
			t.Skip("TEST_BOOKING_URL not set, skipping")
		}

		adapter := NewBrowserAdapter(client, nil)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		slots, err := adapter.GetUpcomingAvailability(ctx, bookingURL, 7)
		if err != nil {
			t.Skipf("Could not fetch availability: %v", err)
		}

		formatted := FormatSlotsForLLM(slots, 5)
		t.Logf("Formatted for LLM:\n%s", formatted)

		if len(slots) > 0 && formatted == "" {
			t.Error("expected non-empty formatted string when slots exist")
		}
	})
}

func TestBrowserAdapter_E2E_ErrorHandling(t *testing.T) {
	sidecarURL := os.Getenv("BROWSER_SIDECAR_URL")
	if sidecarURL == "" {
		sidecarURL = "http://localhost:3000"
	}

	client := browser.NewClient(sidecarURL)
	adapter := NewBrowserAdapter(client, nil)

	// First check if sidecar is available
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if !client.IsReady(ctx) {
		t.Skip("Browser sidecar not available")
	}

	t.Run("handles non-existent domain", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		_, err := adapter.GetUpcomingAvailability(ctx, "https://this-domain-does-not-exist-12345.com/booking", 7)
		// Should return an error, not panic
		if err == nil {
			t.Log("Note: Non-existent domain did not return error (may have returned empty slots)")
		}
	})

	t.Run("handles non-booking page", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		slots, err := adapter.GetUpcomingAvailability(ctx, "https://example.com", 7)
		// Should complete without error (may return empty slots)
		if err != nil {
			t.Logf("Note: example.com returned error: %v", err)
		} else {
			t.Logf("example.com returned %d slots (expected 0)", len(slots))
		}
	})
}

func TestBrowserAdapter_E2E_ConcurrentRequests(t *testing.T) {
	sidecarURL := os.Getenv("BROWSER_SIDECAR_URL")
	if sidecarURL == "" {
		sidecarURL = "http://localhost:3000"
	}

	bookingURL := os.Getenv("TEST_BOOKING_URL")
	if bookingURL == "" {
		t.Skip("TEST_BOOKING_URL not set")
	}

	client := browser.NewClient(sidecarURL)

	// Check if sidecar is available
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if !client.IsReady(ctx) {
		t.Skip("Browser sidecar not available")
	}

	// Test concurrent requests
	adapter := NewBrowserAdapter(client, nil)
	concurrency := 3
	results := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		go func(id int) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			_, err := adapter.GetUpcomingAvailability(ctx, bookingURL, 7)
			results <- err
			t.Logf("Request %d completed (err: %v)", id, err)
		}(i)
	}

	// Collect results
	var errors []error
	for i := 0; i < concurrency; i++ {
		if err := <-results; err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) == concurrency {
		t.Errorf("All %d concurrent requests failed", concurrency)
	} else if len(errors) > 0 {
		t.Logf("Note: %d of %d concurrent requests failed", len(errors), concurrency)
	}
}
