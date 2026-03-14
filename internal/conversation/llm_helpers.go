package conversation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
)

// splitSystemAndMessages separates system messages from user/assistant messages
// in a conversation history, filtering out empty messages.
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

// formatIntroMessage builds a structured introduction message from a start
// request, including org/lead IDs, channel, source, and metadata.
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

// appendContext enriches the conversation history with contextual system
// messages including deposit status, lead preferences, business hours,
// RAG snippets, and real-time EMR availability.
func (s *LLMService) appendContext(ctx context.Context, history []ChatMessage, orgID, leadID, clinicID, query string) []ChatMessage {
	history = s.appendDepositContext(ctx, history, orgID, leadID)
	history = s.appendLeadPreferenceContext(ctx, history, orgID, leadID)
	history = s.appendClinicContext(ctx, history, orgID, query)
	history = s.appendRAGContext(ctx, history, clinicID, query)
	history = s.appendEMRAvailability(ctx, history, query)
	return history
}

// appendDepositContext checks payment status and injects deposit guardrails
// into the conversation history to prevent duplicate deposits.
func (s *LLMService) appendDepositContext(ctx context.Context, history []ChatMessage, orgID, leadID string) []ChatMessage {
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
					content := depositContextForStatus(status)
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
	return history
}

// depositContextForStatus returns the appropriate system message for a given
// deposit payment status.
func depositContextForStatus(status string) string {
	switch status {
	case "succeeded":
		return "IMPORTANT: This patient has ALREADY PAID their deposit. The platform already sent a payment confirmation SMS automatically when the payment succeeded. Do NOT offer another deposit. Do NOT restart intake or offer to schedule a consultation again. Do NOT repeat the payment confirmation message. Answer their questions normally and defer personalized/medical advice to the practitioner during their consultation. If they ask about next steps: \"Our team will call you within 24 hours to confirm a specific date and time that works for you.\""
	case "deposit_pending":
		return "IMPORTANT: This patient was already sent a deposit payment link and it is still pending. Do NOT offer another deposit or claim the deposit is already received. Do NOT restart intake or offer to schedule a consultation again. Answer their questions normally and defer personalized/medical advice to the practitioner during their consultation. If they ask about payment, tell them to use the deposit link they received."
	default:
		return "IMPORTANT: This patient has an existing deposit in progress. Do NOT offer another deposit. Do NOT restart intake or offer to schedule a consultation again. Answer their questions normally and defer personalized/medical advice to the practitioner during their consultation."
	}
}

// appendLeadPreferenceContext fetches lead preferences and injects them so
// the assistant doesn't re-ask for already captured information.
func (s *LLMService) appendLeadPreferenceContext(ctx context.Context, history []ChatMessage, orgID, leadID string) []ChatMessage {
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
	return history
}

// appendClinicContext adds business hours, deposit amount, AI persona, and
// service highlights from the clinic configuration.
func (s *LLMService) appendClinicContext(ctx context.Context, history []ChatMessage, orgID, query string) []ChatMessage {
	if s.clinicStore == nil || orgID == "" {
		return history
	}
	cfg, err := s.clinicStore.Get(ctx, orgID)
	if err != nil {
		s.logger.Warn("failed to fetch clinic config", "org_id", orgID, "error", err)
		return history
	}
	if cfg == nil {
		return history
	}
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
	return history
}

// appendRAGContext retrieves relevant knowledge base snippets for the query
// and adds them to the conversation history.
func (s *LLMService) appendRAGContext(ctx context.Context, history []ChatMessage, clinicID, query string) []ChatMessage {
	if s.rag == nil || strings.TrimSpace(query) == "" {
		return history
	}
	snippets, err := s.rag.Query(ctx, clinicID, query, 3)
	if err != nil {
		s.logger.Error("failed to retrieve RAG context", "error", err)
		return history
	}
	if len(snippets) == 0 {
		return history
	}
	builder := strings.Builder{}
	builder.WriteString("Relevant clinic context:\n")
	for i, snippet := range snippets {
		builder.WriteString(fmt.Sprintf("%d. %s\n", i+1, snippet))
	}
	history = append(history, ChatMessage{
		Role:    ChatRoleSystem,
		Content: builder.String(),
	})
	return history
}

// appendEMRAvailability checks if the query mentions booking intent and, if
// so, fetches real-time appointment slots from the EMR system.
func (s *LLMService) appendEMRAvailability(ctx context.Context, history []ChatMessage, query string) []ChatMessage {
	if s.emr == nil || !s.emr.IsConfigured() || !containsBookingIntent(query) {
		return history
	}
	slots, err := s.emr.GetUpcomingAvailability(ctx, 7, "")
	if err != nil {
		s.logger.Warn("failed to fetch EMR availability", "error", err)
		return history
	}
	if len(slots) == 0 {
		return history
	}
	availabilityContext := FormatSlotsForLLM(slots, 5)
	history = append(history, ChatMessage{
		Role:    ChatRoleSystem,
		Content: "Real-time appointment availability from clinic calendar:\n" + availabilityContext,
	})
	return history
}

// trimHistory keeps the most recent messages up to the given limit, always
// preserving the first system message if present.
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
