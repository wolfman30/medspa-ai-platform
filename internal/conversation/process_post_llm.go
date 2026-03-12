package conversation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
)

// handlePostLLMResponse handles everything after the LLM reply: deposit flow,
// preference extraction, time selection triggering, booking request assembly.
func (s *LLMService) handlePostLLMResponse(ctx context.Context, pc *processContext) {
	pc.depositIntent = s.handleDepositFlow(ctx, pc.history)

	// Extract and save scheduling preferences
	if pc.req.LeadID != "" && s.leadsRepo != nil {
		if err := s.extractAndSavePreferences(ctx, pc.req.LeadID, pc.history); err != nil {
			s.logger.Warn("failed to save scheduling preferences", "lead_id", pc.req.LeadID, "error", err)
		}
		if email := ExtractEmailFromHistory(pc.history); email != "" {
			if err := s.leadsRepo.UpdateEmail(ctx, pc.req.LeadID, email); err != nil {
				s.logger.Warn("failed to save email", "lead_id", pc.req.LeadID, "error", err)
			}
		}
	}

	// Load clinic config for post-response decisions
	var usesMoxie bool
	var clinicCfg *clinic.Config
	if s.clinicStore != nil && pc.req.OrgID != "" {
		if cfg, err := s.clinicStore.Get(ctx, pc.req.OrgID); err == nil && cfg != nil {
			clinicCfg = cfg
			usesMoxie = cfg.UsesMoxieBooking() || cfg.UsesBoulevardBooking()
		}
	}

	// Enforce clinic-configured deposit amounts for Square clinics
	if pc.depositIntent != nil && clinicCfg != nil && !usesMoxie {
		if prefs, ok := extractPreferences(pc.history, serviceAliasesFromConfig(clinicCfg)); ok && prefs.ServiceInterest != "" {
			if amount := clinicCfg.DepositAmountForService(prefs.ServiceInterest); amount > 0 {
				pc.depositIntent.AmountCents = int32(amount)
			}
		}
	}

	// GUARD: Booking API clinics — no deposit before time selection
	if pc.depositIntent != nil && usesMoxie && (pc.timeSelectionState == nil || !pc.timeSelectionState.SlotSelected) {
		s.logger.Warn("deposit intent suppressed: booking API clinic requires time selection before deposit",
			"conversation_id", pc.req.ConversationID,
			"slot_selected", pc.timeSelectionState != nil && pc.timeSelectionState.SlotSelected,
		)
		pc.depositIntent = nil
	}

	// Time selection triggering
	s.maybeTriggerTimeSelection(ctx, pc, clinicCfg, usesMoxie)

	// Replace LLM reply in history when time selection takes over
	if pc.timeSelectionResponse != nil && pc.timeSelectionResponse.SMSMessage != "" {
		for i := len(pc.history) - 1; i >= 0; i-- {
			if pc.history[i].Role == ChatRoleAssistant {
				pc.history[i].Content = pc.timeSelectionResponse.SMSMessage
				break
			}
		}
		if err := s.history.Save(ctx, pc.req.ConversationID, pc.history); err != nil {
			s.logger.Warn("failed to re-save history after time selection", "error", err)
		}
		pc.timeSelectionResponse.SavedToHistory = true
	}

	// Clear Square deposit for booking API clinics (they use their own payment flow)
	if usesMoxie && pc.depositIntent != nil {
		s.logger.Info("clinic uses booking API - skipping Square deposit intent", "org_id", pc.req.OrgID)
		pc.depositIntent = nil
	}

	// Booking request assembly for Moxie clinics
	s.assembleBookingRequest(ctx, pc, clinicCfg, usesMoxie)
}

