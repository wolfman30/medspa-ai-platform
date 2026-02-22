package conversation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/browser"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	moxieclient "github.com/wolfman30/medspa-ai-platform/internal/moxie"
)

func (w *Worker) handleMoxieBooking(ctx context.Context, msg MessageRequest, req *BookingRequest) {
	if req == nil {
		return
	}

	// Check if clinic uses Stripe for payments — if so, send Stripe Checkout link
	// instead of Moxie sidecar URL. After payment, handlePaymentEvent will call
	// createMoxieBookingAfterPayment to book via Moxie API.
	if w.deposits != nil && w.clinicStore != nil {
		cfg, err := w.clinicStore.Get(ctx, req.OrgID)
		if err == nil && cfg != nil && cfg.UsesStripePayment() {
			w.logger.Info("moxie booking: routing to Stripe Checkout (payment_provider=stripe)",
				"org_id", req.OrgID, "lead_id", req.LeadID, "service", req.Service)
			// Parse booking date/time into a time.Time for the deposit intent
			var scheduledFor *time.Time
			if req.Date != "" && req.Time != "" {
				loc, _ := time.LoadLocation(cfg.Timezone)
				if loc == nil {
					loc = time.UTC
				}
				if parsed, perr := time.ParseInLocation("2006-01-02 3:04pm", req.Date+" "+strings.ToLower(req.Time), loc); perr == nil {
					scheduledFor = &parsed
				}
			}
			desc := req.Service
			if scheduledFor != nil {
				desc = fmt.Sprintf("%s - %s", req.Service, scheduledFor.Format("Mon Jan 2 at 3:04 PM"))
			}
			resp := &Response{
				DepositIntent: &DepositIntent{
					AmountCents:     int32(cfg.DepositAmountForService(req.Service)),
					Description:     desc,
					ScheduledFor:    scheduledFor,
					BookingPolicies: cfg.BookingPolicies,
				},
			}
			if err := w.deposits.SendDeposit(ctx, msg, resp); err != nil {
				w.logger.Error("failed to send Stripe checkout for Moxie booking",
					"error", err, "org_id", req.OrgID, "lead_id", req.LeadID)
			}
			return
		}
	}

	// Fallback: use browser sidecar for Moxie checkout URL handoff
	if w.browserBooking == nil {
		w.logger.Warn("booking request received but no booking client configured",
			"org_id", req.OrgID, "lead_id", req.LeadID)
		return
	}
	w.handleMoxieBookingSidecar(ctx, msg, req)
}

// handleMoxieBookingDirect creates a Moxie appointment via their GraphQL API.

