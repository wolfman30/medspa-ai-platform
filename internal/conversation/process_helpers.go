package conversation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
)

// handleVariantResolution checks whether the patient's chosen service has delivery
// variants (e.g. in-person vs virtual) and, if so, attempts to resolve which one
// they want. Returns a *Response when a clarification question must be sent to the
// patient, or nil when the variant is resolved (or not applicable). On success the
// resolved service name is written back into prefs.ServiceInterest.
func (s *LLMService) handleVariantResolution(
	ctx context.Context,
	cfg *clinic.Config,
	prefs *leads.SchedulingPreferences,
	history []ChatMessage,
	rawMessage, conversationID, orgID string,
) (*Response, error) {
	msgs := recentUserMessages(history, rawMessage, 6)
	resolved, question := s.variantResolver.Resolve(ctx, cfg, prefs.ServiceInterest, msgs)
	if question != "" {
		s.events.VariantAsked(ctx, conversationID, orgID, prefs.ServiceInterest, nil)
		s.logger.Info("service variant clarification needed",
			"conversation_id", conversationID,
			"service", prefs.ServiceInterest,
		)
		// Replace the last assistant message with the clarification question.
		for i := len(history) - 1; i >= 0; i-- {
			if history[i].Role == ChatRoleAssistant {
				history[i].Content = question
				break
			}
		}
		if err := s.history.Save(ctx, conversationID, history); err != nil {
			s.logger.Warn("failed to save history after variant question", "error", err)
		}
		return &Response{
			ConversationID: conversationID,
			Message:        question,
			Timestamp:      time.Now().UTC(),
		}, nil
	}
	if resolved != prefs.ServiceInterest {
		s.events.VariantResolved(ctx, conversationID, orgID, prefs.ServiceInterest, resolved, "auto")
		s.logger.Info("service variant resolved",
			"conversation_id", conversationID,
			"original_service", prefs.ServiceInterest,
			"resolved_variant", resolved,
		)
	}
	prefs.ServiceInterest = resolved
	return nil, nil
}

// handleProviderPreference checks whether the resolved service requires a provider
// preference and returns a Response asking the patient to choose when one is needed.
// Returns nil if provider preference is already set or the service has a single provider.
func (s *LLMService) handleProviderPreference(
	cfg *clinic.Config,
	prefs *leads.SchedulingPreferences,
	resolved string,
	conversationID string,
) *Response {
	if prefs.ProviderPreference != "" || cfg == nil || !cfg.ServiceNeedsProviderPreference(resolved) {
		return nil
	}
	providerNames := make([]string, 0)
	if cfg.MoxieConfig != nil {
		for _, name := range cfg.MoxieConfig.ProviderNames {
			providerNames = append(providerNames, name)
		}
	}
	var providerList string
	if len(providerNames) > 0 {
		providerList = " We have " + strings.Join(providerNames, " and ") + "."
	}
	s.logger.Info("asking provider preference after variant resolution",
		"conversation_id", conversationID,
		"resolved_service", resolved,
	)
	return &Response{
		Message:        fmt.Sprintf("Great choice — %s!%s Do you have a provider preference, or would you like the first available appointment?", resolved, providerList),
		ConversationID: conversationID,
		Timestamp:      time.Now().UTC(),
	}
}