// maybeTriggerTimeSelection checks whether to fetch and present available time slots.
func (s *LLMService) maybeTriggerTimeSelection(ctx context.Context, pc *processContext, clinicCfg *clinic.Config, usesMoxie bool) {
	moxieAPIReady := s.moxieClient != nil && clinicCfg != nil && clinicCfg.MoxieConfig != nil
	boulevardReady := s.boulevardAdapter != nil && clinicCfg != nil && clinicCfg.UsesBoulevardBooking()
	bookingAPIReady := moxieAPIReady || boulevardReady
	qualificationsMet := ShouldFetchAvailabilityWithConfig(pc.history, nil, clinicCfg)
	shouldTrigger := bookingAPIReady && pc.timeSelectionState == nil

	if pc.timeSelectionState != nil && pc.timeSelectionState.SlotSelected {
		shouldTrigger = false
	}
	if usesMoxie || boulevardReady {
		shouldTrigger = shouldTrigger && qualificationsMet
	} else {
		shouldTrigger = shouldTrigger && pc.depositIntent != nil && qualificationsMet
	}

	// Defer until schedule preferences exist
	var earlyPrefs *leads.SchedulingPreferences
	if shouldTrigger && (usesMoxie || boulevardReady) {
		p, _ := extractPreferences(pc.history, serviceAliasesFromConfig(clinicCfg))
		earlyPrefs = &p
		if !hasSchedulePreferences(earlyPrefs) {
			s.logger.Info("ProcessMessage: deferring time selection — no schedule preferences yet",
				"conversation_id", pc.req.ConversationID)
			shouldTrigger = false
		}
	}

	s.logger.Info("time selection trigger check",
		"conversation_id", pc.req.ConversationID,
		"moxie_api_ready", moxieAPIReady,
		"qualifications_met", qualificationsMet,
		"time_selection_state_exists", pc.timeSelectionState != nil,
		"uses_moxie", usesMoxie,
		"should_trigger", shouldTrigger,
	)

	if !shouldTrigger {
		return
	}

	bookingURL := ""
	if clinicCfg != nil {
		bookingURL = clinicCfg.BookingURL
	}
	if bookingURL == "" {
		return
	}

	var prefs leads.SchedulingPreferences
	if earlyPrefs != nil {
		prefs = *earlyPrefs
	} else {
		prefs, _ = extractPreferences(pc.history, serviceAliasesFromConfig(clinicCfg))
	}

	// Service variant resolution
	variantResp, variantErr := s.handleVariantResolution(ctx, clinicCfg, &prefs, pc.history, pc.rawMessage, pc.req.ConversationID, pc.req.OrgID)
	if variantErr != nil || variantResp != nil {
		if variantResp != nil {
			pc.reply = variantResp.Message
		}
		return
	}

	// Provider preference check
	if provResp := s.handleProviderPreference(clinicCfg, &prefs, prefs.ServiceInterest, pc.req.ConversationID); provResp != nil {
		pc.reply = provResp.Message
		return
	}

	// Voice channel: defer to async SMS
	if isVoiceChannel(pc.req.Channel) {
		s.logger.Info("voice channel: deferring availability to async SMS",
			"conversation_id", pc.req.ConversationID,
			"service", prefs.ServiceInterest,
		)
		pc.reply = fmt.Sprintf("Let me check what's available for %s. I'll text you the options in just a moment so you can pick the best time.", prefs.ServiceInterest)
		pc.asyncAvailability = &AsyncAvailabilityRequest{
			OrgID:           pc.req.OrgID,
			ConversationID:  pc.req.ConversationID,
			From:            pc.req.From,
			To:              pc.req.To,
			BookingURL:      bookingURL,
			ServiceInterest: prefs.ServiceInterest,
		}
		return
	}

	// Fetch and present availability
	pc.timeSelectionResponse = s.fetchAndPresentAvailability(ctx, &prefs, clinicCfg, bookingURL, pc.req.ConversationID, pc.req.OrgID, pc.req.OnProgress)
	if pc.timeSelectionResponse != nil && len(pc.timeSelectionResponse.Slots) > 0 {
		pc.depositIntent = nil
	}
}

// assembleBookingRequest builds a BookingRequest for Moxie clinics when a
// selected slot + email are available.
func (s *LLMService) assembleBookingRequest(ctx context.Context, pc *processContext, clinicCfg *clinic.Config, usesMoxie bool) {
	if !usesMoxie || clinicCfg == nil || clinicCfg.BookingURL == "" {
		return
	}

	// Check for previously selected slot on the lead
	var previouslySelectedDateTime *time.Time
	var previouslySelectedService string
	if pc.selectedSlot == nil && pc.timeSelectionState != nil && pc.timeSelectionState.SlotSelected && pc.req.LeadID != "" && s.leadsRepo != nil {
		if lead, err := s.leadsRepo.GetByID(ctx, pc.req.OrgID, pc.req.LeadID); err == nil && lead != nil && lead.SelectedDateTime != nil {
			dt := *lead.SelectedDateTime
			if clinicCfg.Timezone != "" {
				if loc, lerr := time.LoadLocation(clinicCfg.Timezone); lerr == nil {
					dt = dt.In(loc)
				}
			}
			previouslySelectedDateTime = &dt
			previouslySelectedService = lead.SelectedService
			s.logger.Info("found previously selected slot on lead",
				"lead_id", pc.req.LeadID,
				"date_time", lead.SelectedDateTime,
				"service", lead.SelectedService,
			)
		}
	}

	if pc.selectedSlot == nil && previouslySelectedDateTime == nil {
		return
	}

	firstName, lastName := splitName("")
	phone := pc.req.From
	email := ""

	if pc.req.LeadID != "" && s.leadsRepo != nil {
		if lead, err := s.leadsRepo.GetByID(ctx, pc.req.OrgID, pc.req.LeadID); err == nil && lead != nil {
			firstName, lastName = splitName(lead.Name)
			if lead.Phone != "" {
				phone = lead.Phone
			}
			email = lead.Email
		}
	}

	if email == "" {
		email = ExtractEmailFromHistory(pc.history)
	}

	if email == "" {
		s.logger.Info("proceeding with booking without email — will be captured on booking page", "lead_id", pc.req.LeadID)
	}

	var slotDateTime time.Time
	var slotService string
	if pc.selectedSlot != nil {
		slotDateTime = pc.selectedSlot.DateTime
		if pc.timeSelectionState != nil {
			slotService = pc.timeSelectionState.Service
		}
	} else if previouslySelectedDateTime != nil {
		slotDateTime = *previouslySelectedDateTime
		slotService = previouslySelectedService
	}

	dateStr := slotDateTime.Format("2006-01-02")
	timeStr := strings.ToLower(slotDateTime.Format("3:04pm"))

	var callbackURL string
	if s.apiBaseURL != "" {
		callbackURL = fmt.Sprintf("%s/webhooks/booking/callback?orgId=%s&from=%s",
			strings.TrimRight(s.apiBaseURL, "/"), pc.req.OrgID, pc.req.From)
	}

	pc.bookingRequest = &BookingRequest{
		BookingURL:  clinicCfg.BookingURL,
		Date:        dateStr,
		Time:        timeStr,
		Service:     slotService,
		LeadID:      pc.req.LeadID,
		OrgID:       pc.req.OrgID,
		FirstName:   firstName,
		LastName:    lastName,
		Phone:       phone,
		Email:       email,
		CallbackURL: callbackURL,
	}
	s.logger.Info("booking request prepared for Moxie",
		"booking_url", clinicCfg.BookingURL,
		"date", dateStr,
		"time", timeStr,
		"lead_id", pc.req.LeadID,
	)
}
