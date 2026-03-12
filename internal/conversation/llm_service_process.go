package conversation

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

// ProcessMessage continues an existing conversation with Redis-backed context.
// If the conversation doesn't exist, it automatically starts a new one.
//
// The method is organised into sequential phases, each extracted into its own
// method on LLMService. A shared processContext carries state between phases.
func (s *LLMService) ProcessMessage(ctx context.Context, req MessageRequest) (*Response, error) {
	if isVoiceChannel(req.Channel) && s.voiceModel != "" {
		ctx = context.WithValue(ctx, ctxKeyVoiceModel, s.voiceModel)
	}
	if strings.TrimSpace(req.ConversationID) == "" {
		return nil, errors.New("conversation: conversationID required")
	}

	pc, earlyResp := s.newProcessContext(ctx, req)
	if earlyResp != nil {
		return earlyResp, nil
	}

	s.logger.Info("ProcessMessage called",
		"conversation_id", req.ConversationID,
		"org_id", req.OrgID,
		"lead_id", req.LeadID,
		"message", pc.redactedMessage,
	)

	ctx, span := llmTracer.Start(ctx, "conversation.message")
	defer span.End()
	span.SetAttributes(
		attribute.String("medspa.org_id", req.OrgID),
		attribute.String("medspa.conversation_id", req.ConversationID),
		attribute.String("medspa.channel", string(req.Channel)),
	)
	pc.span = span

	resp, histErr := s.loadHistory(ctx, pc)
	if histErr != nil {
		return nil, histErr
	}
	if resp != nil {
		return resp, nil
	}
	if pc.history == nil {
		return s.StartConversation(ctx, StartRequest{
			OrgID:          req.OrgID,
			ConversationID: req.ConversationID,
			LeadID:         req.LeadID,
			ClinicID:       req.ClinicID,
			Intro:          pc.rawMessage,
			Channel:        req.Channel,
			From:           req.From,
			To:             req.To,
			Metadata:       req.Metadata,
		})
	}

	if resp := s.handleSafetyDeflections(ctx, pc); resp != nil {
		return resp, nil
	}

	pc.history = s.appendContext(ctx, pc.history, req.OrgID, req.LeadID, req.ClinicID, pc.rawMessage)
	pc.history = append(pc.history, ChatMessage{Role: ChatRoleUser, Content: pc.rawMessage})

	if s.clinicStore != nil && req.OrgID != "" {
		if loaded, err := s.clinicStore.Get(ctx, req.OrgID); err == nil {
			pc.cfg = loaded
		}
	}

	if resp := s.handleDeterministicGuardrails(ctx, pc); resp != nil {
		return resp, nil
	}
	if resp := s.handleFAQClassification(ctx, pc); resp != nil {
		return resp, nil
	}

	s.loadTimeSelectionState(ctx, pc)
	s.handleActiveTimeSelection(ctx, pc)
	s.injectMoxieQualificationGuardrails(ctx, pc)

	reply, err := s.generateResponse(ctx, pc.history)
	if err != nil {
		return nil, err
	}
	reply = sanitizeSMSResponse(reply)
	pc.reply = reply
	pc.history = append(pc.history, ChatMessage{Role: ChatRoleAssistant, Content: reply})
	pc.history = trimHistory(pc.history, maxHistoryMessages)
	if err := s.history.Save(ctx, req.ConversationID, pc.history); err != nil {
		span.RecordError(err)
		return nil, err
	}

	s.handlePostLLMResponse(ctx, pc)

	return &Response{
		ConversationID:        req.ConversationID,
		Message:               pc.reply,
		Timestamp:             time.Now().UTC(),
		DepositIntent:         pc.depositIntent,
		TimeSelectionResponse: pc.timeSelectionResponse,
		BookingRequest:        pc.bookingRequest,
		AsyncAvailability:     pc.asyncAvailability,
	}, nil
}

// GetHistory retrieves the conversation history for a given conversation ID.
func (s *LLMService) GetHistory(ctx context.Context, conversationID string) ([]Message, error) {
	history, err := s.history.Load(ctx, conversationID)
	if err != nil {
		return nil, err
	}

	var messages []Message
	for _, msg := range history {
		if msg.Role == ChatRoleSystem {
			continue
		}
		messages = append(messages, Message{Role: msg.Role, Content: msg.Content})
	}
	return messages, nil
}
