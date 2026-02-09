package conversation

import (
	"context"
	"testing"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/browser"
)

type mockBrowserClient struct {
	response *browser.AvailabilityResponse
	err      error
	ready    bool
}

func (m *mockBrowserClient) GetAvailability(ctx context.Context, req browser.AvailabilityRequest) (*browser.AvailabilityResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func (m *mockBrowserClient) GetBatchAvailability(ctx context.Context, req browser.BatchAvailabilityRequest) (*browser.BatchAvailabilityResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	// Build batch response from single-date response for each date
	var results []browser.AvailabilityResponse
	for _, date := range req.Dates {
		if m.response != nil {
			r := *m.response
			r.Date = date
			results = append(results, r)
		}
	}
	return &browser.BatchAvailabilityResponse{Success: true, Results: results}, nil
}

func (m *mockBrowserClient) IsReady(ctx context.Context) bool {
	return m.ready
}

func TestBrowserAdapter_GetUpcomingAvailability(t *testing.T) {
	t.Run("returns empty when client is nil", func(t *testing.T) {
		adapter := NewBrowserAdapter(nil, nil)
		slots, err := adapter.GetUpcomingAvailability(context.Background(), "https://example.com/booking", 7)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if slots != nil {
			t.Error("expected nil slots when client is nil")
		}
	})

	t.Run("returns error when booking URL is empty", func(t *testing.T) {
		adapter := NewBrowserAdapter(&mockBrowserClient{ready: true}, nil)
		_, err := adapter.GetUpcomingAvailability(context.Background(), "", 7)
		if err == nil {
			t.Error("expected error for empty booking URL")
		}
	})

	t.Run("converts browser slots to availability slots", func(t *testing.T) {
		mock := &mockBrowserClient{
			ready: true,
			response: &browser.AvailabilityResponse{
				Success:    true,
				BookingURL: "https://example.com/booking",
				Date:       time.Now().Format("2006-01-02"),
				Slots: []browser.TimeSlot{
					{Time: "10:00 AM", Available: true, Provider: "Dr. Smith"},
					{Time: "11:00 AM", Available: true, Provider: "Dr. Jones"},
					{Time: "12:00 PM", Available: false}, // should be filtered out
				},
				ScrapedAt: time.Now().Format(time.RFC3339),
			},
		}

		adapter := NewBrowserAdapter(mock, nil)
		slots, err := adapter.GetUpcomingAvailability(context.Background(), "https://example.com/booking", 7)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(slots) != 2 {
			t.Errorf("expected 2 available slots, got %d", len(slots))
		}

		// Check first slot
		if slots[0].ProviderName != "Dr. Smith" {
			t.Errorf("expected provider Dr. Smith, got %s", slots[0].ProviderName)
		}
	})

	t.Run("handles scrape failure", func(t *testing.T) {
		mock := &mockBrowserClient{
			ready: true,
			response: &browser.AvailabilityResponse{
				Success: false,
				Error:   "Page not found",
			},
		}

		adapter := NewBrowserAdapter(mock, nil)
		_, err := adapter.GetUpcomingAvailability(context.Background(), "https://example.com/booking", 7)
		if err == nil {
			t.Error("expected error for failed scrape")
		}
	})

	t.Run("limits days to 14 max", func(t *testing.T) {
		mock := &mockBrowserClient{
			ready: true,
			response: &browser.AvailabilityResponse{
				Success: true,
				Slots:   []browser.TimeSlot{},
			},
		}

		adapter := NewBrowserAdapter(mock, nil)
		// Request 30 days, but should be limited to 14
		_, err := adapter.GetUpcomingAvailability(context.Background(), "https://example.com/booking", 30)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("defaults days to 7 when zero or negative", func(t *testing.T) {
		mock := &mockBrowserClient{
			ready: true,
			response: &browser.AvailabilityResponse{
				Success: true,
				Slots:   []browser.TimeSlot{},
			},
		}

		adapter := NewBrowserAdapter(mock, nil)
		_, err := adapter.GetUpcomingAvailability(context.Background(), "https://example.com/booking", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestBrowserAdapter_IsConfigured(t *testing.T) {
	t.Run("returns false for nil adapter", func(t *testing.T) {
		var adapter *BrowserAdapter
		if adapter.IsConfigured() {
			t.Error("expected false for nil adapter")
		}
	})

	t.Run("returns false for nil client", func(t *testing.T) {
		adapter := NewBrowserAdapter(nil, nil)
		if adapter.IsConfigured() {
			t.Error("expected false for nil client")
		}
	})

	t.Run("returns true when client is set", func(t *testing.T) {
		adapter := NewBrowserAdapter(&mockBrowserClient{}, nil)
		if !adapter.IsConfigured() {
			t.Error("expected true when client is set")
		}
	})
}

func TestBrowserAdapter_IsReady(t *testing.T) {
	t.Run("returns false for nil adapter", func(t *testing.T) {
		var adapter *BrowserAdapter
		if adapter.IsReady(context.Background()) {
			t.Error("expected false for nil adapter")
		}
	})

	t.Run("returns client ready status", func(t *testing.T) {
		mock := &mockBrowserClient{ready: true}
		adapter := NewBrowserAdapter(mock, nil)
		if !adapter.IsReady(context.Background()) {
			t.Error("expected true when client is ready")
		}

		mock.ready = false
		if adapter.IsReady(context.Background()) {
			t.Error("expected false when client is not ready")
		}
	})
}

func TestParseTimeSlot(t *testing.T) {
	tests := []struct {
		name     string
		date     string
		time     string
		wantHour int
		wantMin  int
		wantErr  bool
	}{
		{"12-hour AM", "2024-01-15", "10:00 AM", 10, 0, false},
		{"12-hour PM", "2024-01-15", "2:30 PM", 14, 30, false},
		{"12-hour no space", "2024-01-15", "10:00AM", 10, 0, false},
		{"24-hour", "2024-01-15", "14:30", 14, 30, false},
		{"hour only AM", "2024-01-15", "3 PM", 15, 0, false},
		{"invalid", "2024-01-15", "invalid", 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseTimeSlot(tt.date, tt.time)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTimeSlot() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if result.Hour() != tt.wantHour {
					t.Errorf("parseTimeSlot() hour = %v, want %v", result.Hour(), tt.wantHour)
				}
				if result.Minute() != tt.wantMin {
					t.Errorf("parseTimeSlot() minute = %v, want %v", result.Minute(), tt.wantMin)
				}
			}
		})
	}
}
