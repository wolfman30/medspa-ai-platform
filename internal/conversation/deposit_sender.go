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

// shortURLSaver stores a checkout URL and returns a short code for redirect.
type shortURLSaver interface {
	SaveCheckoutURL(paymentID uuid.UUID, checkoutURL string) string
}

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
	apiBaseURL string // Public API base URL for short payment URLs
	shortURLs  shortURLSaver
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

// DepositOption configures optional depositDispatcher fields.
type DepositOption func(*depositDispatcher)

// WithShortURLs enables short payment URL generation.
func WithShortURLs(saver shortURLSaver, apiBaseURL string) DepositOption {
	return func(d *depositDispatcher) {
		d.shortURLs = saver
		d.apiBaseURL = apiBaseURL
	}
}

// NewDepositDispatcher wires a deposit sender with the required dependencies.
func NewDepositDispatcher(paymentsRepo paymentIntentCreator, checkout paymentLinkCreator, outbox outboxWriter, sms ReplyMessenger, numbers payments.OrgNumberResolver, leadsRepo leads.Repository, transcript *SMSTranscriptStore, convStore conversationWriter, logger *logging.Logger, opts ...DepositOption) DepositSender {
	if logger == nil {
		logger = logging.Default()
	}
	d := &depositDispatcher{
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
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// SendDeposit orchestrates the full deposit flow: validates inputs, checks for
// duplicates, creates a payment intent, generates a checkout link, sends the
// deposit SMS, and emits an outbox event.
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
	if d.payments == nil || d.checkout == nil {
		return fmt.Errorf("SendDeposit: missing payments or checkout dependency")
	}

	orgUUID, leadUUID, err := d.parseDepositIDs(msg)
	if err != nil {
		return err
	}

	isDuplicate, err := d.checkDuplicateDeposit(ctx, orgUUID, leadUUID, msg)
	if err != nil {
		return err
	}
	if isDuplicate {
		return nil
	}

	paymentID, err := d.createPaymentIntent(ctx, orgUUID, leadUUID, intent, msg)
	if err != nil {
		return err
	}

	fromNumber := d.resolveFromNumber(msg)

	link, err := d.resolveCheckoutLink(ctx, intent, msg, paymentID, fromNumber)
	if err != nil {
		return err
	}

	if link.URL != "" {
		d.sendDepositSMS(ctx, msg, resp, intent, paymentID, fromNumber, link.URL)
	}

	d.emitDepositEvent(ctx, msg, paymentID, intent, link.URL)

	return nil
}

// parseDepositIDs validates and parses org and lead IDs from the message request.
func (d *depositDispatcher) parseDepositIDs(msg MessageRequest) (uuid.UUID, uuid.UUID, error) {
	orgUUID, err := uuid.Parse(msg.OrgID)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("SendDeposit: invalid org id: %w", err)
	}
	leadUUID, err := uuid.Parse(msg.LeadID)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("SendDeposit: invalid lead id: %w", err)
	}
	return orgUUID, leadUUID, nil
}

// checkDuplicateDeposit verifies no pending/succeeded deposit already exists for this lead.
// Returns (true, nil) if a duplicate exists and the caller should stop.
func (d *depositDispatcher) checkDuplicateDeposit(ctx context.Context, orgUUID, leadUUID uuid.UUID, msg MessageRequest) (bool, error) {
	checker, ok := d.payments.(paymentIntentChecker)
	if !ok {
		d.logger.Warn("SendDeposit: payments repo does not support HasOpenDeposit check, skipping to avoid duplicate", "org_id", msg.OrgID, "lead_id", msg.LeadID)
		return false, fmt.Errorf("SendDeposit: cannot verify existing deposit - payments repo missing HasOpenDeposit")
	}
	has, err := checker.HasOpenDeposit(ctx, orgUUID, leadUUID)
	if err != nil {
		d.logger.Error("SendDeposit: could not check for existing deposit, skipping to avoid duplicate", "error", err, "org_id", msg.OrgID, "lead_id", msg.LeadID)
		return false, fmt.Errorf("SendDeposit: unable to verify existing deposit status: %w", err)
	}
	if has {
		d.logger.Info("SendDeposit: existing deposit intent found; skipping new link", "org_id", msg.OrgID, "lead_id", msg.LeadID)
		return true, nil
	}
	return false, nil
}

// createPaymentIntent records a deposit intent in the payments store and updates the lead status.
func (d *depositDispatcher) createPaymentIntent(ctx context.Context, orgUUID, leadUUID uuid.UUID, intent *DepositIntent, msg MessageRequest) (uuid.UUID, error) {
	bookingIntentID := uuid.Nil
	if intent.PreloadedPaymentID != "" {
		if parsed, err := uuid.Parse(intent.PreloadedPaymentID); err == nil {
			bookingIntentID = parsed
		}
	}

	paymentRow, err := d.payments.CreateIntent(ctx, orgUUID, leadUUID, "square", bookingIntentID, intent.AmountCents, "deposit_pending", intent.ScheduledFor)
	if err != nil {
		return uuid.Nil, fmt.Errorf("SendDeposit: create intent: %w", err)
	}

	if d.leads != nil {
		if err := d.leads.UpdateDepositStatus(ctx, msg.LeadID, "pending", "normal"); err != nil {
			d.logger.Warn("SendDeposit: failed to update lead deposit status", "error", err, "org_id", msg.OrgID, "lead_id", msg.LeadID)
		}
	}

	var paymentID uuid.UUID
	if paymentRow.ID.Valid {
		paymentID = uuid.UUID(paymentRow.ID.Bytes)
	}
	return paymentID, nil
}

