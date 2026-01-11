package conversation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/payments"
	paymentsql "github.com/wolfman30/medspa-ai-platform/internal/payments/sqlc"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// depositDispatcher creates a payment intent, generates a checkout link, emits an event, and sends an SMS.
type depositDispatcher struct {
	payments   paymentIntentCreator
	checkout   paymentLinkCreator
	outbox     outboxWriter
	sms        ReplyMessenger
	numbers    payments.OrgNumberResolver
	leads      leads.Repository
	transcript *SMSTranscriptStore
	convStore  conversationWriter
	logger     *logging.Logger
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

type conversationWriter interface {
	AppendMessage(ctx context.Context, conversationID string, msg SMSTranscriptMessage) error
}

// NewDepositDispatcher wires a deposit sender with the required dependencies.
func NewDepositDispatcher(paymentsRepo paymentIntentCreator, checkout paymentLinkCreator, outbox outboxWriter, sms ReplyMessenger, numbers payments.OrgNumberResolver, leadsRepo leads.Repository, transcript *SMSTranscriptStore, convStore conversationWriter, logger *logging.Logger) DepositSender {
	if logger == nil {
		logger = logging.Default()
	}
	return &depositDispatcher{
		payments:   paymentsRepo,
		checkout:   checkout,
		outbox:     outbox,
		sms:        sms,
		numbers:    numbers,
		leads:      leadsRepo,
		transcript: transcript,
		convStore:  convStore,
		logger:     logger,
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
	// Allow deposit link without scheduled time - clinic will confirm later
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
		has, cerr := checker.HasOpenDeposit(ctx, orgUUID, leadUUID)
		if cerr != nil {
			// If we can't verify, don't send a duplicate - fail safe
			d.logger.Error("deposit: could not check for existing deposit, skipping to avoid duplicate", "error", cerr, "org_id", msg.OrgID, "lead_id", msg.LeadID)
			return fmt.Errorf("deposit: unable to verify existing deposit status: %w", cerr)
		}
		if has {
			d.logger.Info("deposit: existing deposit intent found; skipping new link", "org_id", msg.OrgID, "lead_id", msg.LeadID)
			return nil
		}
	} else {
		d.logger.Warn("deposit: payments repo does not support HasOpenDeposit check, skipping to avoid duplicate", "org_id", msg.OrgID, "lead_id", msg.LeadID)
		return fmt.Errorf("deposit: cannot verify existing deposit - payments repo missing HasOpenDeposit")
	}

	// Check if we have a preloaded checkout link (generated in parallel with LLM)
	var preloadedPaymentID uuid.UUID
	if intent.PreloadedPaymentID != "" {
		if parsed, perr := uuid.Parse(intent.PreloadedPaymentID); perr == nil {
			preloadedPaymentID = parsed
		}
	}

	// Use preloaded payment ID if available, otherwise generate new
	bookingIntentID := uuid.Nil
	if preloadedPaymentID != uuid.Nil {
		bookingIntentID = preloadedPaymentID
	}

	paymentRow, err := d.payments.CreateIntent(ctx, orgUUID, leadUUID, "square", bookingIntentID, intent.AmountCents, "deposit_pending", intent.ScheduledFor)
	if err != nil {
		return fmt.Errorf("deposit: create intent: %w", err)
	}
	if d.leads != nil {
		if err := d.leads.UpdateDepositStatus(ctx, msg.LeadID, "pending", "normal"); err != nil {
			d.logger.Warn("deposit: failed to update lead deposit status", "error", err, "org_id", msg.OrgID, "lead_id", msg.LeadID)
		}
	}
	var paymentID uuid.UUID
	if paymentRow.ID.Valid {
		paymentID = uuid.UUID(paymentRow.ID.Bytes)
	}

	// Use preloaded checkout link if available, otherwise create new
	var link *payments.CheckoutResponse
	if intent.PreloadedURL != "" {
		d.logger.Info("deposit: using preloaded checkout link (saved ~1.7s)",
			"org_id", msg.OrgID,
			"lead_id", msg.LeadID,
			"payment_id", paymentID,
		)
		link = &payments.CheckoutResponse{
			URL:        intent.PreloadedURL,
			ProviderID: "", // Preloaded links don't have provider ID available here
		}
	} else {
		link, err = d.checkout.CreatePaymentLink(ctx, payments.CheckoutParams{
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
	}

	if link.URL != "" {
		// Prefer the inbound destination number for this conversation (msg.To). This ensures
		// the deposit link is sent from the same clinic number the patient texted/called.
		// Only fall back to an org-level default when msg.To is missing (e.g. web lead flow).
		fromNumber := strings.TrimSpace(msg.To)
		if fromNumber == "" && d.numbers != nil {
			if resolved := strings.TrimSpace(d.numbers.DefaultFromNumber(msg.OrgID)); resolved != "" {
				fromNumber = resolved
			}
		}
		d.logger.Info("deposit: sending sms with checkout link",
			"to", msg.From,
			"from", fromNumber,
			"payment_id", paymentID,
		)
		body := fmt.Sprintf("To secure priority booking, please place a refundable $%.2f deposit: %s\n\nNote: This reserves your priority spot, not a confirmed time. Our team will call to finalize your exact appointment.", float64(intent.AmountCents)/100, link.URL)
		conversationID := strings.TrimSpace(resp.ConversationID)
		if conversationID == "" {
			conversationID = strings.TrimSpace(msg.ConversationID)
		}
		sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if d.sms != nil {
			reply := OutboundReply{
				OrgID:          msg.OrgID,
				LeadID:         msg.LeadID,
				ConversationID: resp.ConversationID,
				To:             msg.From,
				From:           fromNumber,
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
		} else {
			d.logger.Warn("deposit: sms messenger nil; link not sent", "org_id", msg.OrgID, "lead_id", msg.LeadID)
		}
		d.appendTranscript(context.Background(), conversationID, SMSTranscriptMessage{
			Role: "assistant",
			From: fromNumber,
			To:   msg.From,
			Body: body,
			Kind: "deposit_link",
			Metadata: map[string]string{
				"payment_id": paymentID.String(),
				"lead_id":    msg.LeadID,
			},
		})
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

func (d *depositDispatcher) appendTranscript(ctx context.Context, conversationID string, msg SMSTranscriptMessage) {
	if conversationID == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if d.transcript != nil {
		if err := d.transcript.Append(ctx, conversationID, msg); err != nil {
			d.logger.Warn("deposit: failed to append sms transcript", "error", err, "conversation_id", conversationID)
		}
	}
	if d.convStore != nil {
		if err := d.convStore.AppendMessage(ctx, conversationID, msg); err != nil {
			d.logger.Warn("deposit: failed to persist transcript", "error", err, "conversation_id", conversationID)
		}
	}
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
