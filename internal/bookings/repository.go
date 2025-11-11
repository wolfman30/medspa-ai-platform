package bookings

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	bookingsql "github.com/wolfman30/medspa-ai-platform/internal/bookings/sqlc"
)

// Repository provides persistence helpers for bookings.
type Repository struct {
	queries bookingsql.Querier
}

// NewRepository creates a repository backed by pgx pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	if pool == nil {
		panic("bookings: pgx pool required")
	}
	return &Repository{queries: bookingsql.New(pool)}
}

// NewRepositoryWithQuerier allows injecting mocks for tests.
func NewRepositoryWithQuerier(q bookingsql.Querier) *Repository {
	return &Repository{queries: q}
}

// CreateConfirmed inserts a confirmed booking row.
func (r *Repository) CreateConfirmed(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID, scheduledFor *time.Time) (*bookingsql.Booking, error) {
	arg := bookingsql.InsertBookingParams{
		ID:           toPGUUID(uuid.New()),
		OrgID:        orgID.String(),
		LeadID:       toPGUUID(leadID),
		Status:       "confirmed",
		ConfirmedAt:  toPGTime(time.Now().UTC()),
		ScheduledFor: toPGNullableTime(scheduledFor),
	}
	row, err := r.queries.InsertBooking(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("bookings: insert confirmed: %w", err)
	}
	return &row, nil
}

// GetForOrg returns a booking scoped to the org.
func (r *Repository) GetForOrg(ctx context.Context, orgID uuid.UUID, bookingID uuid.UUID) (*bookingsql.Booking, error) {
	row, err := r.queries.GetBookingForOrg(ctx, bookingsql.GetBookingForOrgParams{
		ID:    toPGUUID(bookingID),
		OrgID: orgID.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("bookings: load for org: %w", err)
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

func toPGTime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{
		Time:  t,
		Valid: true,
	}
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

