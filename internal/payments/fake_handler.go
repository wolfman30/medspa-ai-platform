package payments

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	paymentsql "github.com/wolfman30/medspa-ai-platform/internal/payments/sqlc"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

type paymentByIDStore interface {
	GetByID(ctx context.Context, id uuid.UUID) (*paymentsql.Payment, error)
	UpdateStatusByID(ctx context.Context, id uuid.UUID, status, providerRef string) (*paymentsql.Payment, error)
}

// FakePaymentsHandler exposes a tiny demo UI to "complete" deposits without Square.
// Only mount this handler when ALLOW_FAKE_PAYMENTS=true.
type FakePaymentsHandler struct {
	payments   paymentByIDStore
	leads      leads.Repository
	processed  processedTracker
	outbox     outboxWriter
	numbers    OrgNumberResolver
	publicHost string
	logger     *logging.Logger
}

func NewFakePaymentsHandler(paymentsRepo paymentByIDStore, leadsRepo leads.Repository, processed processedTracker, outbox outboxWriter, numbers OrgNumberResolver, publicBaseURL string, logger *logging.Logger) *FakePaymentsHandler {
	if logger == nil {
		logger = logging.Default()
	}
	return &FakePaymentsHandler{
		payments:   paymentsRepo,
		leads:      leadsRepo,
		processed:  processed,
		outbox:     outbox,
		numbers:    numbers,
		publicHost: strings.TrimRight(strings.TrimSpace(publicBaseURL), "/"),
		logger:     logger,
	}
}

func (h *FakePaymentsHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/payments/{paymentID}", h.HandleCheckout)
	r.Post("/payments/{paymentID}/complete", h.HandleComplete)
	r.Get("/payments/{paymentID}/success", h.HandleSuccess)
	return r
}

