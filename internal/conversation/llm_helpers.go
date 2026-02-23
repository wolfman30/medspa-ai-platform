package conversation

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
)

func splitSystemAndMessages(history []ChatMessage) ([]string, []ChatMessage) {
	if len(history) == 0 {
		return nil, nil
	}
	system := make([]string, 0, 4)
	messages := make([]ChatMessage, 0, len(history))
	for _, msg := range history {
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		if msg.Role == ChatRoleSystem {
			system = append(system, msg.Content)
			continue
		}
		messages = append(messages, msg)
	}
	return system, messages
}

func formatIntroMessage(req StartRequest, conversationID string) string {
	builder := strings.Builder{}
	builder.WriteString("Lead introduction:\n")
	builder.WriteString(fmt.Sprintf("Conversation ID: %s\n", conversationID))
	if req.OrgID != "" {
		builder.WriteString(fmt.Sprintf("Org ID: %s\n", req.OrgID))
	}
	if req.LeadID != "" {
		builder.WriteString(fmt.Sprintf("Lead ID: %s\n", req.LeadID))
	}
	if req.Channel != ChannelUnknown {
		builder.WriteString(fmt.Sprintf("Channel: %s\n", req.Channel))
	}
	if req.Source != "" {
		builder.WriteString(fmt.Sprintf("Source: %s\n", req.Source))
	}
	if req.From != "" {
		builder.WriteString(fmt.Sprintf("From: %s\n", req.From))
	}
	if req.To != "" {
		builder.WriteString(fmt.Sprintf("To: %s\n", req.To))
	}
	if len(req.Metadata) > 0 {
		builder.WriteString("Metadata:\n")
		for k, v := range req.Metadata {
			builder.WriteString(fmt.Sprintf("- %s: %s\n", k, v))
		}
	}
	builder.WriteString(fmt.Sprintf("Message: %s", req.Intro))
	return builder.String()
}

