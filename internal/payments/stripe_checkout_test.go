package payments

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

type stubStripeAccountResolver struct {
	accountID string
	err       error
}

func (s *stubStripeAccountResolver) GetStripeAccountID(ctx context.Context, orgID string) (string, error) {
	return s.accountID, s.err
}

func TestStripeCheckoutService_CreatePaymentLink(t *testing.T) {
	var gotForm map[string][]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/checkout/sessions" {
			t.Errorf("expected path /v1/checkout/sessions, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk_test_123" {
			t.Errorf("expected auth header, got %q", got)
		}
		if r.Header.Get("Stripe-Version") == "" {
			t.Errorf("expected Stripe-Version header")
		}
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("expected form-urlencoded content type, got %q", r.Header.Get("Content-Type"))
		}

		if err := r.ParseForm(); err != nil {
			t.Fatalf("failed to parse form: %v", err)
		}
		gotForm = r.PostForm

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id":  "cs_test_abc123",
			"url": "https://checkout.stripe.com/pay/cs_test_abc123",
		})
	}))
	defer srv.Close()

	bookingID := uuid.New()
	scheduled := time.Date(2025, 6, 15, 14, 0, 0, 0, time.UTC)

	svc := NewStripeCheckoutService("sk_test_123", "https://success.example.com", "https://cancel.example.com", nil).
		WithBaseURL(srv.URL).
		WithAccountResolver(&stubStripeAccountResolver{accountID: "acct_clinic123"})

	resp, err := svc.CreatePaymentLink(context.Background(), CheckoutParams{
		OrgID:           "org-1",
		LeadID:          "lead-1",
		AmountCents:     5000,
		BookingIntentID: bookingID,
		Description:     "Botox Deposit",
		ScheduledFor:    &scheduled,
		FromNumber:      "+15551112222",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.URL != "https://checkout.stripe.com/pay/cs_test_abc123" {
		t.Fatalf("unexpected URL: %s", resp.URL)
	}
	if resp.ProviderID != "cs_test_abc123" {
		t.Fatalf("unexpected provider ID: %s", resp.ProviderID)
	}

	// Verify form params
	if gotForm == nil {
		t.Fatal("expected form to be captured")
	}
	assertFormValue(t, gotForm, "mode", "payment")
	assertFormValue(t, gotForm, "line_items[0][price_data][currency]", "usd")
	assertFormValue(t, gotForm, "line_items[0][price_data][unit_amount]", "5000")
	assertFormValue(t, gotForm, "line_items[0][price_data][product_data][name]", "Botox Deposit")
	assertFormValue(t, gotForm, "line_items[0][quantity]", "1")
	assertFormValue(t, gotForm, "success_url", "https://success.example.com")
	assertFormValue(t, gotForm, "cancel_url", "https://cancel.example.com")
	assertFormValue(t, gotForm, "metadata[org_id]", "org-1")
	assertFormValue(t, gotForm, "metadata[lead_id]", "lead-1")
	assertFormValue(t, gotForm, "metadata[booking_intent_id]", bookingID.String())
	assertFormValue(t, gotForm, "metadata[scheduled_for]", scheduled.UTC().Format(time.RFC3339))
	assertFormValue(t, gotForm, "metadata[from_number]", "+15551112222")
	assertFormValue(t, gotForm, "payment_intent_data[transfer_data][destination]", "acct_clinic123")
}

func TestStripeCheckoutService_DryRun(t *testing.T) {
	svc := NewStripeCheckoutService("sk_test_123", "", "", nil).WithDryRun(true)

	resp, err := svc.CreatePaymentLink(context.Background(), CheckoutParams{
		OrgID:           "org-1",
		LeadID:          "lead-1",
		AmountCents:     5000,
		BookingIntentID: uuid.New(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.URL == "" {
		t.Fatal("expected non-empty URL in dry run")
	}
	if resp.ProviderID == "" {
		t.Fatal("expected non-empty provider ID in dry run")
	}
}

func TestStripeCheckoutService_NoConnectedAccount(t *testing.T) {
	// When no account resolver is set, transfer_data should not be included
	var gotForm map[string][]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("failed to parse form: %v", err)
		}
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id":  "cs_test_no_connect",
			"url": "https://checkout.stripe.com/pay/cs_test_no_connect",
		})
	}))
	defer srv.Close()

	svc := NewStripeCheckoutService("sk_test_123", "", "", nil).WithBaseURL(srv.URL)

	_, err := svc.CreatePaymentLink(context.Background(), CheckoutParams{
		OrgID:           "org-1",
		LeadID:          "lead-1",
		AmountCents:     3000,
		BookingIntentID: uuid.New(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := gotForm["payment_intent_data[transfer_data][destination]"]; ok {
		t.Fatal("expected no transfer_data when no connected account")
	}
}

func TestStripeCheckoutService_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"message":"Invalid API key","type":"invalid_request_error"}}`)
	}))
	defer srv.Close()

	svc := NewStripeCheckoutService("sk_bad", "", "", nil).WithBaseURL(srv.URL)

	_, err := svc.CreatePaymentLink(context.Background(), CheckoutParams{
		OrgID:           "org-1",
		LeadID:          "lead-1",
		AmountCents:     5000,
		BookingIntentID: uuid.New(),
	})
	if err == nil {
		t.Fatal("expected error for bad API response")
	}
}

func TestStripeCheckoutService_AccountResolverError(t *testing.T) {
	svc := NewStripeCheckoutService("sk_test_123", "", "", nil).
		WithAccountResolver(&stubStripeAccountResolver{err: fmt.Errorf("no account")})

	_, err := svc.CreatePaymentLink(context.Background(), CheckoutParams{
		OrgID:           "org-1",
		LeadID:          "lead-1",
		AmountCents:     5000,
		BookingIntentID: uuid.New(),
	})
	if err == nil {
		t.Fatal("expected error when account resolver fails")
	}
}

func TestStripeCheckoutService_DefaultDescription(t *testing.T) {
	var gotForm map[string][]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id":  "cs_test_default",
			"url": "https://checkout.stripe.com/pay/cs_test_default",
		})
	}))
	defer srv.Close()

	svc := NewStripeCheckoutService("sk_test_123", "", "", nil).WithBaseURL(srv.URL)

	_, err := svc.CreatePaymentLink(context.Background(), CheckoutParams{
		OrgID:           "org-1",
		LeadID:          "lead-1",
		AmountCents:     5000,
		BookingIntentID: uuid.New(),
		Description:     "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertFormValue(t, gotForm, "line_items[0][price_data][product_data][name]", "Deposit")
}

func assertFormValue(t *testing.T, form map[string][]string, key, want string) {
	t.Helper()
	got := form[key]
	if len(got) == 0 {
		t.Errorf("form key %q not found", key)
		return
	}
	if got[0] != want {
		t.Errorf("form[%q] = %q, want %q", key, got[0], want)
	}
}