// fetchAndPresentAvailability fetches real-time availability from the Moxie API or
// browser scraper, saves the resulting time selection state, and returns a
// TimeSelectionResponse ready to send to the patient. Returns nil when no booking URL
// is configured or the fetch otherwise cannot proceed.
func (s *LLMService) fetchAndPresentAvailability(
	ctx context.Context,
	prefs *leads.SchedulingPreferences,
	cfg *clinic.Config,
	bookingURL, conversationID, orgID string,
	onProgress func(ctx context.Context, msg string),
) *TimeSelectionResponse {
	timePrefs := ExtractTimePreferences(prefs.PreferredDays + " " + prefs.PreferredTimes)

	// Resolve patient-facing service name to booking-platform search term
	scraperServiceName := prefs.ServiceInterest
	if cfg != nil {
		scraperServiceName = cfg.ResolveServiceName(scraperServiceName)
	}

	s.events.ServiceExtracted(ctx, conversationID, orgID, prefs.ServiceInterest, scraperServiceName)

	s.logger.Info("fetching available times",
		"conversation_id", conversationID,
		"original_service", prefs.ServiceInterest,
		"resolved_service", scraperServiceName,
		"booking_url", bookingURL,
		"preferred_days", prefs.PreferredDays,
		"preferred_times", prefs.PreferredTimes,
	)

	// Try Moxie API first (instant, ~1s), fall back to browser scraper (~30-60s)
	fetchCtx, fetchCancel := context.WithTimeout(ctx, 120*time.Second)
	var result *AvailabilityResult
	var err error

	if s.moxieClient != nil && cfg != nil && cfg.MoxieConfig != nil {
		s.logger.Info("fetching availability via Moxie API (fast path)",
			"conversation_id", conversationID, "service", scraperServiceName)
		result, err = FetchAvailableTimesFromMoxieAPIWithProvider(fetchCtx, s.moxieClient, cfg, scraperServiceName, prefs.ProviderPreference, timePrefs, onProgress, prefs.ServiceInterest)
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "no serviceMenuItemId") {
				s.logger.Warn("Moxie API: service not found — skipping browser scraper fallback",
					"error", err, "conversation_id", conversationID, "service", scraperServiceName)
				result = &AvailabilityResult{
					Slots:      nil,
					ExactMatch: false,
					Message: fmt.Sprintf(
						"I'm sorry, but %s doesn't appear to be a service currently offered at this clinic. "+
							"Would you like to see what services are available, or is there something else I can help with?",
						prefs.ServiceInterest),
				}
				err = nil
			} else {
				s.logger.Warn("Moxie API availability failed, falling back to browser scraper",
					"error", err, "conversation_id", conversationID)
				result, err = FetchAvailableTimesWithFallback(fetchCtx, s.browser, bookingURL, scraperServiceName, timePrefs, onProgress, prefs.ServiceInterest)
			}
		}
	} else {
		result, err = FetchAvailableTimesWithFallback(fetchCtx, s.browser, bookingURL, scraperServiceName, timePrefs, onProgress, prefs.ServiceInterest)
	}
	fetchCancel()

	if err != nil {
		s.logger.Warn("failed to fetch available times", "error", err)
		return &TimeSelectionResponse{
			Slots:      nil,
			Service:    prefs.ServiceInterest,
			ExactMatch: false,
			SMSMessage: fmt.Sprintf("I had trouble checking availability for %s right now. Could you try again in a moment?", prefs.ServiceInterest),
		}
	}

	if len(result.Slots) > 0 {
		s.events.AvailabilityFetched(ctx, conversationID, orgID, prefs.ServiceInterest, len(result.Slots), 0)
		state := &TimeSelectionState{
			PresentedSlots: result.Slots,
			Service:        prefs.ServiceInterest,
			BookingURL:     bookingURL,
			PresentedAt:    time.Now(),
		}
		if err := s.history.SaveTimeSelectionState(ctx, conversationID, state); err != nil {
			s.logger.Error("CRITICAL: failed to save time selection state — patient will not be able to select a slot",
				"error", err,
				"conversation_id", conversationID,
				"slots", len(result.Slots),
			)
		} else {
			s.logger.Info("time selection state saved successfully",
				"conversation_id", conversationID,
				"slots", len(state.PresentedSlots),
				"service", state.Service,
			)
		}

		return &TimeSelectionResponse{
			Slots:      result.Slots,
			Service:    prefs.ServiceInterest,
			ExactMatch: result.ExactMatch,
			SMSMessage: FormatTimeSlotsForSMS(result.Slots, prefs.ServiceInterest, result.ExactMatch),
		}
	}

	// No slots found
	return &TimeSelectionResponse{
		Slots:      nil,
		Service:    prefs.ServiceInterest,
		ExactMatch: false,
		SMSMessage: result.Message,
	}
}

// handleDepositFlow determines whether a deposit intent should be emitted for the
// current conversation turn. It runs the deterministic agreement check first, then
// falls back to the LLM classifier when appropriate. Returns nil when no deposit
// should be collected.
func (s *LLMService) handleDepositFlow(ctx context.Context, history []ChatMessage) *DepositIntent {
	if latestTurnAgreedToDeposit(history) {
		intent := &DepositIntent{
			AmountCents: s.deposit.DefaultAmountCents,
			Description: s.deposit.Description,
			SuccessURL:  s.deposit.SuccessURL,
			CancelURL:   s.deposit.CancelURL,
		}
		s.logger.Info("deposit intent inferred from explicit user agreement", "amount_cents", intent.AmountCents)
		return intent
	}

	if shouldAttemptDepositClassification(history) {
		extracted, derr := s.extractDepositIntent(ctx, history)
		if derr != nil {
			s.logger.Warn("deposit intent extraction failed", "error", derr)
		} else if extracted != nil {
			s.logger.Info("deposit intent extracted", "amount_cents", extracted.AmountCents)
		} else {
			s.logger.Debug("no deposit intent detected")
		}
		return extracted
	}

	s.logger.Debug("deposit: classifier skipped (no deposit context)")
	return nil
}
