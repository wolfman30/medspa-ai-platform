package boulevard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

const (
	defaultTimeout = 20 * time.Second

	queryServices = `query Services($businessId: ID!) {
  services(businessId: $businessId) {
    id
    name
    duration
  }
}`

	queryProviders = `query Providers($businessId: ID!, $serviceId: ID!) {
  staff(businessId: $businessId, serviceId: $serviceId) {
    id
    fullName
  }
}`

	mutationCreateCart = `mutation CreateCart($businessId: ID!) {
  createCart(input: { businessId: $businessId }) {
    cart { id }
  }
}`

	mutationAddServiceToCart = `mutation CartAddService($cartId: ID!, $serviceId: ID!, $providerId: ID) {
  cartAddService(input: { cartId: $cartId, serviceId: $serviceId, providerId: $providerId }) {
    cart { id }
  }
}`

	queryCartAvailableSlots = `query CartAvailableTimeSlots($cartId: ID!, $date: Date!, $providerId: ID) {
  cartAvailableTimeSlots(cartId: $cartId, date: $date, providerId: $providerId) {
    startAt
    endAt
  }
}`

	mutationReserveSlot = `mutation CartReserveTimeSlot($cartId: ID!, $startAt: DateTime!) {
  cartReserveTimeSlot(input: { cartId: $cartId, startAt: $startAt }) {
    cart { id }
  }
}`

	mutationSetClient = `mutation CartSetClient($cartId: ID!, $firstName: String!, $lastName: String!, $email: String, $phone: String) {
  cartSetClient(input: {
    cartId: $cartId,
    firstName: $firstName,
    lastName: $lastName,
    email: $email,
    phone: $phone
  }) {
    cart { id }
  }
}`

	mutationCheckout = `mutation CartCheckout($cartId: ID!, $notes: String) {
  cartCheckout(input: { cartId: $cartId, notes: $notes }) {
    booking {
      id
      status
    }
  }
}`
)

// BoulevardClient is a lightweight GraphQL client for Boulevard booking flows.
type BoulevardClient struct {
	endpoint   string
	httpClient *http.Client
	apiKey     string
	businessID string
	logger     *logging.Logger
}

// NewBoulevardClient creates a new Boulevard GraphQL client.
func NewBoulevardClient(apiKey, businessID string, logger *logging.Logger) *BoulevardClient {
	if logger == nil {
		logger = logging.Default()
	}
	return &BoulevardClient{
		endpoint: defaultGraphQLEndpoint,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		apiKey:     apiKey,
		businessID: businessID,
		logger:     logger,
	}
}

// GetServices returns available services.
func (c *BoulevardClient) GetServices(ctx context.Context) ([]Service, error) {
	var out graphQLResponse[servicesData]
	if err := c.do(ctx, "Services", queryServices, map[string]interface{}{"businessId": c.businessID}, &out); err != nil {
		return nil, err
	}
	services := make([]Service, 0, len(out.Data.Services))
	for _, s := range out.Data.Services {
		services = append(services, Service{ID: s.ID, Name: s.Name, DurationMin: s.Duration})
	}
	return services, nil
}

// GetProviders returns providers/staff that can perform a service.
func (c *BoulevardClient) GetProviders(ctx context.Context, serviceID string) ([]Provider, error) {
	var out graphQLResponse[providersData]
	if err := c.do(ctx, "Providers", queryProviders, map[string]interface{}{"businessId": c.businessID, "serviceId": serviceID}, &out); err != nil {
		return nil, err
	}
	providers := make([]Provider, 0, len(out.Data.Staff))
	for _, p := range out.Data.Staff {
		providers = append(providers, Provider{ID: p.ID, Name: p.FullName})
	}
	return providers, nil
}