// resolveFromNumber determines the SMS "from" number. It prefers the inbound
// destination (msg.To) so the deposit link comes from the same number the patient
// texted, falling back to an org-level default.
func (d *depositDispatcher) resolveFromNumber(msg MessageRequest) string {
	fromNumber := strings.TrimSpace(msg.To)
	if fromNumber == "" && d.numbers != nil {
		if resolved := strings.TrimSpace(d.numbers.DefaultFromNumber(msg.OrgID)); resolved != "" {
			fromNumber = resolved
		}
	}
	return fromNumber
}

// resolveCheckoutLink returns a preloaded checkout link if available, otherwise
// creates a new one through the payment provider.
func (d *depositDispatcher) resolveCheckoutLink(ctx context.Context, intent *DepositIntent, msg MessageRequest, paymentID uuid.UUID, fromNumber string) (*payments.CheckoutResponse, error) {
	if intent.PreloadedURL != "" {
		d.logger.Info("SendDeposit: using preloaded checkout link (saved ~1.7s)",
			"org_id", msg.OrgID,
			"lead_id", msg.LeadID,
			"payment_id", paymentID,
		)
		return &payments.CheckoutResponse{URL: intent.PreloadedURL}, nil
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
		FromNumber:      fromNumber,
	})
	if err != nil {
		return nil, fmt.Errorf("SendDeposit: create checkout link: %w", err)
	}
	d.logger.Info("SendDeposit: link created",
		"org_id", msg.OrgID,
		"lead_id", msg.LeadID,
		"amount_cents", intent.AmountCents,
		"payment_id", paymentID,
		"provider_link_id", link.ProviderID,
	)
	return link, nil
}

// buildDepositSMSBody constructs the deposit SMS text including amount, policies, and checkout URL.
func buildDepositSMSBody(intent *DepositIntent, checkoutURL string) string {
	amount := fmt.Sprintf("$%.2f", float64(intent.AmountCents)/100)
	explainer := fmt.Sprintf("💳 %s deposit — applies toward your treatment cost and secures your spot.\n\n⚠️ Deposits are forfeited for no-shows or late cancellations.", amount)

	if len(intent.BookingPolicies) > 0 {
		var sb strings.Builder
		sb.WriteString(explainer)
		sb.WriteString("\n\n📋 Booking policies:\n")
		for _, policy := range intent.BookingPolicies {
			sb.WriteString("  ✅ ")
			sb.WriteString(policy)
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("\n→ Complete your deposit here:\n%s", checkoutURL))
		return sb.String()
	}
	return fmt.Sprintf("%s\n\n→ Complete your deposit here:\n%s", explainer, checkoutURL)
}

// sendDepositSMS builds the deposit message, sends it via SMS, and records the transcript.
func (d *depositDispatcher) sendDepositSMS(ctx context.Context, msg MessageRequest, resp *Response, intent *DepositIntent, paymentID uuid.UUID, fromNumber, rawURL string) {
	checkoutURL := rawURL
	if d.shortURLs != nil && d.apiBaseURL != "" {
		code := d.shortURLs.SaveCheckoutURL(paymentID, rawURL)
		checkoutURL = fmt.Sprintf("%s/pay/%s", strings.TrimRight(d.apiBaseURL, "/"), code)
	}

	body := buildDepositSMSBody(intent, checkoutURL)

	conversationID := strings.TrimSpace(resp.ConversationID)
	if conversationID == "" {
		conversationID = strings.TrimSpace(msg.ConversationID)
	}

	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	d.logger.Info("SendDeposit: sending sms with checkout link",
		"to", msg.From,
		"from", fromNumber,
		"payment_id", paymentID,
	)

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
			d.logger.Error("SendDeposit: failed to send sms", "error", err, "org_id", msg.OrgID, "lead_id", msg.LeadID)
		} else {
			d.logger.Info("SendDeposit: sms sent", "to", msg.From, "payment_id", paymentID)
		}
	} else {
		d.logger.Warn("SendDeposit: sms messenger nil; link not sent", "org_id", msg.OrgID, "lead_id", msg.LeadID)
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

// emitDepositEvent publishes a deposit.requested event to the outbox for downstream consumers.
func (d *depositDispatcher) emitDepositEvent(ctx context.Context, msg MessageRequest, paymentID uuid.UUID, intent *DepositIntent, checkoutURL string) {
	if d.outbox == nil {
		return
	}
	event := events.DepositRequestedV1{
		EventID:         uuid.NewString(),
		OrgID:           msg.OrgID,
		LeadID:          msg.LeadID,
		AmountCents:     int64(intent.AmountCents),
		BookingIntentID: paymentID.String(),
		RequestedAt:     time.Now().UTC(),
		CheckoutURL:     checkoutURL,
		Provider:        "square",
	}
	if _, err := d.outbox.Insert(ctx, msg.OrgID, "payments.deposit.requested.v1", event); err != nil {
		d.logger.Warn("SendDeposit: failed to enqueue outbox event", "error", err, "org_id", msg.OrgID, "lead_id", msg.LeadID)
	}
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
			d.logger.Warn("SendDeposit: failed to append sms transcript", "error", err, "conversation_id", conversationID)
		}
	}
	if d.convStore != nil {
		if err := d.convStore.AppendMessage(ctx, conversationID, msg); err != nil {
			d.logger.Warn("SendDeposit: failed to persist transcript", "error", err, "conversation_id", conversationID)
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
