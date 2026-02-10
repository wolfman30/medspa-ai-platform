// Package browser provides a client for the headless browser sidecar service.
package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// TimeSlot represents an available or unavailable appointment slot.
type TimeSlot struct {
	Time      string `json:"time"`
	Available bool   `json:"available"`
	Provider  string `json:"provider,omitempty"`
	Duration  int    `json:"duration,omitempty"` // minutes
}

// AvailabilityRequest is the request to fetch booking availability.
type AvailabilityRequest struct {
	BookingURL   string `json:"bookingUrl"`
	Date         string `json:"date"` // YYYY-MM-DD format
	ServiceName  string `json:"serviceName,omitempty"`
	ProviderName string `json:"providerName,omitempty"`
	Timeout      int    `json:"timeout,omitempty"` // milliseconds, default 30000
}

// AvailabilityResponse is the response from the browser sidecar.
type AvailabilityResponse struct {
	Success    bool       `json:"success"`
	BookingURL string     `json:"bookingUrl"`
	Date       string     `json:"date"`
	Slots      []TimeSlot `json:"slots"`
	Provider   string     `json:"provider,omitempty"`
	Service    string     `json:"service,omitempty"`
	ScrapedAt  string     `json:"scrapedAt"`
	Error      string     `json:"error,omitempty"`
}

// BatchAvailabilityRequest is the request for fetching multiple dates.
type BatchAvailabilityRequest struct {
	BookingURL   string   `json:"bookingUrl"`
	Dates        []string `json:"dates"` // YYYY-MM-DD format, max 7
	ServiceName  string   `json:"serviceName,omitempty"`
	ProviderName string   `json:"providerName,omitempty"`
	Timeout      int      `json:"timeout,omitempty"`
}

// BatchAvailabilityResponse is the response for batch requests.
type BatchAvailabilityResponse struct {
	Success bool                   `json:"success"`
	Results []AvailabilityResponse `json:"results"`
	Error   string                 `json:"error,omitempty"`
}

// HealthResponse is the health check response from the sidecar.
type HealthResponse struct {
	Status       string `json:"status"` // ok, degraded, error
	Version      string `json:"version"`
	BrowserReady bool   `json:"browserReady"`
	Uptime       int    `json:"uptime"` // seconds
}

// Client is an HTTP client for the browser sidecar service.
type Client struct {
	baseURL    string
	httpClient *http.Client
	logger     *logging.Logger
}

// ClientOption is a functional option for configuring the Client.
type ClientOption func(*Client)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = client
	}
}

// WithLogger sets a custom logger.
func WithLogger(logger *logging.Logger) ClientOption {
	return func(c *Client) {
		c.logger = logger
	}
}

// NewClient creates a new browser sidecar client.
// baseURL should be the sidecar service URL (e.g., "http://localhost:3000" for sidecar).
func NewClient(baseURL string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
		logger: logging.Default(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Health checks the health of the browser sidecar.
func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return nil, fmt.Errorf("browser: create health request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("browser: health request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("browser: health check failed with status %d: %s", resp.StatusCode, string(body))
	}

	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, fmt.Errorf("browser: decode health response: %w", err)
	}

	return &health, nil
}

// IsReady checks if the browser sidecar is ready to accept requests.
func (c *Client) IsReady(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/ready", nil)
	if err != nil {
		return false
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// GetAvailability fetches booking availability for a specific date.
func (c *Client) GetAvailability(ctx context.Context, req AvailabilityRequest) (*AvailabilityResponse, error) {
	if req.Timeout == 0 {
		req.Timeout = 30000
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("browser: marshal request: %w", err)
	}

	c.logger.Debug("fetching availability", "url", req.BookingURL, "date", req.Date)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/availability", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("browser: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("browser: request failed: %w", err)
	}
	defer resp.Body.Close()

	var result AvailabilityResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("browser: decode response: %w", err)
	}

	if !result.Success {
		c.logger.Warn("availability fetch failed", "error", result.Error, "url", req.BookingURL)
	} else {
		c.logger.Info("availability fetched", "url", req.BookingURL, "date", req.Date, "slots", len(result.Slots))
	}

	return &result, nil
}

// GetBatchAvailability fetches availability for multiple dates.
func (c *Client) GetBatchAvailability(ctx context.Context, req BatchAvailabilityRequest) (*BatchAvailabilityResponse, error) {
	if len(req.Dates) == 0 {
		return nil, fmt.Errorf("browser: dates array is required")
	}
	if len(req.Dates) > 31 {
		return nil, fmt.Errorf("browser: maximum 31 dates per batch request")
	}
	if req.Timeout == 0 {
		req.Timeout = 30000
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("browser: marshal request: %w", err)
	}

	c.logger.Debug("fetching batch availability", "url", req.BookingURL, "dates", req.Dates)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/availability/batch", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("browser: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("browser: request failed: %w", err)
	}
	defer resp.Body.Close()

	var result BatchAvailabilityResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("browser: decode response: %w", err)
	}

	return &result, nil
}

// ---------------------------------------------------------------------------
// Booking session types (mirror browser-sidecar/src/types.ts)
// ---------------------------------------------------------------------------

// BookingLeadInfo contains patient details for the booking form.
type BookingLeadInfo struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Phone     string `json:"phone"`
	Email     string `json:"email"`
	Notes     string `json:"notes,omitempty"`
}

