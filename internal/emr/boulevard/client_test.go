package boulevard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCreateCart(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify x-blvd-bid header
		if got := r.Header.Get("x-blvd-bid"); got != "biz123" {
			t.Errorf("x-blvd-bid = %q, want %q", got, "biz123")
		}
		// Verify NO Authorization header
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("unexpected Authorization header: %s", auth)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"createCart": map[string]any{
					"cart": map[string]any{
						"id": "cart_1",
						"availableCategories": []map[string]any{
							{
								"name": "Injectables",
								"availableItems": []map[string]any{
									{"id": "s_1", "name": "Botox", "description": "Wrinkle relaxer", "listPriceRange": map[string]any{"min": map[string]any{"amount": 20000}, "max": map[string]any{"amount": 20000}}},
								},
							},
						},
					},
				},
			},
		})
	}))
	defer ts.Close()

	c := NewBoulevardClient("biz123", "loc456", nil)
	c.endpoint = ts.URL

	cartID, services, err := c.CreateCart(context.Background())
	if err != nil {
		t.Fatalf("CreateCart error: %v", err)
	}
	if cartID != "cart_1" {
		t.Fatalf("cartID = %q, want cart_1", cartID)
	}
	if len(services) != 1 || services[0].Name != "Botox" {
		t.Fatalf("unexpected services: %+v", services)
	}
	if services[0].PriceCents != 20000 {
		t.Fatalf("price = %d, want 20000", services[0].PriceCents)
	}
}

func TestAddSelectedItem(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"cartAddSelectedBookableItem": map[string]any{
					"cart": map[string]any{
						"selectedItems": []map[string]any{
							{"id": "sel_1", "item": map[string]any{"name": "Botox"}},
						},
					},
				},
			},
		})
	}))
	defer ts.Close()

	c := NewBoulevardClient("biz", "loc", nil)
	c.endpoint = ts.URL

	selID, err := c.AddSelectedItem(context.Background(), "cart_1", "s_1")
	if err != nil {
		t.Fatalf("AddSelectedItem error: %v", err)
	}
	if selID != "sel_1" {
		t.Fatalf("selectedItemID = %q, want sel_1", selID)
	}
}

func TestGetBookableTimes(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"cartBookableTimes": []map[string]any{
					{"id": "t_2026-03-11T11:30:00", "startTime": "2026-03-11T11:30:00-04:00"},
					{"id": "t_2026-03-11T14:00:00", "startTime": "2026-03-11T14:00:00-04:00"},
				},
			},
		})
	}))
	defer ts.Close()

	c := NewBoulevardClient("biz", "loc", nil)
	c.endpoint = ts.URL

	slots, err := c.GetBookableTimes(context.Background(), "cart_1", "2026-03-11", "America/New_York")
	if err != nil {
		t.Fatalf("GetBookableTimes error: %v", err)
	}
	if len(slots) != 2 {
		t.Fatalf("got %d slots, want 2", len(slots))
	}
	if slots[0].ID != "t_2026-03-11T11:30:00" {
		t.Fatalf("slot[0].ID = %q", slots[0].ID)
	}
}

func TestGraphQLError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]any{{"message": "boom"}}})
	}))
	defer ts.Close()

	c := NewBoulevardClient("biz", "loc", nil)
	c.endpoint = ts.URL
	_, _, err := c.CreateCart(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDryRunAdapter(t *testing.T) {
	adapter := NewBoulevardAdapter(nil, true, nil)
	if !adapter.IsDryRun() {
		t.Fatal("expected dry run")
	}
	// With nil client, ResolveAvailability should fail (no mock data anymore)
	_, err := adapter.ResolveAvailability(context.Background(), "Botox", "", time.Now())
	if err == nil {
		t.Fatal("expected error with nil client")
	}
}