func (w *Worker) handleMoxieBookingDirect(ctx context.Context, msg MessageRequest, req *BookingRequest, cfg *clinic.Config) {
	mc := cfg.MoxieConfig
	w.logger.Info("creating Moxie appointment via direct API",
		"org_id", req.OrgID, "lead_id", req.LeadID,
		"medspa_id", mc.MedspaID, "service", req.Service)

	// Resolve serviceMenuItemId from service name
	serviceMenuItemID := ""
	normalizedService := strings.ToLower(req.Service)
	if mc.ServiceMenuItems != nil {
		serviceMenuItemID = mc.ServiceMenuItems[normalizedService]
		// Try alias resolution
		if serviceMenuItemID == "" {
			resolved := cfg.ResolveServiceName(normalizedService)
			serviceMenuItemID = mc.ServiceMenuItems[strings.ToLower(resolved)]
		}
	}
	if serviceMenuItemID == "" {
		w.logger.Error("no Moxie serviceMenuItemId for service",
			"service", req.Service, "org_id", req.OrgID)
		w.sendBookingFallbackSMS(ctx, msg, "We couldn't find that service in our booking system. Please call the clinic directly to book your appointment.")
		return
	}

	// Parse the selected time slot to get start/end times in UTC
	// req.Date is YYYY-MM-DD, req.Time is e.g. "7:15 PM"
	startTime, endTime, err := w.parseMoxieTimeSlot(req.Date, req.Time, cfg.Timezone)
	if err != nil {
		w.logger.Error("failed to parse time slot for Moxie booking",
			"error", err, "date", req.Date, "time", req.Time)
		w.sendBookingFallbackSMS(ctx, msg, "We had trouble with the appointment time. Please try again or call the clinic directly.")
		return
	}

	// Determine provider ID
	providerID := mc.DefaultProviderID
	if providerID == "" {
		providerID = "no-preference"
	}

	// Create the appointment
	result, err := w.moxieClient.CreateAppointment(ctx, moxieclient.CreateAppointmentRequest{
		MedspaID:  mc.MedspaID,
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Email:     req.Email,
		Phone:     req.Phone,
		Note:      "",
		Services: []moxieclient.ServiceInput{{
			ServiceMenuItemID: serviceMenuItemID,
			ProviderID:        providerID,
			StartTime:         startTime,
			EndTime:           endTime,
		}},
		IsNewClient:              true, // Assume new client for SMS leads
		NoPreferenceProviderUsed: providerID == "no-preference",
	})
	if err != nil {
		w.logger.Error("Moxie API create appointment failed", "error", err,
			"org_id", req.OrgID, "lead_id", req.LeadID)
		w.sendBookingFallbackSMS(ctx, msg, "We're having trouble booking your appointment right now. Please try again in a moment or call the clinic directly.")
		return
	}

	if !result.OK {
		w.logger.Error("Moxie appointment creation returned not OK",
			"message", result.Message, "org_id", req.OrgID, "lead_id", req.LeadID)
		w.sendBookingFallbackSMS(ctx, msg, "We're having trouble booking your appointment right now. Please try again in a moment or call the clinic directly.")
		return
	}

	w.logger.Info("Moxie appointment created successfully via API",
		"appointment_id", result.AppointmentID,
		"org_id", req.OrgID, "lead_id", req.LeadID,
		"service", req.Service, "date", req.Date, "time", req.Time)

	// Update conversation status to booked
	if w.convStore != nil {
		if err := w.convStore.UpdateStatus(ctx, msg.ConversationID, StatusBooked); err != nil {
			w.logger.Warn("failed to update conversation status to booked", "error", err, "conversation_id", msg.ConversationID)
		}
	}

	// Send confirmation SMS
	confirmMsg := fmt.Sprintf("Your appointment has been booked! 🎉\n\n📋 %s\n📅 %s at %s\n📍 %s\n\nYou'll receive a confirmation from the clinic shortly. See you then!",
		req.Service, req.Date, req.Time, cfg.Name)
	if w.messenger != nil {
		reply := OutboundReply{
			OrgID:          msg.OrgID,
			LeadID:         msg.LeadID,
			ConversationID: msg.ConversationID,
			To:             msg.From,
			From:           msg.To,
			Body:           confirmMsg,
		}
		if err := w.messenger.SendReply(ctx, reply); err != nil {
			w.logger.Error("failed to send booking confirmation SMS", "error", err,
				"org_id", req.OrgID, "appointment_id", result.AppointmentID)
		}
	}

	// Update lead with appointment ID
	if w.leadsRepo != nil && req.LeadID != "" {
		now := time.Now()
		if err := w.leadsRepo.UpdateBookingSession(ctx, req.LeadID, leads.BookingSessionUpdate{
			SessionID:     result.AppointmentID,
			Platform:      "moxie",
			HandoffSentAt: &now,
		}); err != nil {
			w.logger.Warn("failed to update lead with appointment ID", "error", err,
				"lead_id", req.LeadID, "appointment_id", result.AppointmentID)
		}
	}

	// Record to transcript + DB
	w.appendTranscript(ctx, msg.ConversationID, SMSTranscriptMessage{
		Role:      "assistant",
		Body:      confirmMsg,
		Timestamp: time.Now(),
	})
	if w.convStore != nil {
		_ = w.convStore.AppendMessage(ctx, msg.ConversationID, SMSTranscriptMessage{
			Role:      "assistant",
			Body:      confirmMsg,
			Timestamp: time.Now(),
		})
	}
}

func (w *Worker) parseMoxieTimeSlot(date, timeStr, timezone string) (string, string, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}

	// Parse "7:15 PM" or "7:15pm" style
	timeStr = strings.TrimSpace(strings.ToUpper(timeStr))
	timeStr = strings.Replace(timeStr, ".", "", -1) // remove dots from "P.M."

	// Try common formats
	var t time.Time
	for _, fmt := range []string{"3:04 PM", "3:04PM", "15:04"} {
		t, err = time.Parse(fmt, timeStr)
		if err == nil {
			break
		}
	}
	if err != nil {
		return "", "", fmt.Errorf("parse time %q: %w", timeStr, err)
	}

	// Parse date
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return "", "", fmt.Errorf("parse date %q: %w", date, err)
	}

	// Combine date + time in clinic timezone
	start := time.Date(d.Year(), d.Month(), d.Day(), t.Hour(), t.Minute(), 0, 0, loc)
	end := start.Add(45 * time.Minute) // Default 45 min appointment

	return start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339), nil
}

