package payments

import (
	"context"
	"testing"
)

func TestStubServiceReturnsNotImplemented(t *testing.T) {
	svc := NewStubService()
	ctx := context.Background()

	if payment, err := svc.CreatePayment(ctx, 1000, "usd", "cust_123"); err != ErrNotImplemented || payment != nil {
		t.Fatalf("expected ErrNotImplemented and nil payment, got %v, %#v", err, payment)
	}

	if payment, err := svc.GetPayment(ctx, "pay_123"); err != ErrNotImplemented || payment != nil {
		t.Fatalf("expected ErrNotImplemented and nil payment, got %v, %#v", err, payment)
	}

	if err := svc.RefundPayment(ctx, "pay_123"); err != ErrNotImplemented {
		t.Fatalf("expected ErrNotImplemented, got %v", err)
	}
}
