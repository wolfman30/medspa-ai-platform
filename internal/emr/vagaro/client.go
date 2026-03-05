package vagaro

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

const (
	defaultBaseURL = "https://www.vagaro.com"
	defaultTimeout = 15 * time.Second
)

// VagaroClient wraps REST calls used by Vagaro's booking widget.
// Endpoint paths are intentionally scaffolded and may be adjusted as API
// discovery is finalized.
type VagaroClient struct {
	httpClient *http.Client
	baseURL    string
	logger     *logging.Logger
}

// NewVagaroClient constructs a Vagaro REST client.
func NewVagaroClient(baseURL string, logger *logging.Logger) *VagaroClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultBaseURL
	}
	if logger == nil {
		logger = logging.Default()
	}
	return &VagaroClient{
		httpClient: &http.Client{Timeout: defaultTimeout},
		baseURL:    strings.TrimRight(baseURL, "/"),
		logger:     logger,
	}
}

// GetServices lists active services for a Vagaro business.
func (c *VagaroClient) GetServices(ctx context.Context, businessID string) ([]Service, error) {
	path := fmt.Sprintf("/api/v1/businesses/%s/services", url.PathEscape(businessID))

	var wrapped struct {
		Services []Service `json:"services"`
		Data     []Service `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &wrapped); err != nil {
		return nil, fmt.Errorf("get services: %w", err)
	}
	if len(wrapped.Services) > 0 {
		return wrapped.Services, nil
	}
	return wrapped.Data, nil
}

// GetProviders lists active providers/staff for a Vagaro business.
func (c *VagaroClient) GetProviders(ctx context.Context, businessID string) ([]Provider, error) {
	path := fmt.Sprintf("/api/v1/businesses/%s/providers", url.PathEscape(businessID))

	var wrapped struct {
		Providers []Provider `json:"providers"`
		Data      []Provider `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &wrapped); err != nil {
		return nil, fmt.Errorf("get providers: %w", err)
	}
	if len(wrapped.Providers) > 0 {
		return wrapped.Providers, nil
	}
	return wrapped.Data, nil
}

// GetAvailableSlots returns available time slots for a service/provider on a date.
func (c *VagaroClient) GetAvailableSlots(ctx context.Context, businessID, serviceID, providerID string, date time.Time) ([]TimeSlot, error) {
	q := url.Values{}
	q.Set("serviceId", serviceID)
	if providerID != "" {
		q.Set("providerId", providerID)
	}
	q.Set("date", date.Format("2006-01-02"))

	path := fmt.Sprintf("/api/v1/businesses/%s/availability?%s", url.PathEscape(businessID), q.Encode())

	var wrapped struct {
		Slots []TimeSlot `json:"slots"`
		Data  []TimeSlot `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &wrapped); err != nil {
		return nil, fmt.Errorf("get availability: %w", err)
	}
	if len(wrapped.Slots) > 0 {
		return wrapped.Slots, nil
	}
	return wrapped.Data, nil
}

// CreateAppointment creates an appointment in Vagaro when endpoint support exists.
func (c *VagaroClient) CreateAppointment(ctx context.Context, req AppointmentRequest) (*AppointmentResponse, error) {
	path := "/api/v1/appointments"
	var resp AppointmentResponse
	if err := c.doJSON(ctx, http.MethodPost, path, req, &resp); err != nil {
		return nil, fmt.Errorf("create appointment: %w", err)
	}
	return &resp, nil
}

func (c *VagaroClient) doJSON(ctx context.Context, method, path string, body interface{}, out interface{}) error {
	endpoint := c.baseURL + path

	var bodyReader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := string(respBody)
		if len(msg) > 300 {
			msg = msg[:300]
		}
		c.logger.Warn("vagaro API non-2xx response", "status", resp.StatusCode, "path", path, "body", msg)
		return fmt.Errorf("vagaro API returned %d: %s", resp.StatusCode, msg)
	}

	if len(respBody) == 0 || out == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
