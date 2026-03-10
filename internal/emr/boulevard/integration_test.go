//go:build integration

package boulevard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	bodyTonicBusinessID = "0062db5c-de83-410a-9f3a-443178b60854"
	bodyTonicLocationID = "120f1955-3b16-49a0-9a90-6097b102838d"
	blvdEndpoint        = "https://www.joinblvd.com/b/.api/graph"
)

// blvdDo executes a raw GraphQL request against the live Boulevard API.
func blvdDo(t *testing.T, query string, variables map[string]interface{}) json.RawMessage {
	t.Helper()
	payload := map[string]interface{}{"query": query}
	if variables != nil {
		payload["variables"] = variables
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", blvdEndpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-blvd-bid", bodyTonicBusinessID)

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("http request: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var gql struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(respBody, &gql); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(gql.Errors) > 0 {
		t.Fatalf("graphql error: %s", gql.Errors[0].Message)
	}
	return gql.Data
}

func TestIntegration_FullBookingFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_ = ctx

	// Step 1: CreateCart
	t.Log("Step 1: CreateCart")
	createCartQuery := fmt.Sprintf(`mutation {
		createCart(locationId: "%s") {
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
	}`, bodyTonicLocationID)

	data := blvdDo(t, createCartQuery, nil)

	var cartResult struct {
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
							Min json.RawMessage `json:"min"`
							Max json.RawMessage `json:"max"`
						} `json:"listPriceRange"`
					} `json:"availableItems"`
				} `json:"availableCategories"`
			} `json:"cart"`
		} `json:"createCart"`
	}
	if err := json.Unmarshal(data, &cartResult); err != nil {
		t.Fatalf("unmarshal cart: %v", err)
	}

	cartID := cartResult.CreateCart.Cart.ID
	if cartID == "" {
		t.Fatal("empty cart ID")
	}
	t.Logf("  Cart ID: %s", cartID)

	// Flatten services
	type svc struct {
		id, name string
		minRaw   json.RawMessage
	}
	var services []svc
	for _, cat := range cartResult.CreateCart.Cart.AvailableCategories {
		for _, item := range cat.AvailableItems {
			services = append(services, svc{id: item.ID, name: item.Name, minRaw: item.ListPriceRange.Min})
		}
	}
	if len(services) == 0 {
		t.Fatal("no services returned")
	}
	t.Logf("  Total services: %d", len(services))

	// Step 2: Find Botox
	t.Log("Step 2: Verify Botox exists")
	var botox *svc
	for i, s := range services {
		if strings.Contains(strings.ToLower(s.name), "botox") {
			botox = &services[i]
			break
		}
	}
	if botox == nil {
		names := make([]string, len(services))
		for i, s := range services {
			names[i] = s.name
		}
		t.Fatalf("Botox not found. Available: %v", names)
	}
	t.Logf("  Found: %s (ID: %s)", botox.name, botox.id)

	// Step 3: Verify Money scalar parsing — min/max should be parseable (int or string)
	t.Log("Step 3: Verify Money scalar fields")
	t.Logf("  Raw min value: %s", string(botox.minRaw))
	// Should parse as either a number or a string — not an object like {"amount":...}
	var numVal float64
	var strVal string
	if err := json.Unmarshal(botox.minRaw, &numVal); err == nil {
		t.Logf("  Parsed as number: %.0f", numVal)
	} else if err := json.Unmarshal(botox.minRaw, &strVal); err == nil {
		t.Logf("  Parsed as string: %s", strVal)
	} else {
		// Check it's not an object (the old broken format)
		var obj map[string]interface{}
		if json.Unmarshal(botox.minRaw, &obj) == nil {
			t.Fatalf("Money field is an object (broken): %v", obj)
		}
		t.Fatalf("Money field unparseable: %s", string(botox.minRaw))
	}

	// Step 4: AddSelectedItem
	t.Log("Step 4: AddSelectedItem (Botox)")
	addQuery := `mutation Add($input: CartAddSelectedBookableItemInput!) {
		cartAddSelectedBookableItem(input: $input) {
			cart { selectedItems { id item { name } } }
		}
	}`
	addData := blvdDo(t, addQuery, map[string]interface{}{
		"input": map[string]interface{}{
			"idOrToken": cartID,
			"itemId":    botox.id,
		},
	})

	var addResult struct {
		CartAddSelectedBookableItem struct {
			Cart struct {
				SelectedItems []struct {
					ID   string                `json:"id"`
					Item struct{ Name string } `json:"item"`
				} `json:"selectedItems"`
			} `json:"cart"`
		} `json:"cartAddSelectedBookableItem"`
	}
	if err := json.Unmarshal(addData, &addResult); err != nil {
		t.Fatalf("unmarshal add: %v", err)
	}
	items := addResult.CartAddSelectedBookableItem.Cart.SelectedItems
	if len(items) == 0 {
		t.Fatal("no selected items after add")
	}
	selectedItemID := items[len(items)-1].ID
	t.Logf("  Selected item ID: %s", selectedItemID)

	// Step 5: GetBookableDates
	t.Log("Step 5: GetBookableDates")
	datesQuery := fmt.Sprintf(`{ cartBookableDates(idOrToken: "%s", limit: 14, tz: "America/New_York") { date } }`, cartID)
	datesData := blvdDo(t, datesQuery, nil)

	var datesResult struct {
		CartBookableDates []struct {
			Date string `json:"date"`
		} `json:"cartBookableDates"`
	}
	if err := json.Unmarshal(datesData, &datesResult); err != nil {
		t.Fatalf("unmarshal dates: %v", err)
	}
	if len(datesResult.CartBookableDates) == 0 {
		t.Fatal("no bookable dates")
	}
	dates := make([]string, len(datesResult.CartBookableDates))
	for i, d := range datesResult.CartBookableDates {
		dates[i] = d.Date
	}
	t.Logf("  Bookable dates (%d): %v", len(dates), dates[:min(3, len(dates))])

	// Step 6: GetBookableTimes
	t.Log("Step 6: GetBookableTimes for first date")
	firstDate := dates[0]
	timesQuery := fmt.Sprintf(`{ cartBookableTimes(idOrToken: "%s", searchDate: "%s", tz: "America/New_York") { id startTime } }`, cartID, firstDate)
	timesData := blvdDo(t, timesQuery, nil)

	var timesResult struct {
		CartBookableTimes []struct {
			ID        string `json:"id"`
			StartTime string `json:"startTime"`
		} `json:"cartBookableTimes"`
	}
	if err := json.Unmarshal(timesData, &timesResult); err != nil {
		t.Fatalf("unmarshal times: %v", err)
	}
	if len(timesResult.CartBookableTimes) == 0 {
		t.Fatalf("no bookable times for %s", firstDate)
	}
	t.Logf("  Time slots for %s: %d", firstDate, len(timesResult.CartBookableTimes))
	for i, slot := range timesResult.CartBookableTimes {
		if i >= 3 {
			t.Logf("    ... and %d more", len(timesResult.CartBookableTimes)-3)
			break
		}
		t.Logf("    %s @ %s", slot.ID, slot.StartTime)
	}

	t.Log("✅ Full Boulevard booking flow passed")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
