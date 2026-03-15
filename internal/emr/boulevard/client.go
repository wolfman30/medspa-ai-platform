package boulevard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// parseMoneyScalar converts a Boulevard Money scalar string (e.g. "200.00") to cents.
// Returns 0 if the string is empty or unparsable.
func parseMoneyScalar(s string) int {
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int(math.Round(f * 100))
}

const defaultTimeout = 20 * time.Second

// BoulevardClient is a GraphQL client for Boulevard's public booking widget API.
// No API key required — uses x-blvd-bid header with the business ID.
type BoulevardClient struct {
	endpoint   string
	httpClient *http.Client
	businessID string
	locationID string
	logger     *logging.Logger
}

// NewBoulevardClient creates a new Boulevard public API client.
// businessID and locationID come from the clinic config (extracted from the booking widget).
func NewBoulevardClient(businessID, locationID string, logger *logging.Logger) *BoulevardClient {
	if logger == nil {
		logger = logging.Default()
	}
	return &BoulevardClient{
		endpoint:   publicGraphQLEndpoint,
		httpClient: &http.Client{Timeout: defaultTimeout},
		businessID: businessID,
		locationID: locationID,
		logger:     logger,
	}
}

// CreateCart creates a new booking cart and returns the cart ID plus all available services.
func (c *BoulevardClient) CreateCart(ctx context.Context) (cartID string, services []Service, err error) {
	query := `mutation CreateCart($loc: ID) {
  createCart(locationId: $loc) {
    cart {
      id
      availableCategories {
        name
        availableItems {
          id name description
          listPriceRange { min max }
        }
      }
    }
  }
}`
	raw, err := c.do(ctx, query, map[string]interface{}{"loc": c.locationID})
	if err != nil {
		return "", nil, fmt.Errorf("createCart: %w", err)
	}

	var result struct {
		CreateCart struct {
			Cart struct {
				ID                  string `json:"id"`
				AvailableCategories []struct {
					Name           string `json:"name"`
					AvailableItems []struct {
						ID             string `json:"id"`
						Name           string `json:"name"`
						Description    string `json:"description"`
						ListPriceRange struct {
							Min interface{} `json:"min"`
							Max interface{} `json:"max"`
						} `json:"listPriceRange"`
					} `json:"availableItems"`
				} `json:"availableCategories"`
			} `json:"cart"`
		} `json:"createCart"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", nil, fmt.Errorf("createCart unmarshal: %w", err)
	}

	cartID = result.CreateCart.Cart.ID
	if cartID == "" {
		return "", nil, fmt.Errorf("createCart: empty cart ID")
	}

	for _, cat := range result.CreateCart.Cart.AvailableCategories {
		for _, item := range cat.AvailableItems {
			services = append(services, Service{
				ID:          item.ID,
				Name:        item.Name,
				Description: item.Description,
				PriceCents:  parseMoneyScalar(fmt.Sprintf("%v", item.ListPriceRange.Min)),
			})
		}
	}
	return cartID, services, nil
}

// AddSelectedItem adds a service to the cart and returns the selected item ID.
func (c *BoulevardClient) AddSelectedItem(ctx context.Context, cartID, itemID string) (selectedItemID string, err error) {
	query := `mutation Add($input: CartAddSelectedBookableItemInput!) {
  cartAddSelectedBookableItem(input: $input) {
    cart { selectedItems { id item { name } } }
  }
}`
	raw, err := c.do(ctx, query, map[string]interface{}{
		"input": map[string]interface{}{
			"idOrToken": cartID,
			"itemId":    itemID,
		},
	})
	if err != nil {
		return "", fmt.Errorf("addSelectedItem: %w", err)
	}

	var result struct {
		CartAddSelectedBookableItem struct {
			Cart struct {
				SelectedItems []struct {
					ID   string                `json:"id"`
					Item struct{ Name string } `json:"item"`
				} `json:"selectedItems"`
			} `json:"cart"`
		} `json:"cartAddSelectedBookableItem"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("addSelectedItem unmarshal: %w", err)
	}

	items := result.CartAddSelectedBookableItem.Cart.SelectedItems
	if len(items) == 0 {
		return "", fmt.Errorf("addSelectedItem: no items in cart after add")
	}
	return items[len(items)-1].ID, nil
}

