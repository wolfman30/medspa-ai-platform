package conversation

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"go.opentelemetry.io/otel/trace"
)

// processContext carries state through the phases of ProcessMessage. Each phase
// can read/mutate this struct and optionally return an early *Response.
type processContext struct {
	// Inputs (immutable after init)
	req        MessageRequest
	rawMessage string // possibly sanitized by prompt-injection filter
	span       trace.Span

	// Inbound filter results
	filter          FilterResult
	redactedMessage string
	sawPHI          bool
	medicalKeywords []string

	// Loaded state
	history            []ChatMessage
	cfg                *clinic.Config
	timeSelectionState *TimeSelectionState

	// Outputs built during processing
	timeSelectionResponse *TimeSelectionResponse
	selectedSlot          *PresentedSlot
	depositIntent         *DepositIntent
	bookingRequest        *BookingRequest
	reply                 string
}

// newProcessContext initialises a processContext from a MessageRequest,
// running the inbound filter and prompt-injection scan.
func (s *LLMService) newProcessContext(ctx context.Context, req MessageRequest) (*processContext, *Response) {
	filter := FilterInbound(req.Message)
	rawMessage := req.Message

	s.events.MessageReceived(ctx, req.ConversationID, req.OrgID, req.LeadID, rawMessage)

	// Prompt injection — hard block
	if filter.DeflectionMsg == blockedReply {
		injectionResult := ScanForPromptInjection(rawMessage)
		s.events.PromptInjectionDetected(ctx, req.ConversationID, req.OrgID, true, injectionResult.Score, injectionResult.Reasons)
		s.logger.Warn("ProcessMessage: prompt injection BLOCKED",
			"conversation_id", req.ConversationID,
			"org_id", req.OrgID,
			"score", injectionResult.Score,
			"reasons", injectionResult.Reasons,
		)
		if s.audit != nil && strings.TrimSpace(req.OrgID) != "" {
			_ = s.audit.LogPromptInjection(ctx, req.OrgID, req.ConversationID, req.LeadID, injectionResult.Reasons)
		}
		return nil, &Response{ConversationID: req.ConversationID, Message: blockedReply, Timestamp: time.Now().UTC()}
	}

	// Prompt injection — soft warning (sanitize)
	if filter.Sanitized != rawMessage {
		injectionResult := ScanForPromptInjection(rawMessage)
		s.logger.Warn("ProcessMessage: prompt injection WARNING",
			"conversation_id", req.ConversationID,
			"org_id", req.OrgID,
			"score", injectionResult.Score,
			"reasons", injectionResult.Reasons,
		)
		rawMessage = filter.Sanitized
	}

	return &processContext{
		req:             req,
		rawMessage:      rawMessage,
		filter:          filter,
		redactedMessage: filter.RedactedMsg,
		sawPHI:          filter.SawPHI,
		medicalKeywords: filter.MedicalKW,
	}, nil
}

// loadHistory loads the conversation history from Redis. If the conversation
// does not exist, it bootstraps a new one (handling PHI/medical deflections).
// Returns a *Response for early exit, or nil to continue processing.
func (s *LLMService) loadHistory(ctx context.Context, pc *processContext) *Response {
	history, err := s.history.Load(ctx, pc.req.ConversationID)

	// Trim voice history more aggressively for lower latency
	if isVoiceChannel(pc.req.Channel) && len(history) > maxVoiceHistoryMessages {
		history = trimHistory(history, maxVoiceHistoryMessages)
	}

	if err != nil {
		if strings.Contains(err.Error(), "unknown conversation") {
			s.logger.Info("ProcessMessage: conversation not found, starting new",
				"conversation_id", pc.req.ConversationID,
				"message", pc.redactedMessage,
			)
			return s.bootstrapNewConversation(ctx, pc)
		}
		pc.span.RecordError(err)
		return nil // will be handled as error in caller — but we need to propagate
	}

	pc.history = history
	s.logger.Info("ProcessMessage: history loaded",
		"conversation_id", pc.req.ConversationID,
		"history_length", len(history),
	)
	return nil
}

