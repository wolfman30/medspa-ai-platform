package conversation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	moxieclient "github.com/wolfman30/medspa-ai-platform/internal/moxie"
)

func (w *Worker) handleDepositIntent(ctx context.Context, msg MessageRequest, resp *Response) {
	if w.deposits == nil || resp == nil || resp.DepositIntent == nil {
		return
	}
	if w.isOptedOut(ctx, msg.OrgID, msg.From) {
		return
	}

	// Check for preloaded checkout link (generated in parallel with LLM call)
	if w.depositPreloader != nil {
		if preloaded := w.depositPreloader.WaitForPreloaded(msg.ConversationID, 2*time.Second); preloaded != nil {
			if preloaded.Error == nil && preloaded.URL != "" {
				resp.DepositIntent.PreloadedURL = preloaded.URL
				resp.DepositIntent.PreloadedPaymentID = preloaded.PrePaymentID.String()
				w.logger.Info("deposit: using preloaded checkout link",
					"conversation_id", msg.ConversationID,
					"preloaded_url", preloaded.URL[:min(50, len(preloaded.URL))]+"...",
				)
			}
			w.depositPreloader.ClearPreloaded(msg.ConversationID)
		}
	}

	if err := w.deposits.SendDeposit(ctx, msg, resp); err != nil {
		w.logger.Error("failed to send deposit intent", "error", err, "org_id", msg.OrgID, "lead_id", msg.LeadID)
	}
}

