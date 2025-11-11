package bookings

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	bookingsql "github.com/wolfman30/medspa-ai-platform/internal/bookings/sqlc"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

var bookingsTracer = otel.Tracer("medspa.internal.bookings")

// Service confirms bookings once deposits are captured.
type Service struct {
	repo   *Repository
	logger *logging.Logger
}

// NewService constructs a bookings service.
func NewService(repo *Repository, logger *logging.Logger) *Service {
	if repo == nil {
		panic("bookings: repository required")
	}
	if logger == nil {
		logger = logging.Default()
	}
	return &Service{repo: repo, logger: logger}
}

// ConfirmBooking creates a confirmed booking row scoped to the org & lead.
func (s *Service) ConfirmBooking(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID, scheduledFor *time.Time) (*bookingsql.Booking, error) {
	ctx, span := bookingsTracer.Start(ctx, "bookings.confirm")
	defer span.End()
	span.SetAttributes(
		attribute.String("medspa.org_id", orgID.String()),
		attribute.String("medspa.lead_id", leadID.String()),
	)

	row, err := s.repo.CreateConfirmed(ctx, orgID, leadID, scheduledFor)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	var bookingID string
	if row.ID.Valid {
		bookingID = uuid.UUID(row.ID.Bytes).String()
	}
	s.logger.Info("booking confirmed", "org_id", orgID, "lead_id", leadID, "booking_id", bookingID)
	return row, nil
}
