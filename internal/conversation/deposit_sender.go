package conversation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/payments"
	paymentsql "github.com/wolfman30/medspa-ai-platform/internal/payments/sqlc"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// depositDispatcher creates a payment intent, generates a checkout link, emits an event, and sends an SMS.
type depositDispatcher struct {
	payments paymentIntentCreator
	checkout paymentLinkCreator
	outbox   outboxWriter
	sms      ReplyMessenger
	logger   *logging.Logger
}

type outboxWriter interface {
	Insert(ctx context.Context, orgID string, eventType string, payload any) (uuid.UUID, error)
}

type paymentIntentCreator interface {
	CreateIntent(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID, provider string, bookingIntent uuid.UUID, amountCents int32, status string, scheduledFor *time.Time) (*paymentsql.Payment, error)
}

type paymentLinkCreator interface {
	CreatePaymentLink(ctx context.Context, params payments.CheckoutParams) (*payments.CheckoutResponse, error)
}

type paymentIntentChecker interface {
	HasOpenDeposit(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID) (bool, error)
}

// NewDepositDispatcher wires a deposit sender with the required dependencies.
func NewDepositDispatcher(paymentsRepo paymentIntentCreator, checkout paymentLinkCreator, outbox outboxWriter, sms ReplyMessenger, logger *logging.Logger) DepositSender {
	if logger == nil {
		logger = logging.Default()
	}
	return &depositDispatcher{
		payments: paymentsRepo,
		checkout: checkout,
		outbox:   outbox,
		sms:      sms,
		logger:   logger,
	}
}

func (d *depositDispatcher) SendDeposit(ctx context.Context, msg MessageRequest, resp *Response) error {
	if resp == nil || resp.DepositIntent == nil {
		return nil
	}
	intent := resp.DepositIntent
	if intent.ScheduledFor == nil {
		if scheduled := scheduledFromMetadata(msg.Metadata); scheduled != nil {
			intent.ScheduledFor = scheduled
		}
	}
	if intent.ScheduledFor == nil {
		// Require a confirmed slot before sending a deposit link.
		d.logger.Info("deposit: skipping link because scheduled time missing", "org_id", msg.OrgID, "lead_id", msg.LeadID)
		return nil
	}
	if d.payments == nil || d.checkout == nil {
		return fmt.Errorf("deposit: missing payments or checkout dependency")
	}

	orgUUID, err := uuid.Parse(msg.OrgID)
	if err != nil {
		return fmt.Errorf("deposit: invalid org id: %w", err)
	}
	leadUUID, err := uuid.Parse(msg.LeadID)
	if err != nil {
		return fmt.Errorf("deposit: invalid lead id: %w", err)
	}

	// Avoid duplicate deposits if a pending/succeeded intent already exists.
	if checker, ok := d.payments.(paymentIntentChecker); ok {
		if has, cerr := checker.HasOpenDeposit(ctx, orgUUID, leadUUID); cerr != nil {
			return fmt.Errorf("deposit: check existing intent: %w", cerr)
		} else if has {
			d.logger.Info("deposit: existing deposit intent found; skipping new link", "org_id", msg.OrgID, "lead_id", msg.LeadID)
			return nil
		}
	}

	paymentRow, err := d.payments.CreateIntent(ctx, orgUUID, leadUUID, "square", uuid.Nil, intent.AmountCents, "deposit_pending", intent.ScheduledFor)
	if err != nil {
		return fmt.Errorf("deposit: create intent: %w", err)
	}
	var paymentID uuid.UUID
	if paymentRow.ID.Valid {
		paymentID = uuid.UUID(paymentRow.ID.Bytes)
	}

	link, err := d.checkout.CreatePaymentLink(ctx, payments.CheckoutParams{
		OrgID:           msg.OrgID,
		LeadID:          msg.LeadID,
		AmountCents:     intent.AmountCents,
		BookingIntentID: paymentID,
		Description:     defaultString(intent.Description, "Appointment deposit"),
		SuccessURL:      intent.SuccessURL,
		CancelURL:       intent.CancelURL,
		ScheduledFor:    intent.ScheduledFor,
	})
	if err != nil {
		return fmt.Errorf("deposit: create checkout link: %w", err)
	}
	d.logger.Info("deposit: link created",
		"org_id", msg.OrgID,
		"lead_id", msg.LeadID,
		"amount_cents", intent.AmountCents,
		"payment_id", paymentID,
		"provider_link_id", link.ProviderID,
	)

	if d.sms != nil && link.URL != "" {
		d.logger.Info("deposit: sending sms with checkout link",
			"to", msg.From,
			"from", msg.To,
			"payment_id", paymentID,
		)
		body := fmt.Sprintf("Please secure your spot with a deposit: %s", link.URL)
		sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		reply := OutboundReply{
			OrgID:          msg.OrgID,
			LeadID:         msg.LeadID,
			ConversationID: resp.ConversationID,
			To:             msg.From,
			From:           msg.To,
			Body:           body,
			Metadata: map[string]string{
				"provider":   "square",
				"payment_id": paymentID.String(),
			},
		}
		if err := d.sms.SendReply(sendCtx, reply); err != nil {
			d.logger.Error("deposit: failed to send sms", "error", err, "org_id", msg.OrgID, "lead_id", msg.LeadID)
		} else {
			d.logger.Info("deposit: sms sent", "to", msg.From, "payment_id", paymentID)
		}
	} else if link.URL != "" && d.sms == nil {
		d.logger.Warn("deposit: sms messenger nil; link not sent", "org_id", msg.OrgID, "lead_id", msg.LeadID)
	}

	if d.outbox != nil {
		event := events.DepositRequestedV1{
			EventID:         uuid.NewString(),
			OrgID:           msg.OrgID,
			LeadID:          msg.LeadID,
			AmountCents:     int64(intent.AmountCents),
			BookingIntentID: paymentID.String(),
			RequestedAt:     time.Now().UTC(),
			CheckoutURL:     link.URL,
			Provider:        "square",
		}
		if _, err := d.outbox.Insert(ctx, msg.OrgID, "payments.deposit.requested.v1", event); err != nil {
			d.logger.Warn("deposit: failed to enqueue outbox event", "error", err, "org_id", msg.OrgID, "lead_id", msg.LeadID)
		}
	}

	return nil
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func scheduledFromMetadata(meta map[string]string) *time.Time {
	if len(meta) == 0 {
		return nil
	}
	for _, key := range []string{"scheduled_for", "scheduledFor"} {
		if raw, ok := meta[key]; ok {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			if when, err := time.Parse(time.RFC3339, raw); err == nil {
				return &when
			}
		}
	}
	return nil
}
