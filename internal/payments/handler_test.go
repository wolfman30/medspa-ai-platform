package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	paymentsql "github.com/wolfman30/medspa-ai-platform/internal/payments/sqlc"
	"github.com/wolfman30/medspa-ai-platform/internal/tenancy"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestCheckoutHandler_UsesProvidedBookingIntentID(t *testing.T) {
	leadID := uuid.New()
	orgID := uuid.New()
	bookingID := uuid.New()

	leadsRepo := &stubLeadsRepo{
		lead: &leads.Lead{
			ID:    leadID.String(),
			OrgID: orgID.String(),
			Phone: "+15550000000",
		},
	}
	paymentsRepo := &stubPaymentRepo{}
	square := &stubSquareCheckout{}
	handler := NewCheckoutHandler(leadsRepo, paymentsRepo, square, logging.Default(), 5000)

	payload := map[string]any{
		"lead_id":           leadID.String(),
		"amount_cents":      7500,
		"booking_intent_id": bookingID.String(),
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/payments/checkout", bytes.NewReader(body))
	req = req.WithContext(setOrgID(req.Context(), orgID.String()))
	rr := httptest.NewRecorder()

	handler.CreateCheckout(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if paymentsRepo.lastBookingIntent != bookingID {
		t.Fatalf("expected booking intent %s to be persisted, got %s", bookingID, paymentsRepo.lastBookingIntent)
	}
	if square.lastParams.BookingIntentID != bookingID {
		t.Fatalf("expected booking intent %s to be used in Square params, got %s", bookingID, square.lastParams.BookingIntentID)
	}
}

func TestCheckoutHandler_PersistsScheduledFor(t *testing.T) {
	leadID := uuid.New()
	orgID := uuid.New()
	scheduled := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)

	leadsRepo := &stubLeadsRepo{
		lead: &leads.Lead{
			ID:    leadID.String(),
			OrgID: orgID.String(),
			Phone: "+15550000000",
		},
	}
	paymentsRepo := &stubPaymentRepo{}
	square := &stubSquareCheckout{}
	handler := NewCheckoutHandler(leadsRepo, paymentsRepo, square, logging.Default(), 5000)

	payload := map[string]any{
		"lead_id":        leadID.String(),
		"amount_cents":   6500,
		"scheduled_for":  scheduled.Format(time.RFC3339),
		"success_url":    "http://success",
		"cancel_url":     "http://cancel",
		"booking_intent": "",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/payments/checkout", bytes.NewReader(body))
	req = req.WithContext(setOrgID(req.Context(), orgID.String()))
	rr := httptest.NewRecorder()

	handler.CreateCheckout(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if paymentsRepo.lastScheduled == nil || !paymentsRepo.lastScheduled.Equal(scheduled) {
		t.Fatalf("expected scheduled_for persisted, got %v", paymentsRepo.lastScheduled)
	}
	if square.lastParams.ScheduledFor == nil || !square.lastParams.ScheduledFor.Equal(scheduled) {
		t.Fatalf("expected scheduled_for sent to Square, got %v", square.lastParams.ScheduledFor)
	}
}

func TestCheckoutHandler_RejectsBadScheduledFor(t *testing.T) {
	leadID := uuid.New()
	orgID := uuid.New()

	leadsRepo := &stubLeadsRepo{
		lead: &leads.Lead{
			ID:    leadID.String(),
			OrgID: orgID.String(),
			Phone: "+15550000000",
		},
	}
	handler := NewCheckoutHandler(leadsRepo, &stubPaymentRepo{}, &stubSquareCheckout{}, logging.Default(), 5000)

	payload := map[string]any{
		"lead_id":       leadID.String(),
		"amount_cents":  6500,
		"scheduled_for": "not-a-time",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/payments/checkout", bytes.NewReader(body))
	req = req.WithContext(setOrgID(req.Context(), orgID.String()))
	rr := httptest.NewRecorder()

	handler.CreateCheckout(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid scheduled_for, got %d", rr.Code)
	}
}

func TestCheckoutHandler_RejectsInvalidBookingIntentID(t *testing.T) {
	leadID := uuid.New()
	orgID := uuid.New()

	leadsRepo := &stubLeadsRepo{
		lead: &leads.Lead{
			ID:    leadID.String(),
			OrgID: orgID.String(),
			Phone: "+15550000000",
		},
	}
	handler := NewCheckoutHandler(leadsRepo, &stubPaymentRepo{}, &stubSquareCheckout{}, logging.Default(), 5000)

	payload := map[string]any{
		"lead_id":           leadID.String(),
		"amount_cents":      7500,
		"booking_intent_id": "not-a-uuid",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/payments/checkout", bytes.NewReader(body))
	req = req.WithContext(setOrgID(req.Context(), orgID.String()))
	rr := httptest.NewRecorder()

	handler.CreateCheckout(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid booking_intent_id, got %d", rr.Code)
	}
}

// stubs

type stubLeadsRepo struct {
	lead *leads.Lead
	err  error
}

func (s *stubLeadsRepo) Create(reqCtx context.Context, req *leads.CreateLeadRequest) (*leads.Lead, error) {
	return nil, nil
}

func (s *stubLeadsRepo) GetByID(ctx context.Context, orgID string, id string) (*leads.Lead, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.lead, nil
}

func (s *stubLeadsRepo) GetOrCreateByPhone(ctx context.Context, orgID string, phone string, source string, defaultName string) (*leads.Lead, error) {
	return s.lead, s.err
}

func (s *stubLeadsRepo) UpdateSchedulingPreferences(context.Context, string, leads.SchedulingPreferences) error {
	return nil
}

func (s *stubLeadsRepo) UpdateDepositStatus(context.Context, string, string, string) error {
	return nil
}

func (s *stubLeadsRepo) ListByOrg(context.Context, string, leads.ListLeadsFilter) ([]*leads.Lead, error) {
	return nil, nil
}

func (s *stubLeadsRepo) UpdateSelectedAppointment(context.Context, string, leads.SelectedAppointment) error {
	return nil
}

func (s *stubLeadsRepo) UpdateBookingSession(context.Context, string, leads.BookingSessionUpdate) error {
	return nil
}

func (s *stubLeadsRepo) GetByBookingSessionID(context.Context, string) (*leads.Lead, error) {
	return nil, nil
}

func (s *stubLeadsRepo) UpdateEmail(context.Context, string, string) error {
	return nil
}

type stubPaymentRepo struct {
	lastBookingIntent uuid.UUID
	lastScheduled     *time.Time
}

func (s *stubPaymentRepo) CreateIntent(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID, provider string, bookingIntent uuid.UUID, amountCents int32, status string, scheduledFor *time.Time) (*paymentsql.Payment, error) {
	s.lastBookingIntent = bookingIntent
	s.lastScheduled = scheduledFor
	return &paymentsql.Payment{
		ID: pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true},
	}, nil
}

type stubSquareCheckout struct {
	lastParams CheckoutParams
}

func (s *stubSquareCheckout) CreatePaymentLink(ctx context.Context, params CheckoutParams) (*CheckoutResponse, error) {
	s.lastParams = params
	return &CheckoutResponse{
		URL:        "http://example.com/checkout",
		ProviderID: "sq_123",
	}, nil
}

// helper to set org_id in context for handler
func setOrgID(ctx context.Context, orgID string) context.Context {
	return tenancy.WithOrgID(ctx, orgID)
}