func (s *LLMService) appendContext(ctx context.Context, history []ChatMessage, orgID, leadID, clinicID, query string) []ChatMessage {
	// Append payment status context if available
	depositContextInjected := false
	if s.paymentChecker != nil && orgID != "" && leadID != "" {
		orgUUID, orgErr := uuid.Parse(orgID)
		leadUUID, leadErr := uuid.Parse(leadID)
		if orgErr == nil && leadErr == nil {
			type openDepositStatusChecker interface {
				OpenDepositStatus(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID) (string, error)
			}
			if statusChecker, ok := s.paymentChecker.(openDepositStatusChecker); ok {
				status, err := statusChecker.OpenDepositStatus(ctx, orgUUID, leadUUID)
				if err != nil {
					s.logger.Warn("failed to check payment status", "org_id", orgID, "lead_id", leadID, "error", err)
				} else if strings.TrimSpace(status) != "" {
					content := "IMPORTANT: This patient has an existing deposit in progress. Do NOT offer another deposit. Do NOT restart intake or offer to schedule a consultation again. Answer their questions normally and defer personalized/medical advice to the practitioner during their consultation."
					switch status {
					case "succeeded":
						content = "IMPORTANT: This patient has ALREADY PAID their deposit. The platform already sent a payment confirmation SMS automatically when the payment succeeded. Do NOT offer another deposit. Do NOT restart intake or offer to schedule a consultation again. Do NOT repeat the payment confirmation message. Answer their questions normally and defer personalized/medical advice to the practitioner during their consultation. If they ask about next steps: \"Our team will call you within 24 hours to confirm a specific date and time that works for you.\""
					case "deposit_pending":
						content = "IMPORTANT: This patient was already sent a deposit payment link and it is still pending. Do NOT offer another deposit or claim the deposit is already received. Do NOT restart intake or offer to schedule a consultation again. Answer their questions normally and defer personalized/medical advice to the practitioner during their consultation. If they ask about payment, tell them to use the deposit link they received."
					}
					history = append(history, ChatMessage{
						Role:    ChatRoleSystem,
						Content: content,
					})
					depositContextInjected = true
				}
			} else {
				hasDeposit, err := s.paymentChecker.HasOpenDeposit(ctx, orgUUID, leadUUID)
				if err != nil {
					s.logger.Warn("failed to check payment status", "org_id", orgID, "lead_id", leadID, "error", err)
				} else if hasDeposit {
					history = append(history, ChatMessage{
						Role:    ChatRoleSystem,
						Content: "IMPORTANT: This patient has an existing deposit in progress (pending payment or already paid). Do NOT offer another deposit. Do NOT restart intake or offer to schedule a consultation again. Do NOT repeat any payment confirmation message. Answer their questions normally and defer personalized/medical advice to the practitioner during their consultation. If they ask about next steps: \"Our team will call you within 24 hours to confirm a specific date and time that works for you.\"",
					})
					depositContextInjected = true
				}
			}
		}
	}

	// If the payment checker is unavailable (or hasn't persisted yet) but the conversation indicates
	// the patient already agreed to a deposit, inject guardrails so we don't restart intake.
	if !depositContextInjected && conversationHasDepositAgreement(history) {
		history = append(history, ChatMessage{
			Role:    ChatRoleSystem,
			Content: "IMPORTANT: This patient already agreed to the deposit and is in the booking flow. Do NOT restart intake or offer to schedule a consultation again. Answer their questions normally and defer personalized/medical advice to the practitioner during their consultation.",
		})
	}

	// Append lead preferences so the assistant doesn't re-ask for captured info.
	if s.leadsRepo != nil && orgID != "" && leadID != "" {
		lead, err := s.leadsRepo.GetByID(ctx, orgID, leadID)
		if err != nil {
			if !errors.Is(err, leads.ErrLeadNotFound) {
				s.logger.Warn("failed to fetch lead preferences", "org_id", orgID, "lead_id", leadID, "error", err)
			}
		} else if lead != nil {
			if content := formatLeadPreferenceContext(lead); content != "" {
				history = append(history, ChatMessage{
					Role:    ChatRoleSystem,
					Content: content,
				})
			}
		}
	}

	// Append clinic business hours context and deposit amount if available
	if s.clinicStore != nil && orgID != "" {
		cfg, err := s.clinicStore.Get(ctx, orgID)
		if err != nil {
			s.logger.Warn("failed to fetch clinic config", "org_id", orgID, "error", err)
		} else if cfg != nil {
			hoursContext := cfg.BusinessHoursContext(time.Now())
			history = append(history, ChatMessage{
				Role:    ChatRoleSystem,
				Content: hoursContext,
			})
			// Explicitly state the exact deposit amount to prevent LLM from guessing ranges
			depositAmount := cfg.DepositAmountCents
			if depositAmount <= 0 {
				depositAmount = 5000 // default $50
			}
			depositDollars := depositAmount / 100
			history = append(history, ChatMessage{
				Role:    ChatRoleSystem,
				Content: fmt.Sprintf("DEPOSIT AMOUNT: This clinic's deposit is exactly $%d. NEVER say a range like '$50-100'. Always state the exact amount: $%d.", depositDollars, depositDollars),
			})
			// Add AI persona context for personalized voice
			if personaContext := cfg.AIPersonaContext(); personaContext != "" {
				history = append(history, ChatMessage{
					Role:    ChatRoleSystem,
					Content: personaContext,
				})
			}
			if highlightContext := buildServiceHighlightsContext(cfg, query); highlightContext != "" {
				history = append(history, ChatMessage{
					Role:    ChatRoleSystem,
					Content: highlightContext,
				})
			}
		}
	}

	// Append RAG context if available
	if s.rag != nil && strings.TrimSpace(query) != "" {
		snippets, err := s.rag.Query(ctx, clinicID, query, 3)
		if err != nil {
			s.logger.Error("failed to retrieve RAG context", "error", err)
		} else if len(snippets) > 0 {
			builder := strings.Builder{}
			builder.WriteString("Relevant clinic context:\n")
			for i, snippet := range snippets {
				builder.WriteString(fmt.Sprintf("%d. %s\n", i+1, snippet))
			}
			history = append(history, ChatMessage{
				Role:    ChatRoleSystem,
				Content: builder.String(),
			})
		}
	}

	// Append real-time availability if EMR is configured and query mentions booking/appointment
	if s.emr != nil && s.emr.IsConfigured() && containsBookingIntent(query) {
		slots, err := s.emr.GetUpcomingAvailability(ctx, 7, "")
		if err != nil {
			s.logger.Warn("failed to fetch EMR availability", "error", err)
		} else if len(slots) > 0 {
			availabilityContext := FormatSlotsForLLM(slots, 5)
			history = append(history, ChatMessage{
				Role:    ChatRoleSystem,
				Content: "Real-time appointment availability from clinic calendar:\n" + availabilityContext,
			})
		}
	} else if s.browser != nil && s.browser.IsConfigured() && containsBookingIntent(query) {
		// Fall back to browser scraping if EMR is not configured but browser adapter is
		if s.clinicStore != nil {
			cfg, err := s.clinicStore.Get(ctx, orgID)
			if err == nil && cfg != nil && cfg.BookingURL != "" {
				slots, err := s.browser.GetUpcomingAvailability(ctx, cfg.BookingURL, 7)
				if err != nil {
					s.logger.Warn("failed to fetch browser availability", "error", err, "url", cfg.BookingURL)
				} else if len(slots) > 0 {
					availabilityContext := FormatSlotsForLLM(slots, 5)
					history = append(history, ChatMessage{
						Role:    ChatRoleSystem,
						Content: "Real-time appointment availability from booking page:\n" + availabilityContext,
					})
				}
			}
		}
	}

	return history
}

