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

	// --- GraphQL queries matching Boulevard's public booking widget API ---

	mutationCreateCart = `mutation CreateCart($locationId: ID!) {
  createCart(input: { locationId: $locationId }) {
    cart {
      id
      token
      availableCategories {
        id
        name
        availableItems {
          id
          name
          description
        }
      }
    }
  }
}`

	mutationAddSelectedItem = `mutation CartAddSelectedBookableItem($input: CartAddSelectedBookableItemInput!) {
  cartAddSelectedBookableItem(input: $input) {
    cart {
      id
      selectedItems {
        id
        selectedItem {
          ... on CartAvailableBookableItem {
            id
            name
          }
        }
      }
    }
  }
}`

	queryBookableDates = `query CartBookableDates($idOrToken: String!, $limit: Int, $tz: Tz) {
  cartBookableDates(idOrToken: $idOrToken, limit: $limit, tz: $tz) {
    date
  }
}`

	queryBookableTimes = `query CartBookableTimes($idOrToken: String!, $searchDate: Date!, $tz: Tz) {
  cartBookableTimes(idOrToken: $idOrToken, searchDate: $searchDate, tz: $tz) {
    id
    startTime
  }
}`

	queryStaffVariants = `query CartBookableStaffVariants($idOrToken: String!, $bookableTimeId: ID!, $itemId: ID!) {
  cartBookableStaffVariants(idOrToken: $idOrToken, bookableTimeId: $bookableTimeId, itemId: $itemId) {
    staffVariants {
      id
      staff {
        id
        firstName
        lastName
      }
    }
  }
}`

	mutationReserveItems = `mutation ReserveCartBookableItems($input: ReserveCartBookableItemsInput!) {
  reserveCartBookableItems(input: $input) {
    cart {
      id
    }
  }
}`

	mutationUpdateCart = `mutation UpdateCart($input: UpdateCartInput!) {
  updateCart(input: $input) {
    cart {
      id
    }
  }
}`

	mutationCheckoutCart = `mutation CheckoutCart($input: CheckoutCartInput!) {
  checkoutCart(input: $input) {
    appointments {
      id
      startAt
      appointmentServiceOptions {
        id
      }
    }
  }
}`
)

// BoulevardClient is a lightweight GraphQL client for Boulevard's public booking widget API.
// It uses the x-blvd-bid header for authentication — no API key required.
type BoulevardClient struct {
	endpoint   string
	httpClient *http.Client
	businessID string // used as x-blvd-bid header
	locationID string // used for cart creation
	dryRun     bool
	logger     *logging.Logger
}

// NewBoulevardClient creates a new Boulevard public API client.
// businessID is used as the x-blvd-bid header value.
// locationID is used when creating carts.
func NewBoulevardClient(businessID, locationID string, logger *logging.Logger) *BoulevardClient {
	if logger == nil {
		logger = logging.Default()
	}
	return &BoulevardClient{
		endpoint: publicGraphQLEndpoint,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		businessID: businessID,
		locationID: locationID,
		logger:     logger,
	}
}

// SetDryRun enables or disables dry-run mode.
func (c *BoulevardClient) SetDryRun(dryRun bool) {
	c.dryRun = dryRun
}

// IsDryRun returns whether the client is in dry-run mode.
func (c *BoulevardClient) IsDryRun() bool {
	return c.dryRun
}

// CreateCart creates a new booking cart and returns it with available service categories.
func (c *BoulevardClient) CreateCart(ctx context.Context) (*Cart, error) {
	var out graphQLResponse[createCartData]
	if err := c.do(ctx, "CreateCart", mutationCreateCart, map[string]interface{}{
		"locationId": c.locationID,
	}, &out); err != nil {
		return nil, err
	}
	if out.Data.CreateCart.Cart.ID == "" {
		return nil, fmt.Errorf("boulevard: create cart returned empty cart id")
	}

	cart := &Cart{
		ID:    out.Data.CreateCart.Cart.ID,
		Token: out.Data.CreateCart.Cart.Token,
	}

	for _, cat := range out.Data.CreateCart.Cart.AvailableCategories {
		sc := ServiceCategory{ID: cat.ID, Name: cat.Name}
		for _, item := range cat.AvailableItems {
			sc.Services = append(sc.Services, Service{
				ID:          item.ID,
				Name:        item.Name,
				Description: item.Description,
				CategoryID:  cat.ID,
			})
		}
		cart.AvailableCategories = append(cart.AvailableCategories, sc)
	}

	return cart, nil
}

