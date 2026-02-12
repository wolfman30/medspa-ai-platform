package archive

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"time"
)

// TrainingArchiver orchestrates classification + archival for the training pipeline.
// It is designed to be called from the purge flow before data deletion.
// Errors are logged but never block the caller.
type TrainingArchiver struct {
	store      *Store
	classifier *Classifier
	logger     *slog.Logger
}

// NewTrainingArchiver creates a TrainingArchiver. Returns nil if store is not enabled.
func NewTrainingArchiver(store *Store, classifier *Classifier, logger *slog.Logger) *TrainingArchiver {
	if store == nil || !store.Enabled() {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &TrainingArchiver{store: store, classifier: classifier, logger: logger}
}

// TrainingArchiveInput holds the data needed to archive a conversation for training.
type TrainingArchiveInput struct {
	ConversationID string
	OrgID          string
	Phone          string // raw phone for hashing + test detection
	Messages       []Message
	Outcome        string // e.g. "booking_completed", "abandoned", "purged"

	// Context fields (optional)
	ServiceRequested   string
	PatientType        string
	BookingCompleted   bool
	PaymentCompleted   bool
	DepositAmountCents int
}

// Archive classifies and archives a conversation for LLM training.
// This method never returns an error — failures are logged and swallowed
// so that the purge flow is not blocked.
func (ta *TrainingArchiver) Archive(ctx context.Context, input TrainingArchiveInput) {
	if ta == nil {
		return
	}

	ta.logger.Info("training archive: starting",
		"conversation_id", input.ConversationID,
		"message_count", len(input.Messages),
	)

	// Scrub PII from messages (copy to avoid mutating caller's slice)
	msgs := make([]Message, len(input.Messages))
	copy(msgs, input.Messages)
	ScrubMessages(msgs)

	// Classify
	var labels *Labels
	if ta.classifier != nil {
		var err error
		labels, err = ta.classifier.Classify(ctx, input.Phone, msgs)
		if err != nil {
			ta.logger.Warn("training archive: classification failed, using defaults",
				"error", err, "conversation_id", input.ConversationID)
			labels = defaultLabels()
		}
	} else {
		labels = defaultLabels()
	}

	// Calculate duration
	var durationSec int
	if len(msgs) >= 2 {
		first := msgs[0].Timestamp
		last := msgs[len(msgs)-1].Timestamp
		durationSec = int(last.Sub(first).Seconds())
	}

	record := &ConversationRecord{
		Version:         "1.0",
		ConversationID:  input.ConversationID,
		OrgID:           input.OrgID,
		PhoneHash:       hashPhoneStr(input.Phone),
		ArchivedAt:      time.Now().UTC(),
		DurationSeconds: durationSec,
		MessageCount:    len(msgs),
		Outcome:         input.Outcome,
		Labels:          *labels,
		Context: ConversationContext{
			ServiceRequested:   input.ServiceRequested,
			PatientType:        input.PatientType,
			BookingCompleted:   input.BookingCompleted,
			PaymentCompleted:   input.PaymentCompleted,
			DepositAmountCents: input.DepositAmountCents,
		},
		Messages: msgs,
	}

	if err := ta.store.ArchiveConversation(ctx, record); err != nil {
		ta.logger.Error("training archive: failed to archive",
			"error", err, "conversation_id", input.ConversationID)
		return
	}

	ta.logger.Info("training archive: completed",
		"conversation_id", input.ConversationID,
		"category", labels.ConversationCategory,
	)
}

func hashPhoneStr(phone string) string {
	h := sha256.Sum256([]byte(phone))
	return fmt.Sprintf("%x", h)
}