func (h *FakePaymentsHandler) HandleCheckout(w http.ResponseWriter, r *http.Request) {
	paymentID, ok := parseUUIDParam(w, r, "paymentID")
	if !ok {
		return
	}
	if h.payments == nil {
		http.Error(w, "payments unavailable", http.StatusServiceUnavailable)
		return
	}
	row, err := h.payments.GetByID(r.Context(), paymentID)
	if err != nil || row == nil {
		http.Error(w, "payment not found", http.StatusNotFound)
		return
	}

	amount := float64(row.AmountCents) / 100.0
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Demo Deposit Checkout</title>
    <style>
      body{font-family:system-ui,-apple-system,Segoe UI,Roboto,Ubuntu,Cantarell,Noto Sans,sans-serif;max-width:680px;margin:40px auto;padding:0 16px;}
      .card{border:1px solid #e5e7eb;border-radius:12px;padding:18px;}
      .btn{display:inline-block;background:#111827;color:#fff;padding:12px 16px;border-radius:10px;text-decoration:none;border:0;cursor:pointer;}
      .muted{color:#6b7280;font-size:14px;}
      code{background:#f3f4f6;padding:2px 6px;border-radius:6px;}
    </style>
  </head>
  <body>
    <h1>Demo Deposit Checkout</h1>
    <div class="card">
      <p><strong>Amount:</strong> $%.2f</p>
      <p class="muted">This is a demo-only payment page (no real payment is processed).</p>
      <form method="POST" action="/demo/payments/%s/complete">
        <button class="btn" type="submit">Complete Deposit</button>
      </form>
      <p class="muted">Payment ID: <code>%s</code></p>
    </div>
  </body>
</html>`, amount, paymentID.String(), paymentID.String())
}

func (h *FakePaymentsHandler) HandleComplete(w http.ResponseWriter, r *http.Request) {
	paymentID, ok := parseUUIDParam(w, r, "paymentID")
	if !ok {
		return
	}
	if err := h.completePayment(r.Context(), paymentID); err != nil {
		h.logger.Error("fake payment completion failed", "error", err, "payment_id", paymentID)
		http.Error(w, "failed to complete payment", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/demo/payments/%s/success", paymentID.String()), http.StatusSeeOther)
}

func (h *FakePaymentsHandler) HandleSuccess(w http.ResponseWriter, r *http.Request) {
	paymentID, ok := parseUUIDParam(w, r, "paymentID")
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Deposit Completed</title>
    <style>
      body{font-family:system-ui,-apple-system,Segoe UI,Roboto,Ubuntu,Cantarell,Noto Sans,sans-serif;max-width:680px;margin:40px auto;padding:0 16px;}
      .card{border:1px solid #e5e7eb;border-radius:12px;padding:18px;}
      .muted{color:#6b7280;font-size:14px;}
      code{background:#f3f4f6;padding:2px 6px;border-radius:6px;}
    </style>
  </head>
  <body>
    <h1>Deposit Completed</h1>
    <div class="card">
      <p>Thanks — your demo deposit is marked as paid.</p>
      <p class="muted">You can close this tab and continue the SMS conversation.</p>
      <p class="muted">Payment ID: <code>%s</code></p>
    </div>
  </body>
</html>`, paymentID.String())
}

func (h *FakePaymentsHandler) completePayment(ctx context.Context, paymentID uuid.UUID) error {
	if h.payments == nil || h.leads == nil || h.outbox == nil {
		return fmt.Errorf("payments: fake handler missing dependencies")
	}
	idempotencyKey := "fake:" + paymentID.String()
	if h.processed != nil {
		already, err := h.processed.AlreadyProcessed(ctx, "fake.payment_succeeded", idempotencyKey)
		if err == nil && already {
			return nil
		}
	}

	row, err := h.payments.GetByID(ctx, paymentID)
	if err != nil {
		return fmt.Errorf("payments: fake load payment: %w", err)
	}
	if row == nil {
		return fmt.Errorf("payments: fake payment not found")
	}

	providerRef := strings.TrimSpace(row.ProviderRef.String)
	if providerRef == "" {
		providerRef = idempotencyKey
	}
	updated, err := h.payments.UpdateStatusByID(ctx, paymentID, "succeeded", providerRef)
	if err != nil {
		return fmt.Errorf("payments: fake update payment: %w", err)
	}

	if !updated.LeadID.Valid {
		return fmt.Errorf("payments: fake payment missing lead id")
	}
	leadUUID, err := uuid.FromBytes(updated.LeadID.Bytes[:])
	if err != nil {
		return fmt.Errorf("payments: fake lead id parse: %w", err)
	}
	leadID := leadUUID.String()

	lead, err := h.leads.GetByID(ctx, updated.OrgID, leadID)
	if err != nil {
		return fmt.Errorf("payments: fake lead lookup: %w", err)
	}
	if err := h.leads.UpdateDepositStatus(ctx, leadID, "paid", "priority"); err != nil {
		h.logger.Warn("payments: failed to update lead deposit status", "error", err, "org_id", updated.OrgID, "lead_id", leadID)
	}

	var scheduledFor *time.Time
	if updated.ScheduledFor.Valid {
		t := updated.ScheduledFor.Time
		scheduledFor = &t
	}

	event := events.PaymentSucceededV1{
		EventID:         uuid.NewString(),
		OrgID:           updated.OrgID,
		LeadID:          leadID,
		BookingIntentID: paymentID.String(),
		Provider:        "square",
		ProviderRef:     providerRef,
		AmountCents:     int64(updated.AmountCents),
		OccurredAt:      time.Now().UTC(),
		LeadPhone:       lead.Phone,
		ScheduledFor:    scheduledFor,
	}
	if h.numbers != nil {
		event.FromNumber = h.numbers.DefaultFromNumber(updated.OrgID)
	}

	if _, err := h.outbox.Insert(ctx, updated.OrgID, "payment_succeeded.v1", event); err != nil {
		return fmt.Errorf("payments: fake enqueue outbox: %w", err)
	}
	if h.processed != nil {
		if _, err := h.processed.MarkProcessed(ctx, "fake.payment_succeeded", idempotencyKey); err != nil {
			h.logger.Warn("payments: failed to record processed fake payment", "error", err, "payment_id", paymentID)
		}
	}
	return nil
}

func parseUUIDParam(w http.ResponseWriter, r *http.Request, key string) (uuid.UUID, bool) {
	raw := strings.TrimSpace(chi.URLParam(r, key))
	if raw == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return uuid.Nil, false
	}
	parsed, err := uuid.Parse(raw)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return uuid.Nil, false
	}
	return parsed, true
}
