package conversation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/wolfman30/medspa-ai-platform/internal/payments"
	paymentsql "github.com/wolfman30/medspa-ai-platform/internal/payments/sqlc"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestDepositDispatcherHappyPath(t *testing.T) {
	payRepo := &stubPaymentRepo{}
	checkout := &stubCheckout{resp: &payments.CheckoutResponse{URL: "http://pay", ProviderID: "sq_123"}}
	outbox := &stubOutbox{}
	sms := &stubReplyMessenger{}
	logger := logging.Default()

	dispatcher := NewDepositDispatcher(payRepo, checkout, outbox, sms, logger)
	msg := MessageRequest{OrgID: uuid.New().String(), LeadID: uuid.New().String(), From: "+1", To: "+2"}
	now := time.Now()
	resp := &Response{ConversationID: "conv-1", DepositIntent: &DepositIntent{AmountCents: 5000, Description: "Test", ScheduledFor: &now}}

	if err := dispatcher.SendDeposit(context.Background(), msg, resp); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !payRepo.called {
		t.Fatalf("expected payment intent created")
	}
	if !checkout.called {
		t.Fatalf("expected checkout link created")
	}
	if !sms.called {
		t.Fatalf("expected sms sent")
	}
	if !outbox.called {
		t.Fatalf("expected outbox event inserted")
	}
}

func TestDepositDispatcherMissingDeps(t *testing.T) {
	dispatcher := NewDepositDispatcher(nil, nil, nil, nil, logging.Default())
	msg := MessageRequest{OrgID: "org-1", LeadID: uuid.New().String()}
	resp := &Response{DepositIntent: &DepositIntent{AmountCents: 1000, ScheduledFor: ptrTime(time.Now())}}
	if err := dispatcher.SendDeposit(context.Background(), msg, resp); err == nil {
		t.Fatalf("expected error when dependencies missing")
	}
}

func TestDepositDispatcherSkipsWithoutSchedule(t *testing.T) {
	payRepo := &stubPaymentRepo{}
	checkout := &stubCheckout{resp: &payments.CheckoutResponse{URL: "http://pay", ProviderID: "sq_123"}}
	outbox := &stubOutbox{}
	sms := &stubReplyMessenger{}
	dispatcher := NewDepositDispatcher(payRepo, checkout, outbox, sms, logging.Default())

	msg := MessageRequest{OrgID: uuid.New().String(), LeadID: uuid.New().String(), From: "+1", To: "+2"}
	resp := &Response{ConversationID: "conv-1", DepositIntent: &DepositIntent{AmountCents: 5000, Description: "No time"}}

	if err := dispatcher.SendDeposit(context.Background(), msg, resp); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if payRepo.called || checkout.called || sms.called || outbox.called {
		t.Fatalf("expected no actions when scheduled_for missing")
	}
}

func TestDepositDispatcherSkipsDuplicate(t *testing.T) {
	payRepo := &stubPaymentRepo{hasDeposit: true}
	checkout := &stubCheckout{resp: &payments.CheckoutResponse{URL: "http://pay", ProviderID: "sq_123"}}
	outbox := &stubOutbox{}
	sms := &stubReplyMessenger{}
	dispatcher := NewDepositDispatcher(payRepo, checkout, outbox, sms, logging.Default())
	msg := MessageRequest{OrgID: uuid.New().String(), LeadID: uuid.New().String(), From: "+1", To: "+2"}
	now := time.Now()
	resp := &Response{ConversationID: "conv-1", DepositIntent: &DepositIntent{AmountCents: 5000, Description: "Test", ScheduledFor: &now}}

	if err := dispatcher.SendDeposit(context.Background(), msg, resp); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if payRepo.called || checkout.called || sms.called || outbox.called {
		t.Fatalf("expected no actions when duplicate deposit detected")
	}
}

func TestDepositDispatcherUsesMetadataSchedule(t *testing.T) {
	payRepo := &stubPaymentRepo{}
	checkout := &stubCheckout{resp: &payments.CheckoutResponse{URL: "http://pay", ProviderID: "sq_123"}}
	outbox := &stubOutbox{}
	sms := &stubReplyMessenger{}
	dispatcher := NewDepositDispatcher(payRepo, checkout, outbox, sms, logging.Default())

	when := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	msg := MessageRequest{
		OrgID:    uuid.New().String(),
		LeadID:   uuid.New().String(),
		From:     "+1",
		To:       "+2",
		Metadata: map[string]string{"scheduled_for": when.Format(time.RFC3339)},
	}
	resp := &Response{ConversationID: "conv-1", DepositIntent: &DepositIntent{AmountCents: 5000, Description: "Test"}}

	if err := dispatcher.SendDeposit(context.Background(), msg, resp); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if checkout.params.ScheduledFor == nil || !checkout.params.ScheduledFor.Equal(when) {
		t.Fatalf("expected scheduled_for propagated, got %+v", checkout.params.ScheduledFor)
	}
	if !payRepo.called || !checkout.called || !sms.called || !outbox.called {
		t.Fatalf("expected all dependencies invoked when schedule provided via metadata")
	}
}

// stubs
type stubPaymentRepo struct {
	called     bool
	hasDeposit bool
}

func (s *stubPaymentRepo) CreateIntent(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID, provider string, bookingIntent uuid.UUID, amountCents int32, status string) (*paymentsql.Payment, error) {
	s.called = true
	return &paymentsql.Payment{
		ID:          pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true},
		OrgID:       orgID.String(),
		LeadID:      pgtype.UUID{Bytes: [16]byte(leadID), Valid: true},
		AmountCents: int32(amountCents),
		Status:      status,
	}, nil
}

func (s *stubPaymentRepo) HasOpenDeposit(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID) (bool, error) {
	return s.hasDeposit, nil
}
type stubCheckout struct {
	called bool
	resp   *payments.CheckoutResponse
	err    error
	params payments.CheckoutParams
}

func (s *stubCheckout) CreatePaymentLink(ctx context.Context, params payments.CheckoutParams) (*payments.CheckoutResponse, error) {
	s.called = true
	s.params = params
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

type stubOutbox struct {
	called bool
}

func (s *stubOutbox) Insert(ctx context.Context, orgID string, eventType string, payload any) (uuid.UUID, error) {
	s.called = true
	return uuid.New(), nil
}

type stubReplyMessenger struct {
	called bool
}

func (s *stubReplyMessenger) SendReply(ctx context.Context, reply OutboundReply) error {
	s.called = true
	return nil
}

// helper to satisfy repository expectations
// keep compiler happy
var _ = errors.New
var _ = time.Now

func ptrTime(t time.Time) *time.Time {
	return &t
}
