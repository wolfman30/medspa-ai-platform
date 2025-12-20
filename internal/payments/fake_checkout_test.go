package payments

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestFakeCheckoutService_RequiresBaseURL(t *testing.T) {
	svc := NewFakeCheckoutService("", nil)
	_, err := svc.CreatePaymentLink(context.Background(), CheckoutParams{
		OrgID:           "org",
		LeadID:          uuid.NewString(),
		AmountCents:     5000,
		BookingIntentID: uuid.New(),
	})
	if err == nil {
		t.Fatalf("expected error for missing PUBLIC_BASE_URL")
	}
}

func TestFakeCheckoutService_RequiresBookingIntent(t *testing.T) {
	svc := NewFakeCheckoutService("https://example.com", nil)
	_, err := svc.CreatePaymentLink(context.Background(), CheckoutParams{
		OrgID:       "org",
		LeadID:      uuid.NewString(),
		AmountCents: 5000,
	})
	if err == nil {
		t.Fatalf("expected error for missing booking intent id")
	}
}

func TestFakeCheckoutService_ReturnsInternalURL(t *testing.T) {
	intentID := uuid.New()
	svc := NewFakeCheckoutService("https://api-dev.aiwolfsolutions.com/", nil)
	resp, err := svc.CreatePaymentLink(context.Background(), CheckoutParams{
		OrgID:           "org",
		LeadID:          uuid.NewString(),
		AmountCents:     5000,
		BookingIntentID: intentID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil || resp.URL == "" {
		t.Fatalf("expected checkout url")
	}
	wantPrefix := "https://api-dev.aiwolfsolutions.com/payments/fake/" + intentID.String()
	if resp.URL != wantPrefix {
		t.Fatalf("unexpected url: got %s want %s", resp.URL, wantPrefix)
	}
	if resp.ProviderID != "fake:"+intentID.String() {
		t.Fatalf("unexpected provider id: %s", resp.ProviderID)
	}
}

