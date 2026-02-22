package rebooking

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// SMSSender abstracts outbound SMS sending.
type SMSSender interface {
	SendSMS(ctx context.Context, from, to, body string) error
}

// ClinicConfigProvider retrieves clinic config for outbound messaging.
type ClinicConfigProvider interface {
	GetClinicName(ctx context.Context, orgID string) (string, error)
	GetSMSFrom(ctx context.Context, orgID string) (string, error)
}

// Worker processes due rebooking reminders and sends outreach messages.
type Worker struct {
	store   *Store
	sender  SMSSender
	clinics ClinicConfigProvider
	logger  *logging.Logger
}

// NewWorker creates a rebooking worker.
func NewWorker(store *Store, sender SMSSender, clinics ClinicConfigProvider, logger *logging.Logger) *Worker {
	if logger == nil {
		logger = logging.Default()
	}
	return &Worker{store: store, sender: sender, clinics: clinics, logger: logger}
}

// ProcessDue finds all pending reminders that are due and sends outreach messages.
// Returns the number of reminders processed.
func (w *Worker) ProcessDue(ctx context.Context) (int, error) {
	now := time.Now().UTC()
	reminders, err := w.store.ListDue(ctx, now)
	if err != nil {
		return 0, fmt.Errorf("rebooking worker: list due: %w", err)
	}

	if len(reminders) == 0 {
		return 0, nil
	}

	w.logger.Info("rebooking worker: processing due reminders", "count", len(reminders))

	processed := 0
	for i := range reminders {
		r := &reminders[i]
		if err := w.processOne(ctx, r); err != nil {
			w.logger.Error("rebooking worker: failed to process reminder",
				"id", r.ID, "error", err)
			continue
		}
		processed++
	}

	return processed, nil
}

func (w *Worker) processOne(ctx context.Context, r *Reminder) error {
	clinicName, err := w.clinics.GetClinicName(ctx, r.OrgID)
	if err != nil {
		return fmt.Errorf("get clinic name: %w", err)
	}

	from, err := w.clinics.GetSMSFrom(ctx, r.OrgID)
	if err != nil {
		return fmt.Errorf("get sms from: %w", err)
	}

	msg := MessageTemplate(r, clinicName)

	if err := w.sender.SendSMS(ctx, from, r.Phone, msg); err != nil {
		return fmt.Errorf("send sms: %w", err)
	}

	if err := w.store.MarkSent(ctx, r.ID); err != nil {
		return fmt.Errorf("mark sent: %w", err)
	}

	w.logger.Info("rebooking worker: reminder sent",
		"id", r.ID, "phone", r.Phone, "service", r.Service)
	return nil
}

// HandleReply processes an inbound patient reply to a rebooking outreach.
// Returns (action, reminder, error) where action is "rebook", "dismiss", or "" (not a rebooking reply).
func (w *Worker) HandleReply(ctx context.Context, orgID, phone, body string) (string, *Reminder, error) {
	reminder, err := w.store.FindSentByPhone(ctx, orgID, phone)
	if err != nil {
		return "", nil, err
	}
	if reminder == nil {
		return "", nil, nil // No active rebooking reminder for this patient
	}

	normalized := strings.ToLower(strings.TrimSpace(body))

	// Opt-out
	if isOptOut(normalized) {
		if err := w.store.Dismiss(ctx, reminder.ID); err != nil {
			return "", nil, fmt.Errorf("dismiss: %w", err)
		}
		w.logger.Info("rebooking: patient opted out", "id", reminder.ID, "phone", phone)
		return "dismiss", reminder, nil
	}

	// Rebook
	if isRebookConfirm(normalized) {
		// Don't mark booked yet — that happens after actual booking completes.
		// Just signal that the patient wants to rebook.
		w.logger.Info("rebooking: patient wants to rebook", "id", reminder.ID, "phone", phone)
		return "rebook", reminder, nil
	}

	return "", nil, nil
}

func isOptOut(body string) bool {
	optOuts := []string{"stop", "no thanks", "no thank you", "not interested", "unsubscribe", "no"}
	for _, opt := range optOuts {
		if body == opt {
			return true
		}
	}
	return false
}

func isRebookConfirm(body string) bool {
	confirms := []string{"yes", "yeah", "yep", "sure", "ok", "okay", "yes please", "book", "schedule"}
	for _, c := range confirms {
		if body == c {
			return true
		}
	}
	return false
}