// containsBookingIntent checks if the user message suggests they want to book.
func containsBookingIntent(msg string) bool {
	msg = strings.ToLower(msg)
	keywords := []string{"book", "appointment", "schedule", "available", "availability", "when can", "open slot", "time slot"}
	for _, kw := range keywords {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

func trimHistory(history []ChatMessage, limit int) []ChatMessage {
	if limit <= 0 || len(history) <= limit {
		return history
	}
	if len(history) == 0 {
		return history
	}

	var result []ChatMessage
	system := history[0]
	if system.Role == ChatRoleSystem {
		result = append(result, system)
		remaining := limit - 1
		if remaining <= 0 {
			return result
		}
		start := len(history) - remaining
		if start < 1 {
			start = 1
		}
		result = append(result, history[start:]...)
		return result
	}
	return history[len(history)-limit:]
}

// sanitizeSMSResponse strips markdown formatting that doesn't render in SMS.
// This includes **bold**, *italics*, bullet points, and other markdown syntax.
func sanitizeSMSResponse(msg string) string {
	// Remove bold markers **text** -> text
	msg = strings.ReplaceAll(msg, "**", "")
	// Remove italic markers *text* -> text (be careful not to remove asterisks in lists)
	// Only remove single asterisks that are likely italics (surrounded by non-space)
	msg = smsItalicRE.ReplaceAllString(msg, "$1")
	// Remove markdown bullet points at start of lines: "- item" -> "item"
	msg = smsBulletRE.ReplaceAllString(msg, "")
	// Remove numbered list formatting: "1. item" -> "item"
	msg = smsNumberedRE.ReplaceAllString(msg, "")
	// Clean up any double spaces that might result
	msg = smsMultiSpaceRE.ReplaceAllString(msg, " ")
	return strings.TrimSpace(msg)
}

// extractAndSavePreferences extracts scheduling preferences from conversation history and saves them.
func (s *LLMService) extractAndSavePreferences(ctx context.Context, leadID string, history []ChatMessage) error {
	return s.savePreferencesFromHistory(ctx, leadID, history, true)
}

func (s *LLMService) savePreferencesFromHistory(ctx context.Context, leadID string, history []ChatMessage, addNote bool) error {
	if s == nil || s.leadsRepo == nil || strings.TrimSpace(leadID) == "" {
		return nil
	}
	prefs, ok := extractPreferences(history, nil)
	if !ok {
		return nil
	}
	if addNote {
		prefs.Notes = fmt.Sprintf("Auto-extracted from conversation at %s", time.Now().Format(time.RFC3339))
	}
	return s.leadsRepo.UpdateSchedulingPreferences(ctx, leadID, prefs)
}

func (s *LLMService) savePreferencesNoNote(ctx context.Context, leadID string, history []ChatMessage, reason string) {
	if s == nil {
		return
	}
	if err := s.savePreferencesFromHistory(ctx, leadID, history, false); err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to save scheduling preferences", "lead_id", leadID, "reason", reason, "error", err)
		}
	}
}