func (w *Worker) handlePaymentEvent(ctx context.Context, evt *events.PaymentSucceededV1) error {
	if evt == nil {
		return errors.New("conversation: missing payment payload")
	}
	idempotencyKey := strings.TrimSpace(evt.ProviderRef)
	if idempotencyKey == "" {
		idempotencyKey = strings.TrimSpace(evt.BookingIntentID)
	}
	if idempotencyKey == "" {
		idempotencyKey = strings.TrimSpace(evt.EventID)
	}
	if w.processed != nil && idempotencyKey != "" {
		already, err := w.processed.AlreadyProcessed(ctx, "conversation.payment_succeeded.v1", idempotencyKey)
		if err != nil {
			w.logger.Warn("failed to check payment event idempotency", "error", err, "key", idempotencyKey, "event_id", evt.EventID, "provider_ref", evt.ProviderRef, "org_id", evt.OrgID, "lead_id", evt.LeadID)
		} else if already {
			w.logger.Info("skipping duplicate payment success event", "key", idempotencyKey, "event_id", evt.EventID, "provider_ref", evt.ProviderRef, "org_id", evt.OrgID, "lead_id", evt.LeadID)
			return nil
		}
	}
	if w.bookings == nil {
		return nil
	}
	orgID, err := uuid.Parse(evt.OrgID)
	if err != nil {
		return fmt.Errorf("conversation: invalid org id: %w", err)
	}
	leadID, err := uuid.Parse(evt.LeadID)
	if err != nil {
		return fmt.Errorf("conversation: invalid lead id: %w", err)
	}
	if err := w.bookings.ConfirmBooking(ctx, orgID, leadID, evt.ScheduledFor); err != nil {
		return fmt.Errorf("conversation: confirm booking failed: %w", err)
	}

	// Notify clinic operators about the payment (non-blocking)
	if w.notifier != nil {
		if err := w.notifier.NotifyPaymentSuccess(ctx, *evt); err != nil {
			w.logger.Error("failed to send payment notification to clinic", "error", err, "org_id", evt.OrgID, "lead_id", evt.LeadID)
			// Don't fail the payment flow if notification fails
		}
	}

	// For Moxie+Stripe clinics: create the actual appointment on Moxie now that
	// the deposit has been collected. This is the critical "Step 4b" — without it
	// the patient pays but never gets booked.
	cfg := w.clinicConfig(ctx, evt.OrgID)
	moxieBooked := false
	var moxieConfirmMsg string
	if cfg != nil && cfg.UsesStripePayment() && cfg.UsesMoxieBooking() && w.moxieClient != nil && cfg.MoxieConfig != nil {
		moxieBooked, moxieConfirmMsg = w.createMoxieBookingAfterPayment(ctx, evt, cfg)
	}

	if evt.LeadPhone != "" && evt.FromNumber != "" {
		if !w.isOptedOut(ctx, evt.OrgID, evt.LeadPhone) {
			var body string
			if moxieBooked && moxieConfirmMsg != "" {
				body = moxieConfirmMsg
			} else {
				var clinicName, bookingURL, callbackTime, tz string
				if cfg != nil {
					clinicName = strings.TrimSpace(cfg.Name)
					bookingURL = strings.TrimSpace(cfg.BookingURL)
					callbackTime = cfg.ExpectedCallbackTime(time.Now())
					tz = cfg.Timezone
				}
				if callbackTime == "" {
					callbackTime = "within 24 hours" // fallback
				}
				// Convert scheduled time to clinic timezone for display
				if evt.ScheduledFor != nil && tz != "" {
					if loc, lerr := time.LoadLocation(tz); lerr == nil {
						localTime := evt.ScheduledFor.In(loc)
						evt.ScheduledFor = &localTime
					}
				}
				body = paymentConfirmationMessage(evt, clinicName, bookingURL, callbackTime)
			}

			if w.messenger == nil {
				// Transcript is still recorded even when SMS sending is disabled.
			} else {
				reply := OutboundReply{
					OrgID:          evt.OrgID,
					LeadID:         evt.LeadID,
					ConversationID: smsConversationID(evt.OrgID, evt.LeadPhone),
					To:             evt.LeadPhone,
					From:           evt.FromNumber,
					Body:           body,
					Metadata: map[string]string{
						"event_id": evt.EventID,
					},
				}
				sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				if err := w.messenger.SendReply(sendCtx, reply); err != nil {
					w.logger.Error("failed to send booking confirmation sms", "error", err, "event_id", evt.EventID, "org_id", evt.OrgID)
				}
			}

			w.appendTranscript(context.Background(), smsConversationID(evt.OrgID, evt.LeadPhone), SMSTranscriptMessage{
				Role: "assistant",
				From: evt.FromNumber,
				To:   evt.LeadPhone,
				Body: body,
				Kind: "payment_confirmation",
			})
		}
	}

	// Update conversation status to deposit_paid
	if w.convStore != nil && evt.LeadPhone != "" {
		if err := w.convStore.UpdateStatusByPhone(ctx, evt.OrgID, evt.LeadPhone, "deposit_paid"); err != nil {
			w.logger.Warn("failed to update conversation status to deposit_paid", "error", err, "org_id", evt.OrgID, "lead_phone", evt.LeadPhone)
		}
	}

	if w.processed != nil && idempotencyKey != "" {
		if _, err := w.processed.MarkProcessed(ctx, "conversation.payment_succeeded.v1", idempotencyKey); err != nil {
			w.logger.Warn("failed to mark payment event processed", "error", err, "key", idempotencyKey, "event_id", evt.EventID, "provider_ref", evt.ProviderRef, "org_id", evt.OrgID, "lead_id", evt.LeadID)
		}
	}
	if w.autoPurge != nil {
		if err := w.autoPurge.MaybePurgeAfterPayment(ctx, *evt); err != nil {
			w.logger.Warn("sandbox auto purge hook failed", "error", err, "org_id", evt.OrgID, "lead_id", evt.LeadID, "provider_ref", evt.ProviderRef)
		}
	}
	return nil
}

// createMoxieBookingAfterPayment creates a Moxie appointment after Stripe deposit is collected.
// Returns (booked, confirmationMessage). If booking fails, we still proceed with the
// generic payment confirmation — the clinic can manually book the patient.

