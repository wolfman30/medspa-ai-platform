package payments

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestSquareCheckoutService_CreatePaymentLink_BuildsValidCheckoutPayload(t *testing.T) {
	var gotBody map[string]any

	locationID := "LOC123"
	accessToken := "token-abc"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		wantPath := fmt.Sprintf("/v2/locations/%s/checkouts", locationID)
		if r.URL.Path != wantPath {
			t.Errorf("expected path %s, got %s", wantPath, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+accessToken {
			t.Errorf("expected auth header, got %q", got)
		}
		if r.Header.Get("Square-Version") == "" {
			t.Errorf("expected Square-Version header to be set")
		}

		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}
		_ = r.Body.Close()

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"checkout":{"id":"chk_123","checkout_page_url":"https://squareup.com/checkout/abc"}}`)
	}))
	defer srv.Close()

	bookingID := uuid.New()
	svc := NewSquareCheckoutService(accessToken, locationID, "https://success.example", "", nil).WithBaseURL(srv.URL)

	resp, err := svc.CreatePaymentLink(context.Background(), CheckoutParams{
		OrgID:           "org-1",
		LeadID:          "lead-1",
		AmountCents:     5000,
		BookingIntentID: bookingID,
		Description:     "Test Deposit",
		FromNumber:      "+15551112222",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if resp.URL != "https://squareup.com/checkout/abc" {
		t.Fatalf("unexpected checkout url: %s", resp.URL)
	}
	if resp.ProviderID != "chk_123" {
		t.Fatalf("unexpected provider id: %s", resp.ProviderID)
	}

	if gotBody == nil {
		t.Fatalf("expected request body to be captured")
	}
	if gotBody["idempotency_key"] != bookingID.String() {
		t.Fatalf("expected idempotency_key %s, got %#v", bookingID, gotBody["idempotency_key"])
	}
	if gotBody["redirect_url"] != "https://success.example" {
		t.Fatalf("expected redirect_url to use default success url, got %#v", gotBody["redirect_url"])
	}

	createOrderReq := mustMap(t, gotBody["order"])
	order := mustMap(t, createOrderReq["order"])
	if order["location_id"] != locationID {
		t.Fatalf("expected location_id %s, got %#v", locationID, order["location_id"])
	}
	meta := mustMap(t, order["metadata"])
	if meta["from_number"] != "+15551112222" {
		t.Fatalf("expected from_number metadata, got %#v", meta["from_number"])
	}

	items := mustSlice(t, order["line_items"])
	if len(items) != 1 {
		t.Fatalf("expected 1 line_item, got %d", len(items))
	}
	item := mustMap(t, items[0])
	if item["name"] != "Test Deposit" {
		t.Fatalf("expected line item name, got %#v", item["name"])
	}
	price := mustMap(t, item["base_price_money"])
	if int(price["amount"].(float64)) != 5000 {
		t.Fatalf("expected amount 5000, got %#v", price["amount"])
	}
	if price["currency"] != "USD" {
		t.Fatalf("expected currency USD, got %#v", price["currency"])
	}
}

func TestSquareCheckoutService_CreatePaymentLink_OmitsInvalidRedirectURL(t *testing.T) {
	var gotBody map[string]any

	locationID := "LOC123"
	accessToken := "token-abc"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}
		_ = r.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"checkout":{"id":"chk_123","checkout_page_url":"https://squareup.com/checkout/abc"}}`)
	}))
	defer srv.Close()

	// localhost redirect URLs are rejected by Square; service should omit them.
	svc := NewSquareCheckoutService(accessToken, locationID, "http://localhost:8080/success", "", nil).WithBaseURL(srv.URL)

	if _, err := svc.CreatePaymentLink(context.Background(), CheckoutParams{
		OrgID:       "org-1",
		LeadID:      "lead-1",
		AmountCents: 5000,
	}); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if _, ok := gotBody["redirect_url"]; ok {
		t.Fatalf("expected redirect_url to be omitted for invalid default")
	}
}

func mustMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok || m == nil {
		t.Fatalf("expected map, got %#v", v)
	}
	return m
}

func mustSlice(t *testing.T, v any) []any {
	t.Helper()
	s, ok := v.([]any)
	if !ok {
		t.Fatalf("expected slice, got %#v", v)
	}
	return s
}
