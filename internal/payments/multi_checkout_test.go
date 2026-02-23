package payments

import (
	"context"
	"errors"
	"testing"
)

type mockCheckoutProvider struct {
	resp *CheckoutResponse
	err  error
}

func (m *mockCheckoutProvider) CreatePaymentLink(_ context.Context, _ CheckoutParams) (*CheckoutResponse, error) {
	return m.resp, m.err
}

func TestMultiCheckoutService_NoClinicStore(t *testing.T) {
	square := &mockCheckoutProvider{resp: &CheckoutResponse{URL: "https://square.com/pay"}}
	svc := NewMultiCheckoutService(square, nil, nil, nil)

	resp, err := svc.CreatePaymentLink(context.Background(), CheckoutParams{OrgID: "org-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.URL != "https://square.com/pay" {
		t.Errorf("got URL %q", resp.URL)
	}
}

func TestMultiCheckoutService_NoClinicStore_StripeOnly(t *testing.T) {
	stripe := &mockCheckoutProvider{resp: &CheckoutResponse{URL: "https://stripe.com/pay"}}
	svc := NewMultiCheckoutService(nil, stripe, nil, nil)

	resp, err := svc.CreatePaymentLink(context.Background(), CheckoutParams{OrgID: "org-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.URL != "https://stripe.com/pay" {
		t.Errorf("got URL %q", resp.URL)
	}
}

func TestMultiCheckoutService_NoProviders(t *testing.T) {
	svc := NewMultiCheckoutService(nil, nil, nil, nil)

	_, err := svc.CreatePaymentLink(context.Background(), CheckoutParams{OrgID: "org-1"})
	if err == nil {
		t.Fatal("expected error when no providers configured")
	}
}

func TestMultiCheckoutService_SquareError(t *testing.T) {
	square := &mockCheckoutProvider{err: errors.New("square down")}
	svc := NewMultiCheckoutService(square, nil, nil, nil)

	_, err := svc.CreatePaymentLink(context.Background(), CheckoutParams{OrgID: "org-1"})
	if err == nil {
		t.Fatal("expected error")
	}
}
