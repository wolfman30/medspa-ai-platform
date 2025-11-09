package payments

import (
	"context"
	"errors"
)

var (
	// ErrNotImplemented is returned for stub methods
	ErrNotImplemented = errors.New("payment processing not yet implemented")
)

// Payment represents a payment transaction
type Payment struct {
	ID            string
	Amount        int64 // Amount in cents
	Currency      string
	CustomerID    string
	Status        string
	PaymentMethod string
}

// Service defines the interface for payment processing
type Service interface {
	CreatePayment(ctx context.Context, amount int64, currency, customerID string) (*Payment, error)
	GetPayment(ctx context.Context, paymentID string) (*Payment, error)
	RefundPayment(ctx context.Context, paymentID string) error
}

// StubService is a stub implementation of the payment service
type StubService struct{}

// NewStubService creates a new stub payment service
func NewStubService() *StubService {
	return &StubService{}
}

// CreatePayment is a stub method
func (s *StubService) CreatePayment(ctx context.Context, amount int64, currency, customerID string) (*Payment, error) {
	return nil, ErrNotImplemented
}

// GetPayment is a stub method
func (s *StubService) GetPayment(ctx context.Context, paymentID string) (*Payment, error) {
	return nil, ErrNotImplemented
}

// RefundPayment is a stub method
func (s *StubService) RefundPayment(ctx context.Context, paymentID string) error {
	return ErrNotImplemented
}