// GetServices creates a cart and returns all available services from the categories.
func (c *BoulevardClient) GetServices(ctx context.Context) ([]Service, error) {
	cart, err := c.CreateCart(ctx)
	if err != nil {
		return nil, fmt.Errorf("boulevard: get services: %w", err)
	}
	var services []Service
	for _, cat := range cart.AvailableCategories {
		services = append(services, cat.Services...)
	}
	return services, nil
}

// AddSelectedItem adds a bookable item to the cart.
// Returns the selected item ID (different from the catalog item ID).
func (c *BoulevardClient) AddSelectedItem(ctx context.Context, idOrToken, itemID string) (string, error) {
	var out graphQLResponse[addSelectedItemData]
	if err := c.do(ctx, "CartAddSelectedBookableItem", mutationAddSelectedItem, map[string]interface{}{
		"input": map[string]interface{}{
			"idOrToken": idOrToken,
			"itemId":    itemID,
		},
	}, &out); err != nil {
		return "", err
	}
	items := out.Data.CartAddSelectedBookableItem.Cart.SelectedItems
	if len(items) == 0 {
		return "", fmt.Errorf("boulevard: add item returned no selected items")
	}
	// Return the last selected item's ID
	return items[len(items)-1].ID, nil
}

// GetBookableDates returns available dates for the cart.
func (c *BoulevardClient) GetBookableDates(ctx context.Context, idOrToken string, limit int, tz string) ([]BookableDate, error) {
	vars := map[string]interface{}{
		"idOrToken": idOrToken,
	}
	if limit > 0 {
		vars["limit"] = limit
	}
	if tz != "" {
		vars["tz"] = tz
	}
	var out graphQLResponse[bookableDatesData]
	if err := c.do(ctx, "CartBookableDates", queryBookableDates, vars, &out); err != nil {
		return nil, err
	}
	dates := make([]BookableDate, 0, len(out.Data.CartBookableDates))
	for _, d := range out.Data.CartBookableDates {
		dates = append(dates, BookableDate{Date: d.Date})
	}
	return dates, nil
}

// GetBookableTimes returns available time slots for a specific date.
func (c *BoulevardClient) GetBookableTimes(ctx context.Context, idOrToken, searchDate, tz string) ([]BookableTime, error) {
	vars := map[string]interface{}{
		"idOrToken":  idOrToken,
		"searchDate": searchDate,
	}
	if tz != "" {
		vars["tz"] = tz
	}
	var out graphQLResponse[bookableTimesData]
	if err := c.do(ctx, "CartBookableTimes", queryBookableTimes, vars, &out); err != nil {
		return nil, err
	}
	times := make([]BookableTime, 0, len(out.Data.CartBookableTimes))
	for _, t := range out.Data.CartBookableTimes {
		times = append(times, BookableTime{ID: t.ID, StartTime: t.StartTime})
	}
	return times, nil
}

// GetStaffVariants returns providers available for a specific time slot and item.
// itemID must be the SELECTED item ID from AddSelectedItem, not the catalog ID.
func (c *BoulevardClient) GetStaffVariants(ctx context.Context, idOrToken, bookableTimeID, itemID string) ([]StaffVariant, error) {
	var out graphQLResponse[staffVariantsData]
	if err := c.do(ctx, "CartBookableStaffVariants", queryStaffVariants, map[string]interface{}{
		"idOrToken":      idOrToken,
		"bookableTimeId": bookableTimeID,
		"itemId":         itemID,
	}, &out); err != nil {
		return nil, err
	}
	variants := make([]StaffVariant, 0, len(out.Data.CartBookableStaffVariants.StaffVariants))
	for _, v := range out.Data.CartBookableStaffVariants.StaffVariants {
		sv := StaffVariant{ID: v.ID}
		sv.Staff.ID = v.Staff.ID
		sv.Staff.FirstName = v.Staff.FirstName
		sv.Staff.LastName = v.Staff.LastName
		variants = append(variants, sv)
	}
	return variants, nil
}

