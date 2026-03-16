package conversation

import (
	"context"
	"fmt"
	"strings"
)

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

// injectMoxieQualificationGuardrails appends system guardrails to enforce
// the booking qualification order: name → service → patient type → schedule → provider → email.
// Applies to all booking platforms (Moxie and Boulevard).
func (s *LLMService) injectMoxieQualificationGuardrails(ctx context.Context, pc *processContext) {
	if pc.cfg == nil || !pc.cfg.UsesBookingAPI() {
		return
	}

	prefs, _ := extractPreferences(pc.history, serviceAliasesFromConfig(pc.cfg))

	// Pre-fetch availability as soon as we know the service.
	if prefs.ServiceInterest != "" && s.prefetcher != nil {
		s.prefetcher.StartPrefetch(ctx, pc.req.OrgID, pc.cfg, prefs.ServiceInterest, prefs.ProviderPreference)
	}

	// Concern-based guardrail (wrinkles, fine lines, anti-aging → wrinkle relaxer category)
	if isConcernBasedService(prefs.ServiceInterest) && prefs.Name == "" {
		pc.history = append(pc.history, ChatMessage{
			Role: ChatRoleSystem,
			Content: "[SYSTEM GUARDRAIL] The patient described a CONCERN (e.g., wrinkles, fine lines, aging), NOT a specific treatment. " +
				"Do NOT recommend a single treatment like Botox. Instead: (1) Mention that there are several great options like Botox, Dysport, and Xeomin (wrinkle relaxers). " +
				"(2) Explain that the provider will evaluate which is best at their appointment. " +
				"(3) Proceed to book them — we'll search availability under the wrinkle relaxer category and leave a note for the provider. Ask for their full name to get started.",
		})
	} else if prefs.ServiceInterest != "" && prefs.Name == "" && !lastAssistantAskedForName(pc.history) {
		// Name guardrail
		pc.history = append(pc.history, ChatMessage{
			Role: ChatRoleSystem,
			Content: "[SYSTEM GUARDRAIL] The patient mentioned a service but you do NOT have their name yet. " +
				"NAME is #1 in the Moxie checklist and MUST be collected before anything else. " +
				"You MUST ask for their full name NOW. Do NOT ask about patient type, schedule, provider, or email yet. " +
				"Ask something like: 'Great choice! May I have your full name?'",
		})
	}

	// Name confirmation guardrail — confirm name before moving to patient type.
	// If the name was just provided and the last assistant message didn't confirm it, force confirmation.
	if prefs.ServiceInterest != "" && prefs.Name != "" && prefs.PatientType == "" {
		lastAssistant := lastAssistantContent(pc.history)
		nameConfirmed := lastAssistant != "" && (strings.Contains(strings.ToLower(lastAssistant), strings.ToLower(prefs.Name)) ||
			assistantConfirmedName(lastAssistant))
		userConfirmedName := lastUserConfirmedName(pc.history)

		if !nameConfirmed {
			// Name was just given — confirm it first, don't ask patient type yet
			pc.history = append(pc.history, ChatMessage{
				Role: ChatRoleSystem,
				Content: fmt.Sprintf("[SYSTEM GUARDRAIL] The patient just gave their name as %s. "+
					"You MUST confirm the name back to them and WAIT for their reply. "+
					"Do NOT ask any other questions in the same message. "+
					"Just say something like: 'Got it — %s, right?' and wait for confirmation.", prefs.Name, prefs.Name),
			})
		} else if !userConfirmedName {
			// We confirmed the name but haven't gotten the user's yes/no yet — wait
			// (No guardrail needed, just don't inject patient type guardrail)
		} else {
			// Name is confirmed — proceed to patient type
			pc.history = append(pc.history, ChatMessage{
				Role: ChatRoleSystem,
				Content: "[SYSTEM GUARDRAIL] You have the patient's name and service interest. " +
					"Next in the checklist is PATIENT TYPE (#3). " +
					"You MUST ask if they are a new or returning patient NOW. Do NOT ask about schedule, email, or provider yet. " +
					"Ask something like: 'Have you visited us before, or would this be your first time?'",
			})
		}
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

// lastAssistantContent returns the content of the most recent assistant message.
func lastAssistantContent(history []ChatMessage) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == ChatRoleAssistant {
			return history[i].Content
		}
	}
	return ""
}

// assistantConfirmedName checks if the assistant message contains a name confirmation pattern.
func assistantConfirmedName(msg string) bool {
	lower := strings.ToLower(msg)
	confirmPatterns := []string{
		"right?", "correct?", "did i get that right",
		"is that right", "is that correct", "did i spell",
		"got it —", "got it -", "got it,",
	}
	for _, p := range confirmPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// lastUserConfirmedName checks if the most recent user message after a name confirmation
// is an affirmative response (yes, yeah, correct, etc.).
func lastUserConfirmedName(history []ChatMessage) bool {
	// Find the last assistant message that confirmed a name, then check the user reply
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == ChatRoleUser {
			lower := strings.ToLower(strings.TrimSpace(history[i].Content))
			// Check if this is an affirmative reply
			affirmatives := []string{
				"yes", "yeah", "yep", "yup", "correct", "that's right",
				"right", "that's correct", "you got it", "perfect", "exactly",
				"that's me", "thats right", "thats correct", "si", "mhm", "uh huh",
			}
			for _, a := range affirmatives {
				if strings.Contains(lower, a) {
					return true
				}
			}
			// Also check if they corrected the name — that counts as "confirmed" (with new name)
			if strings.Contains(lower, "no") || strings.Contains(lower, "actually") ||
				strings.Contains(lower, "it's") || strings.Contains(lower, "its ") {
				return true // They responded — name interaction is done (correction handled by extractor)
			}
			return false // User replied with something but not a confirmation
		}
		if history[i].Role == ChatRoleAssistant && assistantConfirmedName(history[i].Content) {
			return false // Found confirmation but no user reply yet
		}
	}
	return false
}