// bootstrapNewConversation handles the case where ProcessMessage is called for
// a conversation that doesn't exist yet — either deflecting PHI/medical content
// or delegating to StartConversation.
func (s *LLMService) bootstrapNewConversation(ctx context.Context, pc *processContext) *Response {
	if pc.sawPHI {
		return s.bootstrapWithDeflection(ctx, pc, phiDeflectionReply, pc.redactedMessage, func() {
			if s.audit != nil && strings.TrimSpace(pc.req.OrgID) != "" {
				_ = s.audit.LogPHIDetected(ctx, pc.req.OrgID, pc.req.ConversationID, pc.req.LeadID, pc.req.Message, "keyword")
			}
		})
	}
	if len(pc.medicalKeywords) > 0 {
		return s.bootstrapWithDeflection(ctx, pc, medicalAdviceDeflectionReply, "[REDACTED]", func() {
			if s.audit != nil && strings.TrimSpace(pc.req.OrgID) != "" {
				_ = s.audit.LogMedicalAdviceRefused(ctx, pc.req.OrgID, pc.req.ConversationID, pc.req.LeadID, "[REDACTED]", pc.medicalKeywords)
			}
		})
	}

	// Normal start — delegate to StartConversation
	// We set reply to "" to signal caller to use StartConversation result
	return nil // handled specially in ProcessMessage
}

// bootstrapWithDeflection creates a new conversation history with a deflection
// reply (PHI or medical advice) and persists it.
func (s *LLMService) bootstrapWithDeflection(ctx context.Context, pc *processContext, deflectionReply, intro string, auditFn func()) *Response {
	depositCents := s.deposit.DefaultAmountCents
	var usesMoxie bool
	if s.clinicStore != nil && pc.req.OrgID != "" {
		if cfg, err := s.clinicStore.Get(ctx, pc.req.OrgID); err == nil && cfg != nil {
			if cfg.DepositAmountCents > 0 {
				depositCents = int32(cfg.DepositAmountCents)
			}
			usesMoxie = cfg.UsesMoxieBooking()
		}
	}

	safeStart := StartRequest{
		OrgID:          pc.req.OrgID,
		ConversationID: pc.req.ConversationID,
		LeadID:         pc.req.LeadID,
		ClinicID:       pc.req.ClinicID,
		Intro:          intro,
		Channel:        pc.req.Channel,
		From:           pc.req.From,
		To:             pc.req.To,
		Metadata:       pc.req.Metadata,
	}

	history := []ChatMessage{
		{Role: ChatRoleSystem, Content: buildSystemPrompt(int(depositCents), usesMoxie)},
	}
	history = s.appendContext(ctx, history, pc.req.OrgID, pc.req.LeadID, pc.req.ClinicID, "")
	history = append(history, ChatMessage{
		Role:    ChatRoleUser,
		Content: formatIntroMessage(safeStart, pc.req.ConversationID),
	})
	history = append(history, ChatMessage{
		Role:    ChatRoleAssistant,
		Content: deflectionReply,
	})
	history = trimHistory(history, maxHistoryMessages)
	if err := s.history.Save(ctx, pc.req.ConversationID, history); err != nil {
		pc.span.RecordError(err)
	}
	auditFn()
	return &Response{ConversationID: pc.req.ConversationID, Message: deflectionReply, Timestamp: time.Now().UTC()}
}

// handleSafetyDeflections checks for PHI and medical keywords on existing
// conversations, returning a deflection response if needed.
func (s *LLMService) handleSafetyDeflections(ctx context.Context, pc *processContext) *Response {
	if pc.sawPHI {
		return s.deflectExisting(ctx, pc, phiDeflectionReply, pc.redactedMessage, func() {
			if s.audit != nil && strings.TrimSpace(pc.req.OrgID) != "" {
				_ = s.audit.LogPHIDetected(ctx, pc.req.OrgID, pc.req.ConversationID, pc.req.LeadID, pc.req.Message, "keyword")
			}
		})
	}
	if len(pc.medicalKeywords) > 0 {
		return s.deflectExisting(ctx, pc, medicalAdviceDeflectionReply, "[REDACTED]", func() {
			if s.audit != nil && strings.TrimSpace(pc.req.OrgID) != "" {
				_ = s.audit.LogMedicalAdviceRefused(ctx, pc.req.OrgID, pc.req.ConversationID, pc.req.LeadID, "[REDACTED]", pc.medicalKeywords)
			}
		})
	}
	return nil
}