// GetBookableDates returns available dates for the current cart contents.
func (c *BoulevardClient) GetBookableDates(ctx context.Context, cartID string, limit int, tz string) ([]string, error) {
	if limit <= 0 {
		limit = 14
	}
	if tz == "" {
		tz = "America/New_York"
	}
	query := `{ cartBookableDates(idOrToken: $id, limit: $limit, tz: $tz) { date } }`
	// Boulevard doesn't support $ variables for this query — inline them
	query = fmt.Sprintf(`{ cartBookableDates(idOrToken: "%s", limit: %d, tz: "%s") { date } }`, cartID, limit, tz)

	raw, err := c.do(ctx, query, nil)
	if err != nil {
		return nil, fmt.Errorf("getBookableDates: %w", err)
	}

	var result struct {
		CartBookableDates []struct {
			Date string `json:"date"`
		} `json:"cartBookableDates"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("getBookableDates unmarshal: %w", err)
	}

	dates := make([]string, 0, len(result.CartBookableDates))
	for _, d := range result.CartBookableDates {
		dates = append(dates, d.Date)
	}
	return dates, nil
}

// GetBookableTimes returns available time slots for a given date.
func (c *BoulevardClient) GetBookableTimes(ctx context.Context, cartID, date, tz string) ([]TimeSlot, error) {
	if tz == "" {
		tz = "America/New_York"
	}
	query := fmt.Sprintf(`{ cartBookableTimes(idOrToken: "%s", searchDate: "%s", tz: "%s") { id startTime } }`, cartID, date, tz)

	raw, err := c.do(ctx, query, nil)
	if err != nil {
		return nil, fmt.Errorf("getBookableTimes: %w", err)
	}

	var result struct {
		CartBookableTimes []struct {
			ID        string `json:"id"`
			StartTime string `json:"startTime"`
		} `json:"cartBookableTimes"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("getBookableTimes unmarshal: %w", err)
	}

	slots := make([]TimeSlot, 0, len(result.CartBookableTimes))
	for _, t := range result.CartBookableTimes {
		parsed, err := time.Parse(time.RFC3339, t.StartTime)
		if err != nil {
			c.logger.Warn("boulevard: could not parse time", "raw", t.StartTime, "err", err)
			continue
		}
		slots = append(slots, TimeSlot{ID: t.ID, StartAt: parsed})
	}
	return slots, nil
}