func (w *Worker) handleMoxieBookingSidecar(ctx context.Context, msg MessageRequest, req *BookingRequest) {
	if w.browserBooking == nil {
		w.logger.Warn("booking request received but no browser booking client configured",
			"org_id", req.OrgID, "lead_id", req.LeadID)
		return
	}

	// Step 1: Start the booking session on the sidecar
	startReq := browser.BookingStartRequest{
		BookingURL: req.BookingURL,
		Date:       req.Date,
		Time:       req.Time,
		Lead: browser.BookingLeadInfo{
			FirstName: req.FirstName,
			LastName:  req.LastName,
			Phone:     req.Phone,
			Email:     req.Email,
		},
		Service:     req.Service,
		Provider:    req.Provider,
		CallbackURL: req.CallbackURL,
	}

	startResp, err := w.browserBooking.StartBookingSession(ctx, startReq)
	if err != nil {
		w.logger.Error("failed to start Moxie booking session", "error", err,
			"org_id", req.OrgID, "lead_id", req.LeadID, "booking_url", req.BookingURL)
		w.sendBookingFallbackSMS(ctx, msg, "We're having trouble starting your booking right now. Please try again in a moment or call the clinic directly.")
		return
	}
	if !startResp.Success {
		w.logger.Error("Moxie booking session start failed", "error", startResp.Error,
			"org_id", req.OrgID, "lead_id", req.LeadID)
		w.sendBookingFallbackSMS(ctx, msg, "We're having trouble starting your booking right now. Please try again in a moment or call the clinic directly.")
		return
	}

	sessionID := startResp.SessionID
	w.logger.Info("Moxie booking session started", "session_id", sessionID,
		"org_id", req.OrgID, "lead_id", req.LeadID)

	// Step 2: Update lead with session ID
	if w.leadsRepo != nil && req.LeadID != "" {
		if err := w.leadsRepo.UpdateBookingSession(ctx, req.LeadID, leads.BookingSessionUpdate{
			SessionID: sessionID,
			Platform:  "moxie",
		}); err != nil {
			w.logger.Warn("failed to update lead with booking session", "error", err,
				"lead_id", req.LeadID, "session_id", sessionID)
		}
	}

	// Step 3: Poll for handoff URL (every 2s, up to 90s)
	var handoffURL string
	pollCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-pollCtx.Done():
			w.logger.Warn("Moxie booking handoff URL timed out", "session_id", sessionID,
				"org_id", req.OrgID, "lead_id", req.LeadID)
			_ = w.browserBooking.CancelBookingSession(ctx, sessionID)
			w.sendBookingFallbackSMS(ctx, msg, "We're having trouble completing your booking right now. Please try again in a moment or call the clinic directly.")
			return
		case <-ticker.C:
			resp, err := w.browserBooking.GetHandoffURL(pollCtx, sessionID)
			if err != nil {
				w.logger.Debug("handoff URL poll error", "error", err, "session_id", sessionID)
				continue
			}
			if resp.Success && resp.HandoffURL != "" {
				handoffURL = resp.HandoffURL
				goto gotHandoff
			}
		}
	}

gotHandoff:
	w.logger.Info("Moxie booking handoff URL received", "session_id", sessionID,
		"handoff_url", handoffURL[:min(50, len(handoffURL))], "org_id", req.OrgID)

	handoffMsg := fmt.Sprintf("Your booking is almost complete! Tap the link below to enter your payment info and finalize your appointment:\n%s", handoffURL)
	if w.messenger != nil {
		reply := OutboundReply{
			OrgID:          msg.OrgID,
			LeadID:         msg.LeadID,
			ConversationID: msg.ConversationID,
			To:             msg.From,
			From:           msg.To,
			Body:           handoffMsg,
		}
		if err := w.messenger.SendReply(ctx, reply); err != nil {
			w.logger.Error("failed to send booking handoff SMS", "error", err,
				"session_id", sessionID, "org_id", req.OrgID)
		}
	}

	// Update lead with handoff URL
	if w.leadsRepo != nil && req.LeadID != "" {
		now := time.Now()
		if err := w.leadsRepo.UpdateBookingSession(ctx, req.LeadID, leads.BookingSessionUpdate{
			HandoffURL:    handoffURL,
			HandoffSentAt: &now,
		}); err != nil {
			w.logger.Warn("failed to update lead with handoff URL", "error", err,
				"lead_id", req.LeadID, "session_id", sessionID)
		}
	}

	// Record to transcript
	w.appendTranscript(ctx, msg.ConversationID, SMSTranscriptMessage{
		Role:      "assistant",
		From:      msg.To,
		To:        msg.From,
		Body:      handoffMsg,
		Timestamp: time.Now(),
		Kind:      "booking_handoff",
	})
}

func (w *Worker) shouldUseManualHandoff(ctx context.Context, orgID string) bool {
	if w.manualHandoff == nil {
		return false
	}
	cfg := w.clinicConfig(ctx, orgID)
	if cfg == nil {
		return false
	}
	return cfg.UsesManualHandoff()
}

// handleManualHandoff creates a qualified lead summary and notifies the clinic
// instead of sending a deposit/payment link. Used for non-Moxie clinics that