// BookingStartRequest starts a new booking session on the browser sidecar.
type BookingStartRequest struct {
	BookingURL  string          `json:"bookingUrl"`
	Date        string          `json:"date"` // YYYY-MM-DD
	Time        string          `json:"time"` // e.g. "2:30pm"
	Lead        BookingLeadInfo `json:"lead"`
	Service     string          `json:"service,omitempty"`
	Provider    string          `json:"provider,omitempty"`
	CallbackURL string          `json:"callbackUrl,omitempty"`
	Timeout     int             `json:"timeout,omitempty"` // milliseconds, default 120000
}

// BookingStartResponse is returned when a booking session is created.
type BookingStartResponse struct {
	Success   bool   `json:"success"`
	SessionID string `json:"sessionId"`
	State     string `json:"state"`
	Error     string `json:"error,omitempty"`
}

// BookingHandoffResponse contains the payment handoff URL.
type BookingHandoffResponse struct {
	Success    bool   `json:"success"`
	SessionID  string `json:"sessionId"`
	HandoffURL string `json:"handoffUrl"`
	ExpiresAt  string `json:"expiresAt"`
	State      string `json:"state"`
	Error      string `json:"error,omitempty"`
}

// BookingConfirmationDetails contains extracted confirmation info.
type BookingConfirmationDetails struct {
	ConfirmationNumber string `json:"confirmationNumber,omitempty"`
	AppointmentTime    string `json:"appointmentTime,omitempty"`
	Provider           string `json:"provider,omitempty"`
	Service            string `json:"service,omitempty"`
	RawText            string `json:"rawText,omitempty"`
}

// BookingStatusResponse represents the current state of a booking session.
type BookingStatusResponse struct {
	Success             bool                        `json:"success"`
	SessionID           string                      `json:"sessionId"`
	State               string                      `json:"state"`
	Outcome             string                      `json:"outcome,omitempty"`
	ConfirmationDetails *BookingConfirmationDetails `json:"confirmationDetails,omitempty"`
	Error               string                      `json:"error,omitempty"`
	CreatedAt           string                      `json:"createdAt"`
	UpdatedAt           string                      `json:"updatedAt"`
}

// StartBookingSession initiates a booking session on the browser sidecar.
// The sidecar automates Steps 1-4 (service, provider, date/time, contact info)
// and stops at Step 5 (payment page).
func (c *Client) StartBookingSession(ctx context.Context, req BookingStartRequest) (*BookingStartResponse, error) {
	if req.Timeout == 0 {
		req.Timeout = 120000
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("browser: marshal booking request: %w", err)
	}

	c.logger.Info("starting booking session", "url", req.BookingURL, "date", req.Date, "time", req.Time)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/booking/start", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("browser: create booking request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("browser: booking request failed: %w", err)
	}
	defer resp.Body.Close()

	var result BookingStartResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("browser: decode booking response: %w", err)
	}

	return &result, nil
}

// GetHandoffURL retrieves the Moxie Step 5 payment page URL for a booking session.
// This is the URL sent to the patient so they can enter their card details and
// finalize the booking directly in Moxie (no Square checkout link is used).
func (c *Client) GetHandoffURL(ctx context.Context, sessionID string) (*BookingHandoffResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/v1/booking/"+sessionID+"/handoff-url", nil)
	if err != nil {
		return nil, fmt.Errorf("browser: create handoff request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("browser: handoff request failed: %w", err)
	}
	defer resp.Body.Close()

	var result BookingHandoffResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("browser: decode handoff response: %w", err)
	}

	return &result, nil
}

// GetBookingStatus checks the current state of a booking session.
func (c *Client) GetBookingStatus(ctx context.Context, sessionID string) (*BookingStatusResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/v1/booking/"+sessionID+"/status", nil)
	if err != nil {
		return nil, fmt.Errorf("browser: create status request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("browser: status request failed: %w", err)
	}
	defer resp.Body.Close()

	var result BookingStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("browser: decode status response: %w", err)
	}

	return &result, nil
}

// CancelBookingSession cancels an active booking session.
func (c *Client) CancelBookingSession(ctx context.Context, sessionID string) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		c.baseURL+"/api/v1/booking/"+sessionID, nil)
	if err != nil {
		return fmt.Errorf("browser: create cancel request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("browser: cancel request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("browser: booking session not found: %s", sessionID)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("browser: cancel failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// GetAvailableSlots is a convenience method that returns only available slots.
func (c *Client) GetAvailableSlots(ctx context.Context, bookingURL, date string) ([]TimeSlot, error) {
	resp, err := c.GetAvailability(ctx, AvailabilityRequest{
		BookingURL: bookingURL,
		Date:       date,
	})
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("browser: failed to get availability: %s", resp.Error)
	}

	var available []TimeSlot
	for _, slot := range resp.Slots {
		if slot.Available {
			available = append(available, slot)
		}
	}

	return available, nil
}

// FormatSlotsForDisplay formats available slots for display to users.
func FormatSlotsForDisplay(slots []TimeSlot) string {
	if len(slots) == 0 {
		return "No available appointments"
	}

	var buf bytes.Buffer
	buf.WriteString("Available appointments:\n")
	for i, slot := range slots {
		if slot.Available {
			buf.WriteString(fmt.Sprintf("  - %s", slot.Time))
			if slot.Provider != "" {
				buf.WriteString(fmt.Sprintf(" with %s", slot.Provider))
			}
			buf.WriteString("\n")
			if i >= 9 { // Limit to 10 slots
				remaining := len(slots) - i - 1
				if remaining > 0 {
					buf.WriteString(fmt.Sprintf("  ... and %d more\n", remaining))
				}
				break
			}
		}
	}
	return buf.String()
}