// ReserveSlot reserves a bookable time slot in the cart.
// In dry-run mode, logs but returns a fake success.
func (c *BoulevardClient) ReserveSlot(ctx context.Context, idOrToken, bookableTimeID string) error {
	if c.dryRun {
		c.logger.Info("BOULEVARD DRY RUN: ReserveSlot (skipped)",
			"idOrToken", idOrToken, "bookableTimeId", bookableTimeID)
		return nil
	}
	var out graphQLResponse[reserveItemsData]
	return c.do(ctx, "ReserveCartBookableItems", mutationReserveItems, map[string]interface{}{
		"input": map[string]interface{}{
			"idOrToken":      idOrToken,
			"bookableTimeId": bookableTimeID,
		},
	}, &out)
}

// UpdateCartClient sets client information on the cart.
// In dry-run mode, logs but returns a fake success.
func (c *BoulevardClient) UpdateCartClient(ctx context.Context, idOrToken string, cl Client) error {
	if c.dryRun {
		c.logger.Info("BOULEVARD DRY RUN: UpdateCartClient (skipped)",
			"idOrToken", idOrToken, "client", cl.FirstName+" "+cl.LastName)
		return nil
	}
	var out graphQLResponse[updateCartData]
	return c.do(ctx, "UpdateCart", mutationUpdateCart, map[string]interface{}{
		"input": map[string]interface{}{
			"idOrToken": idOrToken,
			"clientInformation": map[string]interface{}{
				"firstName":   cl.FirstName,
				"lastName":    cl.LastName,
				"email":       cl.Email,
				"phoneNumber": cl.Phone,
			},
		},
	}, &out)
}

// Checkout completes the booking.
// In dry-run mode, logs but returns a fake success.
func (c *BoulevardClient) Checkout(ctx context.Context, idOrToken string) (*BookingResult, error) {
	if c.dryRun {
		c.logger.Info("BOULEVARD DRY RUN: Checkout (skipped)", "idOrToken", idOrToken)
		return &BookingResult{
			BookingID: "dry-run-" + time.Now().Format("20060102150405"),
			CartID:    idOrToken,
			Status:    "DRY_RUN",
		}, nil
	}
	var out graphQLResponse[checkoutCartData]
	if err := c.do(ctx, "CheckoutCart", mutationCheckoutCart, map[string]interface{}{
		"input": map[string]interface{}{
			"idOrToken": idOrToken,
		},
	}, &out); err != nil {
		return nil, err
	}
	result := &BookingResult{
		CartID: idOrToken,
		Status: "confirmed",
	}
	if len(out.Data.CheckoutCart.Appointments) > 0 {
		result.BookingID = out.Data.CheckoutCart.Appointments[0].ID
	}
	return result, nil
}

// GetAvailableSlots performs the full cart-based availability lookup:
// createCart → addItem → getBookableDates → getBookableTimes.
// This is a convenience method that combines multiple API calls.
func (c *BoulevardClient) GetAvailableSlots(ctx context.Context, serviceID, providerID string, date time.Time) ([]TimeSlot, error) {
	cart, err := c.CreateCart(ctx)
	if err != nil {
		return nil, fmt.Errorf("boulevard: create cart for availability: %w", err)
	}

	idOrToken := cart.ID

	selectedItemID, err := c.AddSelectedItem(ctx, idOrToken, serviceID)
	if err != nil {
		return nil, fmt.Errorf("boulevard: add item for availability: %w", err)
	}
	_ = selectedItemID // used for staff variants if needed

	tz := "America/New_York" // default timezone
	times, err := c.GetBookableTimes(ctx, idOrToken, date.Format("2006-01-02"), tz)
	if err != nil {
		return nil, fmt.Errorf("boulevard: get bookable times: %w", err)
	}

	slots := make([]TimeSlot, 0, len(times))
	for _, t := range times {
		startTime, parseErr := time.Parse(time.RFC3339, t.StartTime)
		if parseErr != nil {
			// Try ISO 8601 without timezone
			startTime, parseErr = time.Parse("2006-01-02T15:04:05", t.StartTime)
			if parseErr != nil {
				c.logger.Warn("boulevard: failed to parse time", "startTime", t.StartTime, "error", parseErr)
				continue
			}
		}
		slots = append(slots, TimeSlot{
			ID:      t.ID,
			StartAt: startTime,
			EndAt:   startTime.Add(60 * time.Minute), // default 1hr; actual duration from service
		})
	}
	return slots, nil
}

