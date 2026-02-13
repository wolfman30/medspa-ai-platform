package payments

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	paymentsql "github.com/wolfman30/medspa-ai-platform/internal/payments/sqlc"
)

// Repository persists payment intents and lifecycle transitions.
type Repository struct {
	queries         paymentsql.Querier
	disableCooldown bool // When true, always returns false from HasOpenDeposit (for testing)

	// In-memory checkout URL cache for short URL redirects.
	// Keyed by short code (first 8 chars of payment UUID).
	checkoutURLs   map[string]checkoutURLEntry
	checkoutURLsMu sync.RWMutex
}

type checkoutURLEntry struct {
	url       string
	expiresAt time.Time
}

// NewRepository creates a repository backed by pgx.
// Set DISABLE_PAYMENT_COOLDOWN=true to bypass the 72-hour cooldown check (for testing).
func NewRepository(pool *pgxpool.Pool) *Repository {
	if pool == nil {
		panic("payments: pgx pool required")
	}
	disableCooldown := strings.EqualFold(os.Getenv("DISABLE_PAYMENT_COOLDOWN"), "true")
	return &Repository{
		queries:         paymentsql.New(pool),
		disableCooldown: disableCooldown,
		checkoutURLs:    make(map[string]checkoutURLEntry),
	}
}

// NewRepositoryWithQuerier allows injecting a mocked sqlc interface for tests.
func NewRepositoryWithQuerier(q paymentsql.Querier) *Repository {
	return &Repository{
		queries:      q,
		checkoutURLs: make(map[string]checkoutURLEntry),
	}
}

// HasOpenDeposit returns true if a deposit intent already exists for the lead/org in pending or succeeded state.
// If DISABLE_PAYMENT_COOLDOWN=true, this always returns false to allow repeated testing.
func (r *Repository) HasOpenDeposit(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID) (bool, error) {
	if r.disableCooldown {
		return false, nil
	}
	arg := paymentsql.GetOpenDepositByOrgAndLeadParams{
		OrgID:  orgID.String(),
		LeadID: toPGUUID(leadID),
	}
	payment, err := r.queries.GetOpenDepositByOrgAndLead(ctx, arg)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("payments: check deposit by lead: %w", err)
	}
	return payment.Status == "deposit_pending" || payment.Status == "succeeded", nil
}

// OpenDepositStatus returns the status of the most recent pending or succeeded deposit within 72 hours.
// It returns an empty string when no matching deposit exists.
func (r *Repository) OpenDepositStatus(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID) (string, error) {
	arg := paymentsql.GetOpenDepositByOrgAndLeadParams{
		OrgID:  orgID.String(),
		LeadID: toPGUUID(leadID),
	}
	payment, err := r.queries.GetOpenDepositByOrgAndLead(ctx, arg)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("payments: load open deposit: %w", err)
	}
	return payment.Status, nil
}

// CreateIntent persists a payment intent in deposit pending status.
func (r *Repository) CreateIntent(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID, provider string, bookingIntent uuid.UUID, amountCents int32, status string, scheduledFor *time.Time) (*paymentsql.Payment, error) {
	arg := paymentsql.InsertPaymentParams{
		ID:              toPGUUID(uuid.New()),
		OrgID:           orgID.String(),
		LeadID:          toPGUUID(leadID),
		Provider:        provider,
		ProviderRef:     pgtype.Text{},
		BookingIntentID: toPGUUID(bookingIntent),
		AmountCents:     amountCents,
		Status:          status,
		ScheduledFor:    toPGNullableTime(scheduledFor),
	}
	row, err := r.queries.InsertPayment(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("payments: failed to insert intent: %w", err)
	}
	return &row, nil
}

// MarkSucceeded updates a payment using the provider reference (idempotent on ref).
func (r *Repository) MarkSucceeded(ctx context.Context, providerRef string, status string) (*paymentsql.Payment, error) {
	arg := paymentsql.UpdatePaymentStatusByProviderRefParams{
		ProviderRef: pgtype.Text{
			String: providerRef,
			Valid:  providerRef != "",
		},
		Status: status,
		ProviderRef_2: pgtype.Text{
			String: providerRef,
			Valid:  providerRef != "",
		},
	}
	row, err := r.queries.UpdatePaymentStatusByProviderRef(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("payments: update by provider ref: %w", err)
	}
	return &row, nil
}

// UpdateStatusByID updates a payment using our UUID identifier.
func (r *Repository) UpdateStatusByID(ctx context.Context, id uuid.UUID, status, providerRef string) (*paymentsql.Payment, error) {
	arg := paymentsql.UpdatePaymentStatusByIDParams{
		ID:     toPGUUID(id),
		Status: status,
		ProviderRef: pgtype.Text{
			String: providerRef,
			Valid:  providerRef != "",
		},
	}
	row, err := r.queries.UpdatePaymentStatusByID(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("payments: update by id: %w", err)
	}
	return &row, nil
}

// GetByProviderRef fetches a payment by provider reference.
func (r *Repository) GetByProviderRef(ctx context.Context, providerRef string) (*paymentsql.Payment, error) {
	row, err := r.queries.GetPaymentByProviderRef(ctx, pgtype.Text{String: providerRef, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("payments: load by provider ref: %w", err)
	}
	return &row, nil
}

// GetByID fetches a payment by UUID.
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*paymentsql.Payment, error) {
	row, err := r.queries.GetPaymentByID(ctx, toPGUUID(id))
	if err != nil {
		return nil, fmt.Errorf("payments: load by id: %w", err)
	}
	return &row, nil
}

func toPGUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{
		Bytes: [16]byte(id),
		Valid: true,
	}
}

// SaveCheckoutURL stores a checkout URL keyed by a short code for redirect lookups.
// The short code is the first 8 characters of the payment UUID (no dashes).
// URLs expire after 24 hours.
func (r *Repository) SaveCheckoutURL(paymentID uuid.UUID, checkoutURL string) string {
	code := ShortCodeFromUUID(paymentID)
	r.checkoutURLsMu.Lock()
	r.checkoutURLs[code] = checkoutURLEntry{
		url:       checkoutURL,
		expiresAt: time.Now().Add(24 * time.Hour),
	}
	r.checkoutURLsMu.Unlock()
	return code
}

// GetCheckoutURLByShortCode returns the checkout URL for the given short code, or empty string if not found/expired.
func (r *Repository) GetCheckoutURLByShortCode(_ context.Context, code string) (string, error) {
	r.checkoutURLsMu.RLock()
	entry, ok := r.checkoutURLs[code]
	r.checkoutURLsMu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return "", nil
	}
	return entry.url, nil
}

// ShortCodeFromUUID returns the first 8 hex chars of a UUID (no dashes) for use as a short code.
func ShortCodeFromUUID(id uuid.UUID) string {
	s := strings.ReplaceAll(id.String(), "-", "")
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func toPGNullableTime(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{
		Time:  *t,
		Valid: true,
	}
}
