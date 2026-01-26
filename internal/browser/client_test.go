package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	t.Run("creates client with defaults", func(t *testing.T) {
		client := NewClient("http://localhost:3000")
		if client == nil {
			t.Fatal("expected non-nil client")
		}
		if client.baseURL != "http://localhost:3000" {
			t.Errorf("expected baseURL http://localhost:3000, got %s", client.baseURL)
		}
	})

	t.Run("creates client with custom HTTP client", func(t *testing.T) {
		customClient := &http.Client{Timeout: 10 * time.Second}
		client := NewClient("http://localhost:3000", WithHTTPClient(customClient))
		if client.httpClient != customClient {
			t.Error("expected custom HTTP client to be set")
		}
	})
}

func TestClient_Health(t *testing.T) {
	t.Run("successful health check", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/health" {
				t.Errorf("expected path /health, got %s", r.URL.Path)
			}
			if r.Method != http.MethodGet {
				t.Errorf("expected GET method, got %s", r.Method)
			}

			resp := HealthResponse{
				Status:       "ok",
				Version:      "1.0.0",
				BrowserReady: true,
				Uptime:       100,
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewClient(server.URL)
		health, err := client.Health(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if health.Status != "ok" {
			t.Errorf("expected status ok, got %s", health.Status)
		}
		if !health.BrowserReady {
			t.Error("expected browserReady to be true")
		}
	})

	t.Run("health check failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("service unavailable"))
		}))
		defer server.Close()

		client := NewClient(server.URL)
		_, err := client.Health(context.Background())
		if err == nil {
			t.Fatal("expected error for unhealthy service")
		}
	})
}

func TestClient_IsReady(t *testing.T) {
	t.Run("returns true when ready", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/ready" {
				t.Errorf("expected path /ready, got %s", r.URL.Path)
			}
			json.NewEncoder(w).Encode(map[string]bool{"ready": true})
		}))
		defer server.Close()

		client := NewClient(server.URL)
		if !client.IsReady(context.Background()) {
			t.Error("expected IsReady to return true")
		}
	})

	t.Run("returns false when not ready", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()

		client := NewClient(server.URL)
		if client.IsReady(context.Background()) {
			t.Error("expected IsReady to return false")
		}
	})

	t.Run("returns false on connection error", func(t *testing.T) {
		client := NewClient("http://localhost:99999") // Invalid port
		if client.IsReady(context.Background()) {
			t.Error("expected IsReady to return false on connection error")
		}
	})
}

func TestClient_GetAvailability(t *testing.T) {
	t.Run("successful availability fetch", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/availability" {
				t.Errorf("expected path /api/v1/availability, got %s", r.URL.Path)
			}
			if r.Method != http.MethodPost {
				t.Errorf("expected POST method, got %s", r.Method)
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Error("expected Content-Type application/json")
			}

			var req AvailabilityRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}

			if req.BookingURL != "https://example.com/booking" {
				t.Errorf("unexpected bookingUrl: %s", req.BookingURL)
			}
			if req.Date != "2024-01-15" {
				t.Errorf("unexpected date: %s", req.Date)
			}

			resp := AvailabilityResponse{
				Success:    true,
				BookingURL: req.BookingURL,
				Date:       req.Date,
				Slots: []TimeSlot{
					{Time: "10:00 AM", Available: true},
					{Time: "11:00 AM", Available: true},
					{Time: "12:00 PM", Available: false},
				},
				ScrapedAt: "2024-01-15T10:00:00Z",
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewClient(server.URL)
		resp, err := client.GetAvailability(context.Background(), AvailabilityRequest{
			BookingURL: "https://example.com/booking",
			Date:       "2024-01-15",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !resp.Success {
			t.Error("expected success to be true")
		}
		if len(resp.Slots) != 3 {
			t.Errorf("expected 3 slots, got %d", len(resp.Slots))
		}
	})

	t.Run("availability fetch with error response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			resp := AvailabilityResponse{
				Success: false,
				Error:   "Failed to scrape page",
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewClient(server.URL)
		resp, err := client.GetAvailability(context.Background(), AvailabilityRequest{
			BookingURL: "https://example.com/booking",
			Date:       "2024-01-15",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp.Success {
			t.Error("expected success to be false")
		}
		if resp.Error == "" {
			t.Error("expected error message")
		}
	})

	t.Run("sets default timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req AvailabilityRequest
			json.NewDecoder(r.Body).Decode(&req)

			if req.Timeout != 30000 {
				t.Errorf("expected default timeout 30000, got %d", req.Timeout)
			}

			json.NewEncoder(w).Encode(AvailabilityResponse{Success: true})
		}))
		defer server.Close()

		client := NewClient(server.URL)
		client.GetAvailability(context.Background(), AvailabilityRequest{
			BookingURL: "https://example.com/booking",
			Date:       "2024-01-15",
			// Timeout not set
		})
	})
}

