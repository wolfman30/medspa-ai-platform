package boulevard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetServices(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify x-blvd-bid header
		if got := r.Header.Get("x-blvd-bid"); got != "biz123" {
			t.Errorf("expected x-blvd-bid=biz123, got=%s", got)
		}
		// Verify NO Authorization header
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("expected no Authorization header, got=%s", auth)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"createCart": map[string]any{
					"cart": map[string]any{
						"id":    "cart_1",
						"token": "tok_1",
						"availableCategories": []map[string]any{
							{
								"id":   "cat_1",
								"name": "Injectables",
								"availableItems": []map[string]any{
									{"id": "svc_1", "name": "Botox", "description": "wrinkle treatment"},
								},
							},
						},
					},
				},
			},
		})
	}))
	defer ts.Close()

	c := NewBoulevardClient("biz123", "loc123", nil)
	c.endpoint = ts.URL

	services, err := c.GetServices(context.Background())
	if err != nil {
		t.Fatalf("GetServices error: %v", err)
	}
	if len(services) != 1 || services[0].ID != "svc_1" || services[0].Name != "Botox" {
		t.Fatalf("unexpected services: %+v", services)
	}
}

func TestCreateCart(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		vars, _ := req["variables"].(map[string]any)
		if vars["locationId"] != "loc123" {
			t.Errorf("expected locationId=loc123, got=%v", vars["locationId"])
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"createCart": map[string]any{
					"cart": map[string]any{
						"id":                  "cart_1",
						"token":               "tok_1",
						"availableCategories": []any{},
					},
				},
			},
		})
	}))
	defer ts.Close()

	c := NewBoulevardClient("biz123", "loc123", nil)
	c.endpoint = ts.URL

	cart, err := c.CreateCart(context.Background())
	if err != nil {
		t.Fatalf("CreateCart error: %v", err)
	}
	if cart.ID != "cart_1" || cart.Token != "tok_1" {
		t.Fatalf("unexpected cart: %+v", cart)
	}
}

func TestDryRunReserveAndCheckout(t *testing.T) {
	c := NewBoulevardClient("biz", "loc", nil)
	c.SetDryRun(true)

	// Reserve should succeed without hitting any server
	if err := c.ReserveSlot(context.Background(), "cart_1", "time_1"); err != nil {
		t.Fatalf("ReserveSlot dry-run error: %v", err)
	}

	// Checkout should return dry-run result
	result, err := c.Checkout(context.Background(), "cart_1")
	if err != nil {
		t.Fatalf("Checkout dry-run error: %v", err)
	}
	if result.Status != "DRY_RUN" {
		t.Fatalf("expected DRY_RUN status, got=%s", result.Status)
	}
}

func TestCreateBooking_FullCartFlow(t *testing.T) {
	ops := make([]string, 0)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		op, _ := req["operationName"].(string)
		ops = append(ops, op)

		switch op {
		case "CreateCart":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"createCart": map[string]any{"cart": map[string]any{
					"id": "cart_1", "token": "tok_1", "availableCategories": []any{},
				}},
			}})
		case "CartAddSelectedBookableItem":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"cartAddSelectedBookableItem": map[string]any{"cart": map[string]any{
					"id":            "cart_1",
					"selectedItems": []any{map[string]any{"id": "sel_1"}},
				}},
			}})
		case "CartBookableTimes":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"cartBookableTimes": []any{
					map[string]any{"id": "time_1", "startTime": "2026-02-21T15:00:00Z"},
				},
			}})
		case "ReserveCartBookableItems":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"reserveCartBookableItems": map[string]any{"cart": map[string]any{"id": "cart_1"}},
			}})
		case "UpdateCart":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"updateCart": map[string]any{"cart": map[string]any{"id": "cart_1"}},
			}})
		case "CheckoutCart":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"checkoutCart": map[string]any{"appointments": []any{
					map[string]any{"id": "appt_1", "startAt": "2026-02-21T15:00:00Z"},
				}},
			}})
		default:
			http.Error(w, "unknown op: "+op, http.StatusBadRequest)
		}
	}))
	defer ts.Close()

	c := NewBoulevardClient("biz", "loc", nil)
	c.endpoint = ts.URL

	res, err := c.CreateBooking(context.Background(), CreateBookingRequest{
		ServiceID: "svc_1",
		StartAt:   time.Date(2026, 2, 21, 15, 0, 0, 0, time.UTC),
		Client:    Client{FirstName: "Jane", LastName: "Doe", Email: "jane@example.com", Phone: "+15555550123"},
	})
	if err != nil {
		t.Fatalf("CreateBooking error: %v", err)
	}
	if res.BookingID != "appt_1" {
		t.Fatalf("unexpected booking id: %s", res.BookingID)
	}

	want := []string{"CreateCart", "CartAddSelectedBookableItem", "CartBookableTimes",
		"ReserveCartBookableItems", "UpdateCart", "CheckoutCart"}
	if len(ops) != len(want) {
		t.Fatalf("ops mismatch: got=%v want=%v", ops, want)
	}
	for i := range want {
		if ops[i] != want[i] {
			t.Fatalf("op[%d]=%s want=%s", i, ops[i], want[i])
		}
	}
}

func TestGraphQLError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]any{{"message": "boom"}}})
	}))
	defer ts.Close()

	c := NewBoulevardClient("biz", "loc", nil)
	c.endpoint = ts.URL
	_, err := c.GetServices(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !contains(err.Error(), "boom") {
		t.Fatalf("expected error to contain 'boom', got: %v", err)
	}
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