// CreateBooking executes the full Boulevard cart flow:
// createCart → addItem → reserve → setClient → checkout.
func (c *BoulevardClient) CreateBooking(ctx context.Context, req CreateBookingRequest) (*BookingResult, error) {
	cart, err := c.CreateCart(ctx)
	if err != nil {
		return nil, err
	}
	idOrToken := cart.ID

	_, err = c.AddSelectedItem(ctx, idOrToken, req.ServiceID)
	if err != nil {
		return nil, err
	}

	// Get times for the requested date to find the matching bookableTimeId
	tz := "America/New_York"
	times, err := c.GetBookableTimes(ctx, idOrToken, req.StartAt.Format("2006-01-02"), tz)
	if err != nil {
		return nil, fmt.Errorf("boulevard: get times for booking: %w", err)
	}

	// Find matching time slot
	var bookableTimeID string
	for _, t := range times {
		startTime, parseErr := time.Parse(time.RFC3339, t.StartTime)
		if parseErr != nil {
			startTime, parseErr = time.Parse("2006-01-02T15:04:05", t.StartTime)
			if parseErr != nil {
				continue
			}
		}
		if startTime.Equal(req.StartAt) || startTime.Unix() == req.StartAt.Unix() {
			bookableTimeID = t.ID
			break
		}
	}
	if bookableTimeID == "" {
		return nil, fmt.Errorf("boulevard: no matching time slot found for %s", req.StartAt.Format(time.RFC3339))
	}

	if err := c.ReserveSlot(ctx, idOrToken, bookableTimeID); err != nil {
		return nil, err
	}
	if err := c.UpdateCartClient(ctx, idOrToken, req.Client); err != nil {
		return nil, err
	}
	return c.Checkout(ctx, idOrToken)
}

// GetProviders creates a cart, adds the service, gets dates/times, then gets staff variants.
// This is a convenience method to list providers for a service.
func (c *BoulevardClient) GetProviders(ctx context.Context, serviceID string) ([]Provider, error) {
	cart, err := c.CreateCart(ctx)
	if err != nil {
		return nil, err
	}
	idOrToken := cart.ID

	selectedItemID, err := c.AddSelectedItem(ctx, idOrToken, serviceID)
	if err != nil {
		return nil, err
	}

	tz := "America/New_York"
	dates, err := c.GetBookableDates(ctx, idOrToken, 1, tz)
	if err != nil || len(dates) == 0 {
		return nil, fmt.Errorf("boulevard: no available dates for service %s", serviceID)
	}

	times, err := c.GetBookableTimes(ctx, idOrToken, dates[0].Date, tz)
	if err != nil || len(times) == 0 {
		return nil, fmt.Errorf("boulevard: no available times for service %s on %s", serviceID, dates[0].Date)
	}

	variants, err := c.GetStaffVariants(ctx, idOrToken, times[0].ID, selectedItemID)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var providers []Provider
	for _, v := range variants {
		if !seen[v.Staff.ID] {
			seen[v.Staff.ID] = true
			providers = append(providers, Provider{
				ID:   v.Staff.ID,
				Name: strings.TrimSpace(v.Staff.FirstName + " " + v.Staff.LastName),
			})
		}
	}
	return providers, nil
}

func (c *BoulevardClient) do(ctx context.Context, operationName, query string, variables interface{}, out interface{}) error {
	if strings.TrimSpace(c.businessID) == "" {
		return fmt.Errorf("boulevard: missing business id (x-blvd-bid)")
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
	req.Header.Set("x-blvd-bid", c.businessID)

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

	// Check for GraphQL errors in the response envelope
	var envelope struct {
		Errors []graphQLError `json:"errors"`
	}
	if err := json.Unmarshal(respBody, &envelope); err == nil && len(envelope.Errors) > 0 {
		return fmt.Errorf("boulevard: graphql error: %s", envelope.Errors[0].Message)
	}

	return nil
}
