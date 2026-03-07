package conversation

import (
	"context"
	"strings"
	"time"
)

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
			usesMoxie = cfg.UsesMoxieBooking() || cfg.UsesBoulevardBooking()
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