var (
	priceInquiryRE = regexp.MustCompile(`(?i)\b(?:how much|price|pricing|cost|rate|rates|charge)\b`)
	phiPrefaceRE   = regexp.MustCompile(`(?i)\b(?:diagnosed|diagnosis|my condition|my symptoms|i have|i've had|i am|i'm)\b`)
	// PHI keywords with word boundaries to avoid false positives (e.g., "sti" matching in "existing")
	phiKeywordsRE = regexp.MustCompile(`(?i)\b(?:diabetes|hiv|aids|cancer|hepatitis|pregnant|pregnancy|depression|anxiety|bipolar|schizophrenia|asthma|hypertension|blood pressure|infection|herpes|std|sti)\b`)
	// Strong medical advice cues — always trigger with any medical/service context
	strongMedicalCueRE = regexp.MustCompile(`(?i)\b(?:is it safe|safe to|ok to|okay to|contraindications?|side effects?|dosage|dose|mg|milligram|interactions?|mix with|stop taking)\b`)
	// Weak medical advice cues — only trigger with medical-specific context (not service names alone)
	weakMedicalCueRE = regexp.MustCompile(`(?i)\b(?:should i|can i)\b`)
	// Full medical context (services + medical terms) — used with strong cues
	medicalContextRE = regexp.MustCompile(`(?i)\b(?:botox|filler|laser|microneedling|facial|peel|dermaplaning|prp|injectable|medication|medicine|meds|prescription|ibuprofen|tylenol|acetaminophen|antibiotics?|painkillers?|blood pressure|pregnan(?:t|cy)|breastfeed(?:ing)?|allerg(?:y|ic))\b`)
	// Medical-specific context (conditions/medications only, no service names) — used with weak cues
	medicalSpecificContextRE = regexp.MustCompile(`(?i)\b(?:medication|medicine|meds|prescription|ibuprofen|tylenol|acetaminophen|antibiotics?|painkillers?|blood pressure|pregnan(?:t|cy)|breastfeed(?:ing)?|allerg(?:y|ic))\b`)
)

func isPriceInquiry(message string) bool {
	message = strings.TrimSpace(message)
	if message == "" {
		return false
	}
	return priceInquiryRE.MatchString(message) || strings.Contains(message, "$")
}

func isAmbiguousHelp(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return false
	}
	if !(strings.Contains(message, "help") || strings.Contains(message, "question") || strings.Contains(message, "info")) {
		return false
	}
	// If the user already mentioned booking or a service, let the LLM handle it.
	// "available" indicates booking intent (e.g., "do you have anything available Thursday?")
	for _, kw := range []string{"book", "appointment", "schedule", "available", "opening", "botox", "filler", "facial", "laser", "peel", "microneedling", "hydrafacial"} {
		if strings.Contains(message, kw) {
			return false
		}
	}
	return true
}

func isQuestionSelection(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return false
	}
	message = strings.Trim(message, ".!?")
	message = strings.Join(strings.Fields(message), " ")
	if strings.Contains(message, "?") {
		return false
	}

	for _, kw := range []string{"book", "appointment", "schedule", "botox", "filler", "facial", "laser", "peel", "microneedling"} {
		if strings.Contains(message, kw) {
			return false
		}
	}

	switch message {
	case "question",
		"quick question",
		"a question",
		"a quick question",
		"just a question",
		"just a quick question",
		"i had a question",
		"i had a quick question",
		"i just had a question",
		"i just had a quick question",
		"i have a question",
		"i have a quick question",
		"i just have a question",
		"i just have a quick question",
		"i got a question",
		"i got a quick question",
		"i've got a question",
		"i've got a quick question",
		"had a question",
		"had a quick question",
		"have a question",
		"have a quick question",
		"got a question",
		"got a quick question",
		"question please",
		"quick question please",
		"quick question for you",
		"i have a quick question for you",
		"i had a quick question for you",
		"i just had a quick question for you",
		"just a question please",
		"just a quick question please":
		return true
	default:
		return false
	}
}

func detectServiceKey(message string, cfg *clinic.Config) string {
	message = strings.ToLower(message)
	if strings.TrimSpace(message) == "" {
		return ""
	}
	candidates := make([]string, 0, 16)
	if cfg != nil {
		for key := range cfg.ServicePriceText {
			candidates = append(candidates, key)
		}
		for key := range cfg.ServiceDepositAmountCents {
			candidates = append(candidates, key)
		}
		for _, svc := range cfg.Services {
			candidates = append(candidates, svc)
		}
	}
	candidates = append(candidates, "botox", "filler", "dermal filler", "consultation", "laser", "facial", "peel", "microneedling")

	for _, candidate := range candidates {
		key := strings.ToLower(strings.TrimSpace(candidate))
		if key == "" {
			continue
		}
		if strings.Contains(message, key) {
			// Resolve through aliases to canonical service name for price lookup.
			if cfg != nil {
				if resolved, ok := cfg.ServiceAliases[key]; ok {
					return strings.ToLower(resolved)
				}
			}
			return key
		}
	}
	return ""
}

func detectPHI(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return false
	}
	if !phiPrefaceRE.MatchString(message) {
		return false
	}
	// Use regex with word boundaries to avoid false positives
	// (e.g., "sti" matching inside "existing")
	return phiKeywordsRE.MatchString(message)
}

