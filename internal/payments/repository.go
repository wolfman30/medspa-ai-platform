package payments

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	paymentsql "github.com/wolfman30/medspa-ai-platform/internal/payments/sqlc"
)

// Repository persists payment intents and lifecycle transitions.
type Repository struct {
	queries paymentsql.Querier
}

// NewRepository creates a repository backed by pgx.
func NewRepository(pool *pgxpool.Pool) *Repository {
	if pool == nil {
		panic("payments: pgx pool required")
	}
	return &Repository{
		queries: paymentsql.New(pool),
	}
}

// NewRepositoryWithQuerier allows injecting a mocked sqlc interface for tests.
func NewRepositoryWithQuerier(q paymentsql.Querier) *Repository {
	return &Repository{
		queries: q,
	}
}

// CreateIntent persists a payment intent in deposit pending status.
func (r *Repository) CreateIntent(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID, provider string, bookingIntent uuid.UUID, amountCents int32, status string) (*paymentsql.Payment, error) {
	arg := paymentsql.InsertPaymentParams{
		ID:              toPGUUID(uuid.New()),
		OrgID:           orgID.String(),
		LeadID:          toPGUUID(leadID),
		Provider:        provider,
		ProviderRef:     pgtype.Text{},
		BookingIntentID: toPGUUID(bookingIntent),
		AmountCents:     amountCents,
		Status:          status,
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
