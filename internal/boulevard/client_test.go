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
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"services": []map[string]any{{"id": "svc_1", "name": "Botox", "duration": 30}},
			},
		})
	}))
	defer ts.Close()

	c := NewBoulevardClient("key", "biz", nil)
	c.endpoint = ts.URL

	services, err := c.GetServices(context.Background())
	if err != nil {
		t.Fatalf("GetServices error: %v", err)
	}
	if len(services) != 1 || services[0].ID != "svc_1" {
		t.Fatalf("unexpected services: %+v", services)
	}
}

func TestCreateBooking_FullCartFlow(t *testing.T) {
	ops := make([]string, 0, 5)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode req: %v", err)
		}
		op, _ := req["operationName"].(string)
		ops = append(ops, op)

		switch op {
		case "CreateCart":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"createCart": map[string]any{"cart": map[string]any{"id": "cart_1"}}}})
		case "CartAddService":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"cartAddService": map[string]any{"cart": map[string]any{"id": "cart_1"}}}})
		case "CartReserveTimeSlot":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"cartReserveTimeSlot": map[string]any{"cart": map[string]any{"id": "cart_1"}}}})
		case "CartSetClient":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"cartSetClient": map[string]any{"cart": map[string]any{"id": "cart_1"}}}})
		case "CartCheckout":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"cartCheckout": map[string]any{"booking": map[string]any{"id": "book_1", "status": "confirmed"}}}})
		default:
			http.Error(w, "unknown op", http.StatusBadRequest)
		}
	}))
	defer ts.Close()

	c := NewBoulevardClient("key", "biz", nil)
	c.endpoint = ts.URL

	res, err := c.CreateBooking(context.Background(), CreateBookingRequest{
		ServiceID: "svc_1",
		StartAt:   time.Date(2026, 2, 21, 15, 0, 0, 0, time.UTC),
		Client:    Client{FirstName: "Jane", LastName: "Doe", Email: "jane@example.com", Phone: "+15555550123"},
	})
	if err != nil {
		t.Fatalf("CreateBooking error: %v", err)
	}
	if res.BookingID != "book_1" || res.CartID != "cart_1" {
		t.Fatalf("unexpected result: %+v", res)
	}

	want := []string{"CreateCart", "CartAddService", "CartReserveTimeSlot", "CartSetClient", "CartCheckout"}
	if len(ops) != len(want) {
		t.Fatalf("unexpected ops len: got=%d want=%d ops=%v", len(ops), len(want), ops)
	}
	for i := range want {
		if ops[i] != want[i] {
			t.Fatalf("op[%d]=%s want=%s (ops=%v)", i, ops[i], want[i], ops)
		}
	}
}

func TestGraphQLError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]any{{"message": "boom"}}})
	}))
	defer ts.Close()

	c := NewBoulevardClient("key", "biz", nil)
	c.endpoint = ts.URL
	if _, err := c.GetServices(context.Background()); err == nil {
		t.Fatal("expected error, got nil")
	}
}