// GetAvailableSlots performs cart-based availability lookup.
func (c *BoulevardClient) GetAvailableSlots(ctx context.Context, serviceID, providerID string, date time.Time) ([]TimeSlot, error) {
	cartID, err := c.createCart(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.addService(ctx, cartID, serviceID, providerID); err != nil {
		return nil, err
	}

	vars := map[string]interface{}{
		"cartId": cartID,
		"date":   date.Format("2006-01-02"),
	}
	if strings.TrimSpace(providerID) != "" {
		vars["providerId"] = providerID
	}

	var out graphQLResponse[availableSlotsData]
	if err := c.do(ctx, "CartAvailableTimeSlots", queryCartAvailableSlots, vars, &out); err != nil {
		return nil, err
	}

	slots := make([]TimeSlot, 0, len(out.Data.CartAvailableTimeSlots))
	for _, s := range out.Data.CartAvailableTimeSlots {
		start, err := time.Parse(time.RFC3339, s.StartAt)
		if err != nil {
			continue
		}
		end, err := time.Parse(time.RFC3339, s.EndAt)
		if err != nil {
			continue
		}
		slots = append(slots, TimeSlot{StartAt: start, EndAt: end})
	}
	return slots, nil
}

// CreateBooking executes the full Boulevard cart flow.
func (c *BoulevardClient) CreateBooking(ctx context.Context, req CreateBookingRequest) (*BookingResult, error) {
	cartID, err := c.createCart(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.addService(ctx, cartID, req.ServiceID, req.ProviderID); err != nil {
		return nil, err
	}
	if err := c.reserveSlot(ctx, cartID, req.StartAt); err != nil {
		return nil, err
	}
	if err := c.setClient(ctx, cartID, req.Client); err != nil {
		return nil, err
	}
	return c.checkout(ctx, cartID, req.Notes)
}

func (c *BoulevardClient) createCart(ctx context.Context) (string, error) {
	var out graphQLResponse[createCartData]
	if err := c.do(ctx, "CreateCart", mutationCreateCart, map[string]interface{}{"businessId": c.businessID}, &out); err != nil {
		return "", err
	}
	if out.Data.CreateCart.Cart.ID == "" {
		return "", fmt.Errorf("boulevard: create cart returned empty cart id")
	}
	return out.Data.CreateCart.Cart.ID, nil
}

func (c *BoulevardClient) addService(ctx context.Context, cartID, serviceID, providerID string) error {
	vars := map[string]interface{}{"cartId": cartID, "serviceId": serviceID}
	if strings.TrimSpace(providerID) != "" {
		vars["providerId"] = providerID
	}
	var out graphQLResponse[addServiceData]
	if err := c.do(ctx, "CartAddService", mutationAddServiceToCart, vars, &out); err != nil {
		return err
	}
	return nil
}

func (c *BoulevardClient) reserveSlot(ctx context.Context, cartID string, startAt time.Time) error {
	var out graphQLResponse[reserveSlotData]
	return c.do(ctx, "CartReserveTimeSlot", mutationReserveSlot, map[string]interface{}{
		"cartId":  cartID,
		"startAt": startAt.UTC().Format(time.RFC3339),
	}, &out)
}

func (c *BoulevardClient) setClient(ctx context.Context, cartID string, cl Client) error {
	var out graphQLResponse[addClientData]
	return c.do(ctx, "CartSetClient", mutationSetClient, map[string]interface{}{
		"cartId":    cartID,
		"firstName": cl.FirstName,
		"lastName":  cl.LastName,
		"email":     cl.Email,
		"phone":     cl.Phone,
	}, &out)
}

func (c *BoulevardClient) checkout(ctx context.Context, cartID, notes string) (*BookingResult, error) {
	var out graphQLResponse[checkoutData]
	if err := c.do(ctx, "CartCheckout", mutationCheckout, map[string]interface{}{
		"cartId": cartID,
		"notes":  notes,
	}, &out); err != nil {
		return nil, err
	}
	return &BookingResult{
		BookingID: out.Data.CartCheckout.Booking.ID,
		CartID:    cartID,
		Status:    out.Data.CartCheckout.Booking.Status,
	}, nil
}

func (c *BoulevardClient) do(ctx context.Context, operationName, query string, variables interface{}, out interface{}) error {
	if strings.TrimSpace(c.apiKey) == "" {
		return fmt.Errorf("boulevard: missing api key")
	}
	if strings.TrimSpace(c.businessID) == "" {
		return fmt.Errorf("boulevard: missing business id")
	}

	body, err := json.Marshal(graphQLRequest{OperationName: operationName, Query: query, Variables: variables})
	if err != nil {
		return fmt.Errorf("boulevard: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("boulevard: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("X-Business-Id", c.businessID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("boulevard: http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("boulevard: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		msg := string(respBody)
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return fmt.Errorf("boulevard: status %d: %s", resp.StatusCode, msg)
	}

	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("boulevard: unmarshal response: %w", err)
	}

	// Best-effort error extraction from GraphQL envelope.
	if env, ok := out.(*graphQLResponse[servicesData]); ok && len(env.Errors) > 0 {
		return fmt.Errorf("boulevard: graphql error: %s", env.Errors[0].Message)
	}
	if env, ok := out.(*graphQLResponse[providersData]); ok && len(env.Errors) > 0 {
		return fmt.Errorf("boulevard: graphql error: %s", env.Errors[0].Message)
	}
	if env, ok := out.(*graphQLResponse[createCartData]); ok && len(env.Errors) > 0 {
		return fmt.Errorf("boulevard: graphql error: %s", env.Errors[0].Message)
	}
	if env, ok := out.(*graphQLResponse[addServiceData]); ok && len(env.Errors) > 0 {
		return fmt.Errorf("boulevard: graphql error: %s", env.Errors[0].Message)
	}
	if env, ok := out.(*graphQLResponse[availableSlotsData]); ok && len(env.Errors) > 0 {
		return fmt.Errorf("boulevard: graphql error: %s", env.Errors[0].Message)
	}
	if env, ok := out.(*graphQLResponse[reserveSlotData]); ok && len(env.Errors) > 0 {
		return fmt.Errorf("boulevard: graphql error: %s", env.Errors[0].Message)
	}
	if env, ok := out.(*graphQLResponse[addClientData]); ok && len(env.Errors) > 0 {
		return fmt.Errorf("boulevard: graphql error: %s", env.Errors[0].Message)
	}
	if env, ok := out.(*graphQLResponse[checkoutData]); ok && len(env.Errors) > 0 {
		return fmt.Errorf("boulevard: graphql error: %s", env.Errors[0].Message)
	}

	return nil
}
