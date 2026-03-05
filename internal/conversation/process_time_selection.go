package conversation

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/leads"
)

// loadTimeSelectionState loads time selection state and handles new-service
// detection (resetting state when a patient asks about a different service
// after already booking one).
func (s *LLMService) loadTimeSelectionState(ctx context.Context, pc *processContext) {
	state, tsErr := s.history.LoadTimeSelectionState(ctx, pc.req.ConversationID)
	if tsErr != nil {
		s.logger.Warn("failed to load time selection state", "error", tsErr, "conversation_id", pc.req.ConversationID)
		return
	}
	s.logger.Info("time selection state loaded",
		"conversation_id", pc.req.ConversationID,
		"state_exists", state != nil,
		"slots_count", func() int {
			if state != nil {
				return len(state.PresentedSlots)
			}
			return 0
		}(),
		"slot_selected", func() bool {
			if state != nil {
				return state.SlotSelected
			}
			return false
		}(),
	)

	// Detect new service request after a previous booking
	if state != nil && state.SlotSelected {
		if s.detectNewServiceAfterBooking(ctx, pc, state) {
			state = nil
		}
	}

	pc.timeSelectionState = state
}

// detectNewServiceAfterBooking checks if the patient is asking about a different
// service after already selecting a slot. Returns true if state was reset.
func (s *LLMService) detectNewServiceAfterBooking(ctx context.Context, pc *processContext, state *TimeSelectionState) bool {
	bookedService := strings.ToLower(strings.TrimSpace(state.Service))

	newServiceExact := ""
	if pc.cfg != nil {
		newServiceExact = detectServiceKey(pc.rawMessage, pc.cfg)
	}

	msgLower := strings.ToLower(pc.rawMessage)
	mentionsNewService := false

	if newServiceExact != "" {
		resolvedNew := strings.ToLower(newServiceExact)
		resolvedOld := bookedService
		if pc.cfg != nil {
			resolvedNew = strings.ToLower(pc.cfg.ResolveServiceName(newServiceExact))
			resolvedOld = strings.ToLower(pc.cfg.ResolveServiceName(bookedService))
		}
		mentionsNewService = resolvedNew != resolvedOld
	}

	if !mentionsNewService {
		newServicePatterns := []string{
			`(?i)(?:i\s+(?:also\s+)?want|(?:also|can\s+i)\s+(?:book|get|schedule)|book\s+(?:me\s+)?(?:for\s+)?|i.+?too$)`,
		}
		for _, pat := range newServicePatterns {
			if re, err := regexp.Compile(pat); err == nil && re.MatchString(msgLower) {
				if !strings.Contains(msgLower, bookedService) {
					mentionsNewService = true
				}
				break
			}
		}
	}

	if !mentionsNewService {
		return false
	}

	s.logger.Info("new service detected after previous booking — resetting time selection state",
		"conversation_id", pc.req.ConversationID,
		"old_service", state.Service,
		"message", pc.rawMessage,
	)
	if err := s.history.ClearTimeSelectionState(ctx, pc.req.ConversationID); err != nil {
		s.logger.Warn("failed to clear time selection state for new service", "error", err)
	}
	if pc.req.LeadID != "" && s.leadsRepo != nil {
		if uerr := s.leadsRepo.ClearSelectedAppointment(ctx, pc.req.LeadID); uerr != nil {
			s.logger.Warn("failed to clear selected appointment for new service", "error", uerr)
		}
	}
	return true
}

// handleActiveTimeSelection processes time slot selection, "more times" requests,
// and injects slot context when a patient is in the time-selection flow.
func (s *LLMService) handleActiveTimeSelection(ctx context.Context, pc *processContext) {
	state := pc.timeSelectionState
	if state == nil || len(state.PresentedSlots) == 0 {
		return
	}

	// Build time preferences for disambiguation
	selectionPrefs := TimePreferences{}
	if convPrefs, ok := extractPreferences(pc.history, serviceAliasesFromConfig(pc.cfg)); ok {
		selectionPrefs = ExtractTimePreferences(convPrefs.PreferredDays + " " + convPrefs.PreferredTimes)
	}

	// Check if user is selecting a time slot
	selectedSlot := DetectTimeSelection(pc.rawMessage, state.PresentedSlots, selectionPrefs)
	if selectedSlot != nil {
		s.handleSlotSelection(ctx, pc, selectedSlot)
		return
	}

	// Check if user wants more/different times
	if isMoreTimesRequest(strings.ToLower(pc.rawMessage)) {
		s.handleMoreTimesRequest(ctx, pc)
		return
	}

	// User sent unrelated message — inject slot context so LLM doesn't hallucinate times
	var slotList strings.Builder
	for _, slot := range state.PresentedSlots {
		slotList.WriteString(fmt.Sprintf("  %d. %s\n", slot.Index, slot.TimeStr))
	}
	pc.history = append(pc.history, ChatMessage{
		Role: ChatRoleSystem,
		Content: fmt.Sprintf("[SYSTEM] The following REAL appointment times for %s were already presented to the patient:\n%s"+
			"ONLY reference these times. Do NOT invent, guess, or fabricate any other times. "+
			"If the patient wants different times, offer to check again with different preferences.",
			state.Service, slotList.String()),
	})
}

