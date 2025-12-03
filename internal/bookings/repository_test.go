package bookings

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	bookingsql "github.com/wolfman30/medspa-ai-platform/internal/bookings/sqlc"
)

func TestCreateConfirmedPersistsScheduledFor(t *testing.T) {
	scheduled := time.Now().Add(5 * time.Hour).UTC()
	querier := &stubBookingQuerier{}
	repo := NewRepositoryWithQuerier(querier)

	orgID := uuid.New()
	leadID := uuid.New()

	if _, err := repo.CreateConfirmed(context.Background(), orgID, leadID, &scheduled); err != nil {
		t.Fatalf("CreateConfirmed returned error: %v", err)
	}

	if querier.lastInsert == nil {
		t.Fatalf("expected InsertBooking to be called")
	}
	if !querier.lastInsert.ScheduledFor.Valid {
		t.Fatalf("expected scheduled_for to be set on booking insert")
	}
	if !querier.lastInsert.ScheduledFor.Time.Equal(scheduled) {
		t.Fatalf("scheduled_for mismatch, got %s want %s", querier.lastInsert.ScheduledFor.Time, scheduled)
	}
}

type stubBookingQuerier struct {
	lastInsert *bookingsql.InsertBookingParams
}

func (s *stubBookingQuerier) InsertBooking(ctx context.Context, arg bookingsql.InsertBookingParams) (bookingsql.Booking, error) {
	s.lastInsert = &arg
	return bookingsql.Booking{ScheduledFor: arg.ScheduledFor}, nil
}

func (*stubBookingQuerier) GetBookingForOrg(ctx context.Context, arg bookingsql.GetBookingForOrgParams) (bookingsql.Booking, error) {
	return bookingsql.Booking{}, nil
}