func TestClient_GetBatchAvailability(t *testing.T) {
	t.Run("successful batch fetch", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/availability/batch" {
				t.Errorf("expected path /api/v1/availability/batch, got %s", r.URL.Path)
			}

			resp := BatchAvailabilityResponse{
				Success: true,
				Results: []AvailabilityResponse{
					{Success: true, Date: "2024-01-15", Slots: []TimeSlot{{Time: "10:00 AM", Available: true}}},
					{Success: true, Date: "2024-01-16", Slots: []TimeSlot{{Time: "11:00 AM", Available: true}}},
				},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewClient(server.URL)
		resp, err := client.GetBatchAvailability(context.Background(), BatchAvailabilityRequest{
			BookingURL: "https://example.com/booking",
			Dates:      []string{"2024-01-15", "2024-01-16"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !resp.Success {
			t.Error("expected success to be true")
		}
		if len(resp.Results) != 2 {
			t.Errorf("expected 2 results, got %d", len(resp.Results))
		}
	})

	t.Run("rejects empty dates array", func(t *testing.T) {
		client := NewClient("http://localhost:3000")
		_, err := client.GetBatchAvailability(context.Background(), BatchAvailabilityRequest{
			BookingURL: "https://example.com/booking",
			Dates:      []string{},
		})
		if err == nil {
			t.Error("expected error for empty dates array")
		}
	})

	t.Run("rejects more than 7 dates", func(t *testing.T) {
		client := NewClient("http://localhost:3000")
		_, err := client.GetBatchAvailability(context.Background(), BatchAvailabilityRequest{
			BookingURL: "https://example.com/booking",
			Dates:      []string{"1", "2", "3", "4", "5", "6", "7", "8"},
		})
		if err == nil {
			t.Error("expected error for more than 7 dates")
		}
	})
}

func TestClient_GetAvailableSlots(t *testing.T) {
	t.Run("returns only available slots", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := AvailabilityResponse{
				Success: true,
				Slots: []TimeSlot{
					{Time: "10:00 AM", Available: true},
					{Time: "11:00 AM", Available: false},
					{Time: "12:00 PM", Available: true},
					{Time: "1:00 PM", Available: false},
				},
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewClient(server.URL)
		slots, err := client.GetAvailableSlots(context.Background(), "https://example.com/booking", "2024-01-15")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(slots) != 2 {
			t.Errorf("expected 2 available slots, got %d", len(slots))
		}

		for _, slot := range slots {
			if !slot.Available {
				t.Errorf("expected all slots to be available, got unavailable slot: %s", slot.Time)
			}
		}
	})

	t.Run("returns error on failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := AvailabilityResponse{
				Success: false,
				Error:   "scraping failed",
			}
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client := NewClient(server.URL)
		_, err := client.GetAvailableSlots(context.Background(), "https://example.com/booking", "2024-01-15")
		if err == nil {
			t.Error("expected error for failed scrape")
		}
	})
}

func TestFormatSlotsForDisplay(t *testing.T) {
	t.Run("formats available slots", func(t *testing.T) {
		slots := []TimeSlot{
			{Time: "10:00 AM", Available: true},
			{Time: "11:00 AM", Available: true, Provider: "Dr. Smith"},
		}

		result := FormatSlotsForDisplay(slots)

		if result == "" {
			t.Error("expected non-empty result")
		}
		if !contains(result, "10:00 AM") {
			t.Error("expected result to contain 10:00 AM")
		}
		if !contains(result, "Dr. Smith") {
			t.Error("expected result to contain provider name")
		}
	})

	t.Run("handles empty slots", func(t *testing.T) {
		result := FormatSlotsForDisplay([]TimeSlot{})
		if result != "No available appointments" {
			t.Errorf("expected 'No available appointments', got %s", result)
		}
	})

	t.Run("limits output to 10 slots", func(t *testing.T) {
		slots := make([]TimeSlot, 15)
		for i := range slots {
			slots[i] = TimeSlot{Time: "10:00 AM", Available: true}
		}

		result := FormatSlotsForDisplay(slots)
		if !contains(result, "and") {
			t.Error("expected result to indicate more slots available")
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