func (w *Worker) createMoxieBookingAfterPayment(ctx context.Context, evt *events.PaymentSucceededV1, cfg *clinic.Config) (bool, string) {
	mc := cfg.MoxieConfig
	if mc == nil || mc.MedspaID == "" {
		w.logger.Warn("moxie booking after payment skipped: no moxie config", "org_id", evt.OrgID)
		return false, ""
	}

	// Fetch lead to get selected appointment details
	if w.leadsRepo == nil {
		w.logger.Warn("moxie booking after payment skipped: no leads repo", "org_id", evt.OrgID)
		return false, ""
	}
	lead, err := w.leadsRepo.GetByID(ctx, evt.OrgID, evt.LeadID)
	if err != nil {
		w.logger.Error("moxie booking after payment: lead fetch failed", "error", err,
			"org_id", evt.OrgID, "lead_id", evt.LeadID)
		return false, ""
	}

	// The lead must have a selected appointment (date/time + service)
	if lead.SelectedDateTime == nil {
		// Fall back to evt.ScheduledFor if available
		if evt.ScheduledFor == nil {
			w.logger.Warn("moxie booking after payment skipped: no selected appointment time",
				"org_id", evt.OrgID, "lead_id", evt.LeadID)
			return false, ""
		}
		lead.SelectedDateTime = evt.ScheduledFor
	}

	service := lead.SelectedService
	if service == "" {
		service = lead.ServiceInterest
	}
	if service == "" {
		w.logger.Warn("moxie booking after payment skipped: no service selected",
			"org_id", evt.OrgID, "lead_id", evt.LeadID)
		return false, ""
	}

	// Resolve serviceMenuItemId
	normalizedService := strings.ToLower(service)
	serviceMenuItemID := ""
	if mc.ServiceMenuItems != nil {
		serviceMenuItemID = mc.ServiceMenuItems[normalizedService]
		if serviceMenuItemID == "" {
			resolved := cfg.ResolveServiceName(normalizedService)
			serviceMenuItemID = mc.ServiceMenuItems[strings.ToLower(resolved)]
		}
	}
	if serviceMenuItemID == "" {
		w.logger.Error("moxie booking after payment: no serviceMenuItemId for service",
			"service", service, "org_id", evt.OrgID, "lead_id", evt.LeadID)
		return false, ""
	}

	// Parse start/end times from the selected datetime
	loc := ClinicLocation(cfg.Timezone)
	localTime := lead.SelectedDateTime.In(loc)
	startTime := lead.SelectedDateTime.UTC().Format(time.RFC3339)
	endTime := lead.SelectedDateTime.Add(45 * time.Minute).UTC().Format(time.RFC3339)

	providerID := mc.DefaultProviderID
	if providerID == "" {
		providerID = "no-preference"
	}

	// Split name into first/last
	firstName, lastName := splitName(lead.Name)

	w.logger.Info("creating Moxie appointment after Stripe payment",
		"org_id", evt.OrgID, "lead_id", evt.LeadID,
		"medspa_id", mc.MedspaID, "service", service,
		"start_time", startTime)

	result, err := w.moxieClient.CreateAppointment(ctx, moxieclient.CreateAppointmentRequest{
		MedspaID:  mc.MedspaID,
		FirstName: firstName,
		LastName:  lastName,
		Email:     lead.Email,
		Phone:     lead.Phone,
		Note:      fmt.Sprintf("Deposit collected via Stripe (ref: %s)", evt.ProviderRef),
		Services: []moxieclient.ServiceInput{{
			ServiceMenuItemID: serviceMenuItemID,
			ProviderID:        providerID,
			StartTime:         startTime,
			EndTime:           endTime,
		}},
		IsNewClient:              lead.PatientType != "existing",
		NoPreferenceProviderUsed: providerID == "no-preference",
	})
	if err != nil {
		w.logger.Error("Moxie API create appointment after payment failed", "error", err,
			"org_id", evt.OrgID, "lead_id", evt.LeadID)
		return false, ""
	}
	if !result.OK {
		w.logger.Error("Moxie appointment creation after payment returned not OK",
			"message", result.Message, "org_id", evt.OrgID, "lead_id", evt.LeadID)
		return false, ""
	}

	w.logger.Info("Moxie appointment created successfully after Stripe payment",
		"appointment_id", result.AppointmentID,
		"org_id", evt.OrgID, "lead_id", evt.LeadID,
		"service", service)

	// Update conversation status to booked
	if w.convStore != nil {
		if err := w.convStore.UpdateStatusByPhone(ctx, evt.OrgID, evt.LeadPhone, StatusBooked); err != nil {
			w.logger.Warn("failed to update conversation status to booked", "error", err, "org_id", evt.OrgID, "lead_phone", evt.LeadPhone)
		}
	}

	// Update lead with booking session info
	now := time.Now()
	if err := w.leadsRepo.UpdateBookingSession(ctx, evt.LeadID, leads.BookingSessionUpdate{
		SessionID:   result.AppointmentID,
		Platform:    "moxie",
		Outcome:     "success",
		CompletedAt: &now,
	}); err != nil {
		w.logger.Warn("failed to update lead with Moxie appointment after payment",
			"error", err, "lead_id", evt.LeadID, "appointment_id", result.AppointmentID)
	}

	// Build confirmation message using centralized formatter
	confirmMsg := FormatAppointmentConfirmation(service, localTime, cfg.Name)

	return true, confirmMsg
}