// deflectExisting appends a deflection reply to an existing conversation's history.
func (s *LLMService) deflectExisting(ctx context.Context, pc *processContext, reply, userContent string, auditFn func()) *Response {
	pc.history = s.appendContext(ctx, pc.history, pc.req.OrgID, pc.req.LeadID, pc.req.ClinicID, "")
	pc.history = append(pc.history, ChatMessage{Role: ChatRoleUser, Content: userContent})
	pc.history = append(pc.history, ChatMessage{Role: ChatRoleAssistant, Content: reply})
	pc.history = trimHistory(pc.history, maxHistoryMessages)
	if err := s.history.Save(ctx, pc.req.ConversationID, pc.history); err != nil {
		pc.span.RecordError(err)
	}
	auditFn()
	return &Response{ConversationID: pc.req.ConversationID, Message: reply, Timestamp: time.Now().UTC()}
}

// handleDeterministicGuardrails checks for price inquiries, question selection,
// and ambiguous help — deterministic replies that skip the LLM.
func (s *LLMService) handleDeterministicGuardrails(ctx context.Context, pc *processContext) *Response {
	if pc.cfg != nil && isPriceInquiry(pc.rawMessage) {
		if resp := s.handlePriceInquiry(ctx, pc); resp != nil {
			return resp
		}
	}
	if isQuestionSelection(pc.rawMessage) {
		return s.saveAndReturn(ctx, pc, "Absolutely - what can I help with? If it's about a specific service (Botox, fillers, facials, lasers), let me know which one.", "question_selection")
	}
	if isAmbiguousHelp(pc.rawMessage) {
		s.appendLeadNote(ctx, pc.req.OrgID, pc.req.LeadID, "state:needs_intent")
		return s.saveAndReturn(ctx, pc, "Happy to help. Are you looking to book an appointment, or do you have a question about a specific service (Botox, fillers, facials, lasers)?", "ambiguous_help")
	}
	return nil
}

// handlePriceInquiry handles deterministic price responses from clinic config.
func (s *LLMService) handlePriceInquiry(ctx context.Context, pc *processContext) *Response {
	service := detectServiceKey(pc.rawMessage, pc.cfg)
	if service == "" {
		return nil
	}
	price, ok := pc.cfg.PriceTextForService(service)
	if !ok {
		return nil
	}
	depositCents := pc.cfg.DepositAmountForService(service)
	depositDollars := float64(depositCents) / 100.0
	displayName := strings.Title(service) //nolint:staticcheck
	for _, svc := range pc.cfg.Services {
		if strings.EqualFold(svc, service) {
			displayName = svc
			break
		}
	}
	reply := fmt.Sprintf("%s pricing: %s. To secure priority booking, we collect a small refundable deposit of $%.0f that applies toward your treatment. Would you like to proceed?", displayName, price, depositDollars)
	s.appendLeadNote(ctx, pc.req.OrgID, pc.req.LeadID, "tag:price_shopper")
	return s.saveAndReturn(ctx, pc, reply, "price_inquiry")
}

// saveAndReturn appends a reply to history, saves, and returns a Response.
func (s *LLMService) saveAndReturn(ctx context.Context, pc *processContext, reply, reason string) *Response {
	pc.history = append(pc.history, ChatMessage{Role: ChatRoleAssistant, Content: reply})
	pc.history = trimHistory(pc.history, maxHistoryMessages)
	if err := s.history.Save(ctx, pc.req.ConversationID, pc.history); err != nil {
		pc.span.RecordError(err)
	}
	s.savePreferencesNoNote(ctx, pc.req.LeadID, pc.history, reason)
	return &Response{ConversationID: pc.req.ConversationID, Message: reply, Timestamp: time.Now().UTC()}
}