// handleSlotSelection processes a patient selecting a specific time slot.
func (s *LLMService) handleSlotSelection(ctx context.Context, pc *processContext, slot *PresentedSlot) {
	state := pc.timeSelectionState
	s.events.TimeSlotSelected(ctx, pc.req.ConversationID, pc.req.OrgID, slot.DateTime.Format(time.RFC3339), slot.Index)
	s.logger.Info("time slot selected",
		"slot_index", slot.Index,
		"time", slot.DateTime,
		"service", state.Service,
	)

	// Store selected appointment on the lead
	if pc.req.LeadID != "" && s.leadsRepo != nil {
		if err := s.leadsRepo.UpdateSelectedAppointment(ctx, pc.req.LeadID, leads.SelectedAppointment{
			DateTime: &slot.DateTime,
			Service:  state.Service,
		}); err != nil {
			s.logger.Warn("failed to save selected appointment", "lead_id", pc.req.LeadID, "error", err)
		}
	}

	// Mark slot as selected
	state.SlotSelected = true
	state.PresentedSlots = nil
	if err := s.history.SaveTimeSelectionState(ctx, pc.req.ConversationID, state); err != nil {
		s.logger.Warn("failed to save time selection completion state", "error", err)
	}

	// Inject into history for LLM confirmation
	pc.history = append(pc.history, ChatMessage{
		Role:    ChatRoleSystem,
		Content: fmt.Sprintf("[SYSTEM] The patient selected time slot #%d: %s for %s. Confirm their selection and proceed with booking.", slot.Index, slot.TimeStr, state.Service),
	})
	pc.selectedSlot = slot
}

// handleMoreTimesRequest handles when a patient asks for more/different available times.
func (s *LLMService) handleMoreTimesRequest(ctx context.Context, pc *processContext) {
	state := pc.timeSelectionState
	s.logger.Info("patient requesting more times",
		"conversation_id", pc.req.ConversationID,
		"message", pc.rawMessage,
	)

	moreTimesHandled := false
	if s.moxieClient != nil && pc.cfg != nil && pc.cfg.MoxieConfig != nil {
		prefs, _ := extractPreferences(pc.history, serviceAliasesFromConfig(pc.cfg))
		service := state.Service
		scraperServiceName := service
		if pc.cfg != nil {
			scraperServiceName = pc.cfg.ResolveServiceName(scraperServiceName)
		}

		refinedPrefs := buildRefinedTimePreferences(pc.rawMessage, prefs, state.PresentedSlots)
		s.logger.Info("re-fetching availability with refined preferences",
			"conversation_id", pc.req.ConversationID,
			"refined_after", refinedPrefs.AfterTime,
			"refined_days", refinedPrefs.DaysOfWeek,
			"excluded_times", len(state.PresentedSlots),
		)

		fetchCtx, fetchCancel := context.WithTimeout(ctx, 120*time.Second)
		var result *AvailabilityResult
		var fetchErr error

		result, fetchErr = FetchAvailableTimesFromMoxieAPIWithProvider(fetchCtx, s.moxieClient, pc.cfg,
			scraperServiceName, prefs.ProviderPreference, refinedPrefs, pc.req.OnProgress, service)
		fetchCancel()

		if fetchErr == nil && result != nil {
			newSlots := filterOutPreviousSlots(result.Slots, state.PresentedSlots)
			if len(newSlots) > 0 {
				for i := range newSlots {
					newSlots[i].Index = i + 1
				}
				newState := &TimeSelectionState{
					PresentedSlots: newSlots,
					Service:        service,
					BookingURL:     state.BookingURL,
					PresentedAt:    time.Now(),
				}
				if err := s.history.SaveTimeSelectionState(ctx, pc.req.ConversationID, newState); err != nil {
					s.logger.Error("failed to save refined time selection state", "error", err)
				}
				pc.timeSelectionResponse = &TimeSelectionResponse{
					Slots:      newSlots,
					Service:    service,
					ExactMatch: true,
					SMSMessage: FormatTimeSlotsForSMS(newSlots, service, true),
				}
				moreTimesHandled = true
			} else {
				pc.timeSelectionResponse = &TimeSelectionResponse{
					Slots:      nil,
					Service:    service,
					ExactMatch: false,
					SMSMessage: fmt.Sprintf("Those are the latest available times on those days for %s. Would you like to try different days, or would one of the times I showed work for you?", service),
				}
				moreTimesHandled = true
			}
		}
	}

	if !moreTimesHandled {
		pc.timeSelectionState = nil
		if err := s.history.SaveTimeSelectionState(ctx, pc.req.ConversationID, nil); err != nil {
			s.logger.Warn("failed to clear time selection state", "error", err)
		}
	}
}