func paymentConfirmationMessage(evt *events.PaymentSucceededV1, clinicName, bookingURL, callbackTime string) string {
	if evt == nil {
		return ""
	}
	name := strings.TrimSpace(clinicName)
	bookingURL = strings.TrimSpace(bookingURL)
	callbackTime = strings.TrimSpace(callbackTime)
	if callbackTime == "" {
		callbackTime = "within 24 hours"
	}

	cancellationPolicy := "\n\nReminder: There is a 24-hour cancellation policy. Cancellations made less than 24 hours before your appointment are non-refundable."

	if evt.ScheduledFor != nil {
		tzAbbrev := evt.ScheduledFor.Format("MST")
		date := evt.ScheduledFor.Format("Monday, January 2 at 3:04 PM") + " " + tzAbbrev
		service := evt.ServiceName
		if service == "" {
			service = "your appointment"
		}
		if name != "" {
			return fmt.Sprintf("Payment received! Your %s appointment at %s on %s is confirmed.%s", service, name, date, cancellationPolicy)
		}
		return fmt.Sprintf("Payment received! Your %s appointment on %s is confirmed.%s", service, date, cancellationPolicy)
	}
	amount := float64(evt.AmountCents) / 100
	if name != "" {
		return fmt.Sprintf("Payment of $%.2f received - thank you! A %s team member will call you %s to confirm your appointment.%s", amount, name, callbackTime, cancellationPolicy)
	}
	return fmt.Sprintf("Payment of $%.2f received - thank you! Our team will call you %s to confirm your appointment.%s", amount, callbackTime, cancellationPolicy)
}

func (w *Worker) handlePaymentFailedEvent(ctx context.Context, evt *events.PaymentFailedV1) error {
	if evt == nil {
		return errors.New("conversation: missing payment failed payload")
	}
	idempotencyKey := strings.TrimSpace(evt.ProviderRef)
	if idempotencyKey == "" {
		idempotencyKey = strings.TrimSpace(evt.BookingIntentID)
	}
	if idempotencyKey == "" {
		idempotencyKey = strings.TrimSpace(evt.EventID)
	}
	if w.processed != nil && idempotencyKey != "" {
		already, err := w.processed.AlreadyProcessed(ctx, "conversation.payment_failed.v1", idempotencyKey)
		if err != nil {
			w.logger.Warn("failed to check payment failed event idempotency", "error", err, "key", idempotencyKey, "event_id", evt.EventID, "provider_ref", evt.ProviderRef, "org_id", evt.OrgID, "lead_id", evt.LeadID)
		} else if already {
			w.logger.Info("skipping duplicate payment failed event", "key", idempotencyKey, "event_id", evt.EventID, "provider_ref", evt.ProviderRef, "org_id", evt.OrgID, "lead_id", evt.LeadID)
			return nil
		}
	}

	if w.messenger != nil && evt.LeadPhone != "" && evt.FromNumber != "" {
		if !w.isOptedOut(ctx, evt.OrgID, evt.LeadPhone) {
			body := "Payment failed - we didn't receive your deposit. If you'd still like to book, please reply and we can send a new secure payment link. Our team can also help by phone."
			reply := OutboundReply{
				OrgID:          evt.OrgID,
				LeadID:         evt.LeadID,
				ConversationID: smsConversationID(evt.OrgID, evt.LeadPhone),
				To:             evt.LeadPhone,
				From:           evt.FromNumber,
				Body:           body,
				Metadata: map[string]string{
					"event_id": evt.EventID,
				},
			}
			sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if err := w.messenger.SendReply(sendCtx, reply); err != nil {
				w.logger.Error("failed to send payment failed sms", "error", err, "event_id", evt.EventID, "org_id", evt.OrgID)
			}
		}
	}
	if w.processed != nil && idempotencyKey != "" {
		if _, err := w.processed.MarkProcessed(ctx, "conversation.payment_failed.v1", idempotencyKey); err != nil {
			w.logger.Warn("failed to mark payment failed event processed", "error", err, "key", idempotencyKey, "event_id", evt.EventID, "provider_ref", evt.ProviderRef, "org_id", evt.OrgID, "lead_id", evt.LeadID)
		}
	}
	return nil
}