// handleFAQClassification checks if the message is a service comparison question
// and returns a cached FAQ response if available.
func (s *LLMService) handleFAQClassification(ctx context.Context, pc *processContext) *Response {
	isComparison := IsServiceComparisonQuestion(pc.rawMessage)
	msgPreview := pc.rawMessage
	if len(msgPreview) > 50 {
		msgPreview = msgPreview[:50] + "..."
	}
	s.logger.Info("FAQ classifier check", "is_comparison_question", isComparison, "message_preview", msgPreview)
	if !isComparison {
		return nil
	}

	var faqReply string
	var faqSource string

	// Try LLM classifier first (more accurate)
	if s.faqClassifier != nil {
		category, classifyErr := s.faqClassifier.ClassifyQuestion(ctx, pc.rawMessage)
		s.logger.Info("FAQ LLM classifier result", "category", category, "error", classifyErr)
		if classifyErr == nil && category != FAQCategoryOther {
			faqReply = GetFAQResponse(category)
			faqSource = "llm_classifier"
		} else if classifyErr != nil {
			s.logger.Warn("FAQ LLM classification failed, trying regex fallback", "error", classifyErr)
		}
	}

	// Fallback to regex pattern matching
	if faqReply == "" {
		if regexReply, found := CheckFAQCache(pc.rawMessage); found {
			faqReply = regexReply
			faqSource = "regex_fallback"
			s.logger.Info("FAQ regex fallback hit", "conversation_id", pc.req.ConversationID)
		}
	}

	if faqReply == "" {
		s.logger.Info("FAQ: no match from classifier or regex, falling through to full LLM")
		return nil
	}

	s.logger.Info("FAQ response returned", "source", faqSource, "conversation_id", pc.req.ConversationID)
	return s.saveAndReturn(ctx, pc, faqReply, "faq_response")
}

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

// injectMoxieQualificationGuardrails appends system guardrails to enforce
// the Moxie qualification order: name → service → patient type → schedule → provider → email.
func (s *LLMService) injectMoxieQualificationGuardrails(ctx context.Context, pc *processContext) {
	if pc.cfg == nil || !pc.cfg.UsesMoxieBooking() {
		return
	}

	prefs, _ := extractPreferences(pc.history, serviceAliasesFromConfig(pc.cfg))

	// Pre-fetch availability as soon as we know the service.
	if prefs.ServiceInterest != "" && s.prefetcher != nil {
		s.prefetcher.StartPrefetch(ctx, pc.req.OrgID, pc.cfg, prefs.ServiceInterest, prefs.ProviderPreference)
	}

	// Name guardrail
	if prefs.ServiceInterest != "" && prefs.Name == "" && !lastAssistantAskedForName(pc.history) {
		pc.history = append(pc.history, ChatMessage{
			Role: ChatRoleSystem,
			Content: "[SYSTEM GUARDRAIL] The patient mentioned a service but you do NOT have their name yet. " +
				"NAME is #1 in the Moxie checklist and MUST be collected before anything else. " +
				"You MUST ask for their full name NOW. Do NOT ask about patient type, schedule, provider, or email yet. " +
				"Ask something like: 'Great choice! May I have your full name?'",
		})
	}

	// Patient type guardrail
	if prefs.ServiceInterest != "" && prefs.Name != "" && prefs.PatientType == "" {
		pc.history = append(pc.history, ChatMessage{
			Role: ChatRoleSystem,
			Content: "[SYSTEM GUARDRAIL] You have the patient's name and service interest. " +
				"Next in the checklist is PATIENT TYPE (#3). " +
				"You MUST ask if they are a new or returning patient NOW. Do NOT ask about schedule, email, or provider yet. " +
				"Ask something like: 'Have you visited us before, or would this be your first time?'",
		})
	}

	// Schedule guardrail
	if prefs.ServiceInterest != "" && prefs.Name != "" && prefs.PatientType != "" &&
		prefs.PreferredDays == "" && prefs.PreferredTimes == "" {
		pc.history = append(pc.history, ChatMessage{
			Role: ChatRoleSystem,
			Content: "[SYSTEM GUARDRAIL] You have the patient's name, service, and patient type. " +
				"Next in the Moxie checklist is SCHEDULE (#4). " +
				"You MUST ask about their preferred days and times NOW. Do NOT ask for email or provider preference yet. " +
				"Ask something like: 'What days and times work best for you?'",
		})
	}

	// Provider preference guardrail
	if prefs.ServiceInterest != "" && prefs.ProviderPreference == "" &&
		(prefs.PreferredDays != "" || prefs.PreferredTimes != "") {
		resolvedService := pc.cfg.ResolveServiceName(prefs.ServiceInterest)
		if pc.cfg.ServiceNeedsProviderPreference(resolvedService) {
			providerNames := make([]string, 0)
			if pc.cfg.MoxieConfig != nil {
				for _, name := range pc.cfg.MoxieConfig.ProviderNames {
					providerNames = append(providerNames, name)
				}
			}
			var providerList string
			if len(providerNames) > 0 {
				providerList = fmt.Sprintf(" Available providers: %s.", strings.Join(providerNames, ", "))
			}
			pc.history = append(pc.history, ChatMessage{
				Role: ChatRoleSystem,
				Content: fmt.Sprintf("[SYSTEM GUARDRAIL] The patient wants %s which has multiple providers.%s "+
					"You MUST ask about provider preference NOW. Do NOT ask for email yet. "+
					"Ask: 'Do you have a provider preference, or would you like the first available appointment?'",
					prefs.ServiceInterest, providerList),
			})
		}
	}
}

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
			usesMoxie = cfg.UsesMoxieBooking()
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

	// GUARD: Moxie clinics — no deposit before time selection
	if pc.depositIntent != nil && usesMoxie && (pc.timeSelectionState == nil || !pc.timeSelectionState.SlotSelected) {
		s.logger.Warn("deposit intent suppressed: Moxie clinic requires time selection before deposit",
			"conversation_id", pc.req.ConversationID,
			"slot_selected", pc.timeSelectionState != nil && pc.timeSelectionState.SlotSelected,
		)
		pc.depositIntent = nil
	}

	// Time selection triggering
	s.maybeTriggertimeSelection(ctx, pc, clinicCfg, usesMoxie)

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

	// Clear Square deposit for Moxie clinics
	if usesMoxie && pc.depositIntent != nil {
		s.logger.Info("clinic uses Moxie booking - skipping Square deposit intent", "org_id", pc.req.OrgID)
		pc.depositIntent = nil
	}

	// Booking request assembly for Moxie clinics
	s.assembleBookingRequest(ctx, pc, clinicCfg, usesMoxie)
}

