package rebooking

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// Scheduler creates rebooking reminders after confirmed bookings.
type Scheduler struct {
	store  *Store
	logger *logging.Logger
}

// NewScheduler creates a rebooking scheduler.
func NewScheduler(store *Store, logger *logging.Logger) *Scheduler {
	if logger == nil {
		logger = logging.Default()
	}
	return &Scheduler{store: store, logger: logger}
}

// ScheduleInput contains the information needed to create a rebooking reminder.
type ScheduleInput struct {
	OrgID       string
	PatientID   uuid.UUID
	Phone       string
	PatientName string
	Service     string
	Provider    string
	BookedAt    time.Time
	Channel     Channel
}

// Schedule creates a rebooking reminder for a confirmed booking.
// Returns nil if the service has no known rebooking interval.
func (s *Scheduler) Schedule(ctx context.Context, input ScheduleInput) (*Reminder, error) {
	rebookDate, ok := RebookAfter(input.Service, input.BookedAt)
	if !ok {
		s.logger.Info("rebooking: no duration config for service",
			"service", input.Service,
			"org_id", input.OrgID,
		)
		return nil, nil
	}

	ch := input.Channel
	if ch == "" {
		ch = ChannelSMS
	}

	reminder := &Reminder{
		OrgID:       input.OrgID,
		PatientID:   input.PatientID,
		Phone:       input.Phone,
		PatientName: input.PatientName,
		Service:     input.Service,
		Provider:    input.Provider,
		BookedAt:    input.BookedAt,
		RebookAfter: rebookDate,
		Status:      StatusPending,
		Channel:     ch,
	}

	if err := s.store.Create(ctx, reminder); err != nil {
		return nil, fmt.Errorf("rebooking: schedule: %w", err)
	}

	s.logger.Info("rebooking: reminder scheduled",
		"id", reminder.ID,
		"org_id", input.OrgID,
		"service", input.Service,
		"rebook_after", rebookDate.Format(time.DateOnly),
	)

	return reminder, nil
}