func detectMedicalAdvice(message string) []string {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return nil
	}
	hasStrongCue := strongMedicalCueRE.MatchString(message)
	hasWeakCue := weakMedicalCueRE.MatchString(message)
	if !hasStrongCue && !hasWeakCue {
		return nil
	}
	// Strong cues ("is it safe", "side effects", etc.) trigger with any medical context
	// Weak cues ("can i", "should i") only trigger with medical-specific context
	// (medications, conditions) — not just service names like "botox" which indicate booking intent
	if hasStrongCue {
		if !medicalContextRE.MatchString(message) {
			return nil
		}
	} else {
		if !medicalSpecificContextRE.MatchString(message) {
			return nil
		}
	}
	keywords := []string{}
	for _, kw := range []string{
		"botox", "filler", "laser", "microneedling", "facial", "peel", "dermaplaning", "prp", "injectable",
		"medication", "medicine", "meds", "prescription", "ibuprofen", "tylenol", "acetaminophen", "antibiotic", "antibiotics",
		"painkiller", "painkillers", "blood pressure", "pregnant", "pregnancy", "breastfeeding", "allergy", "allergic",
		"contraindication", "contraindications", "side effects", "dosage", "dose", "interaction", "interactions", "mix with",
	} {
		if strings.Contains(message, kw) {
			keywords = append(keywords, kw)
		}
	}
	if len(keywords) == 0 {
		keywords = append(keywords, "medical_advice_request")
	}
	return keywords
}

func (s *LLMService) appendLeadNote(ctx context.Context, orgID, leadID, note string) {
	if s == nil || s.leadsRepo == nil {
		return
	}
	orgID = strings.TrimSpace(orgID)
	leadID = strings.TrimSpace(leadID)
	note = strings.TrimSpace(note)
	if orgID == "" || leadID == "" || note == "" {
		return
	}
	lead, err := s.leadsRepo.GetByID(ctx, orgID, leadID)
	if err != nil || lead == nil {
		return
	}
	existing := strings.TrimSpace(lead.SchedulingNotes)
	switch {
	case existing == "":
		existing = note
	case strings.Contains(existing, note):
		// Avoid duplication.
	default:
		existing = existing + " | " + note
	}
	_ = s.leadsRepo.UpdateSchedulingPreferences(ctx, leadID, leads.SchedulingPreferences{Notes: existing})
}

// isCapitalized checks if a string starts with an uppercase letter
func isCapitalized(s string) bool {
	if len(s) == 0 {
		return false
	}
	return s[0] >= 'A' && s[0] <= 'Z'
}

func formatLeadPreferenceContext(lead *leads.Lead) string {
	if lead == nil {
		return ""
	}
	lines := make([]string, 0, 5)
	name := strings.TrimSpace(lead.Name)
	if name != "" && !looksLikePhone(name, lead.Phone) {
		label := "Name"
		if len(strings.Fields(name)) == 1 {
			label = "Name (first only)"
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", label, name))
	}
	service := strings.TrimSpace(lead.ServiceInterest)
	if service != "" {
		lines = append(lines, fmt.Sprintf("- Service: %s", service))
	}
	patientType := strings.TrimSpace(lead.PatientType)
	if patientType != "" {
		lines = append(lines, fmt.Sprintf("- Patient type: %s", patientType))
	}
	days := strings.TrimSpace(lead.PreferredDays)
	if days != "" {
		lines = append(lines, fmt.Sprintf("- Preferred days: %s", days))
	}
	times := strings.TrimSpace(lead.PreferredTimes)
	if times != "" {
		lines = append(lines, fmt.Sprintf("- Preferred times: %s", times))
	}
	if len(lines) == 0 {
		return ""
	}
	return "Known scheduling preferences from earlier messages:\n" + strings.Join(lines, "\n")
}

func looksLikePhone(name string, phone string) bool {
	name = strings.TrimSpace(name)
	phone = strings.TrimSpace(phone)
	if name == "" {
		return false
	}
	if phone != "" && name == phone {
		return true
	}
	digits := 0
	for i := 0; i < len(name); i++ {
		if name[i] >= '0' && name[i] <= '9' {
			digits++
		}
	}
	return digits >= 7
}

// splitName splits a full name into first and last name.
// "Andy Wolf" → ("Andy", "Wolf"), "Madonna" → ("Madonna", ""), "  " → ("", "").
func splitName(full string) (string, string) {
	parts := strings.Fields(full)
	switch len(parts) {
	case 0:
		return "", ""
	case 1:
		return parts[0], ""
	default:
		return parts[0], strings.Join(parts[1:], " ")
	}
}