// maybeTriggertimeSelection checks whether to fetch and present available time slots.
func (s *LLMService) maybeTriggertimeSelection(ctx context.Context, pc *processContext, clinicCfg *clinic.Config, usesMoxie bool) {
	moxieAPIReady := s.moxieClient != nil && clinicCfg != nil && clinicCfg.MoxieConfig != nil
	qualificationsMet := ShouldFetchAvailabilityWithConfig(pc.history, nil, clinicCfg)
	shouldTrigger := moxieAPIReady && pc.timeSelectionState == nil

	if pc.timeSelectionState != nil && pc.timeSelectionState.SlotSelected {
		shouldTrigger = false
	}
	if usesMoxie {
		shouldTrigger = shouldTrigger && qualificationsMet
	} else {
		shouldTrigger = shouldTrigger && pc.depositIntent != nil && qualificationsMet
	}

	// Defer until schedule preferences exist
	var earlyPrefs *leads.SchedulingPreferences
	if shouldTrigger && usesMoxie {
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
		s.logger.Warn("booking blocked: no email for Moxie booking", "lead_id", pc.req.LeadID)
		if pc.selectedSlot != nil {
			slotTime := pc.selectedSlot.DateTime
			pc.reply = fmt.Sprintf("Great choice! I've got %s for %s. To complete your booking, I just need your email address. What's the best email for you?",
				slotTime.Format("Monday, January 2 at 3:04 PM"), pc.timeSelectionState.Service)
			for i := len(pc.history) - 1; i >= 0; i-- {
				if pc.history[i].Role == ChatRoleAssistant {
					pc.history[i].Content = pc.reply
					break
				}
			}
			if err := s.history.Save(ctx, pc.req.ConversationID, pc.history); err != nil {
				s.logger.Warn("failed to save history after email request override", "error", err)
			}
		}
		return
	}

	var slotDateTime time.Time
	var slotService string
	if pc.selectedSlot != nil {
		slotDateTime = pc.selectedSlot.DateTime
		if pc.timeSelectionState != nil {
			slotService = pc.timeSelectionState.Service
		}
	} else {
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