// GetStaffVariants returns providers available for a time slot + selected item.
func (c *BoulevardClient) GetStaffVariants(ctx context.Context, cartID, bookableTimeID, selectedItemID string) ([]StaffVariant, error) {
	query := fmt.Sprintf(`{ cartBookableStaffVariants(idOrToken: "%s", bookableTimeId: "%s", itemId: "%s") { id staff { firstName lastName } } }`,
		cartID, bookableTimeID, selectedItemID)

	raw, err := c.do(ctx, query, nil)
	if err != nil {
		return nil, fmt.Errorf("getStaffVariants: %w", err)
	}

	var result struct {
		CartBookableStaffVariants []struct {
			ID    string `json:"id"`
			Staff struct {
				FirstName string `json:"firstName"`
				LastName  string `json:"lastName"`
			} `json:"staff"`
		} `json:"cartBookableStaffVariants"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("getStaffVariants unmarshal: %w", err)
	}

	variants := make([]StaffVariant, 0, len(result.CartBookableStaffVariants))
	for _, v := range result.CartBookableStaffVariants {
		variants = append(variants, StaffVariant{
			ID:        v.ID,
			FirstName: v.Staff.FirstName,
			LastName:  v.Staff.LastName,
		})
	}
	return variants, nil
}

// ReserveSlot holds a time slot in the cart.
func (c *BoulevardClient) ReserveSlot(ctx context.Context, cartID, bookableTimeID string) error {
	query := `mutation Reserve($cartId: ID!, $timeId: ID!) {
  reserveCartBookableItems(idOrToken: $cartId, bookableTimeId: $timeId) { cart { id } }
}`
	_, err := c.do(ctx, query, map[string]interface{}{
		"cartId": cartID,
		"timeId": bookableTimeID,
	})
	if err != nil {
		return fmt.Errorf("reserveSlot: %w", err)
	}
	return nil
}

// GetAvailableSlots is a convenience method that performs the full cart flow:
// CreateCart → find matching service → AddSelectedItem → GetBookableDates → GetBookableTimes.
// Returns real time slots from Boulevard. serviceName is fuzzy-matched against the catalog.
func (c *BoulevardClient) GetAvailableSlots(ctx context.Context, serviceName, providerName, tz string) ([]TimeSlot, string, error) {
	cartID, services, err := c.CreateCart(ctx)
	if err != nil {
		return nil, "", err
	}

	// Fuzzy match service name
	serviceID := ""
	for _, svc := range services {
		if strings.EqualFold(svc.Name, serviceName) {
			serviceID = svc.ID
			break
		}
	}
	if serviceID == "" {
		nameLower := strings.ToLower(serviceName)
		for _, svc := range services {
			svcLower := strings.ToLower(svc.Name)
			if strings.Contains(svcLower, nameLower) || strings.Contains(nameLower, svcLower) {
				serviceID = svc.ID
				break
			}
			// Match against slash-separated parts (e.g. "botox" matches "Botox/Dysport/Xeomin")
			for _, part := range strings.Split(svcLower, "/") {
				part = strings.TrimSpace(part)
				if part != "" && (strings.Contains(part, nameLower) || strings.Contains(nameLower, part)) {
					serviceID = svc.ID
					break
				}
			}
			if serviceID != "" {
				break
			}
		}
	}
	if serviceID == "" {
		return nil, cartID, fmt.Errorf("boulevard: no matching service for %q", serviceName)
	}

	selectedItemID, err := c.AddSelectedItem(ctx, cartID, serviceID)
	if err != nil {
		return nil, cartID, err
	}

	dates, err := c.GetBookableDates(ctx, cartID, 30, tz)
	if err != nil {
		return nil, cartID, err
	}
	if len(dates) == 0 {
		return nil, cartID, nil // no availability
	}

	// Collect slots from available dates (up to 10 dates for broader coverage)
	var allSlots []TimeSlot
	maxDates := 10
	if len(dates) < maxDates {
		maxDates = len(dates)
	}
	for _, date := range dates[:maxDates] {
		times, err := c.GetBookableTimes(ctx, cartID, date, tz)
		if err != nil {
			c.logger.Warn("boulevard: error getting times for date", "date", date, "err", err)
			continue
		}
		allSlots = append(allSlots, times...)
	}

	// If provider preference specified, filter by staff variants
	if providerName != "" && len(allSlots) > 0 {
		provLower := strings.ToLower(providerName)
		var filtered []TimeSlot
		for _, slot := range allSlots {
			variants, err := c.GetStaffVariants(ctx, cartID, slot.ID, selectedItemID)
			if err != nil {
				continue
			}
			for _, v := range variants {
				fullName := strings.ToLower(v.FirstName + " " + v.LastName)
				if strings.Contains(fullName, provLower) || strings.Contains(provLower, strings.ToLower(v.FirstName)) {
					filtered = append(filtered, slot)
					break
				}
			}
		}
		if len(filtered) > 0 {
			allSlots = filtered
		}
	}

	return allSlots, cartID, nil
}

// do executes a GraphQL request against the public Boulevard widget API.
func (c *BoulevardClient) do(ctx context.Context, query string, variables interface{}) (json.RawMessage, error) {
	if c.businessID == "" {
		return nil, fmt.Errorf("boulevard: missing business ID")
	}

	payload := graphQLRequest{Query: query, Variables: variables}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("boulevard: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("boulevard: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-blvd-bid", c.businessID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("boulevard: http: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("boulevard: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		msg := string(respBody)
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, fmt.Errorf("boulevard: status %d: %s", resp.StatusCode, msg)
	}

	var gqlResp graphQLResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return nil, fmt.Errorf("boulevard: unmarshal: %w", err)
	}
	if len(gqlResp.Errors) > 0 {
		return nil, fmt.Errorf("boulevard: graphql: %s", gqlResp.Errors[0].Message)
	}

	return gqlResp.Data, nil
}
