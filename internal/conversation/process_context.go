package conversation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
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
	asyncAvailability     *AsyncAvailabilityRequest
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
func (s *LLMService) loadHistory(ctx context.Context, pc *processContext) (*Response, error) {
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
			return s.bootstrapNewConversation(ctx, pc), nil
		}
		pc.span.RecordError(err)
		return nil, fmt.Errorf("loading conversation history: %w", err)
	}

	pc.history = history

	// Count non-system user messages to enforce conversation length limit.
	userMsgCount := 0
	for _, m := range history {
		if m.Role == ChatRoleUser {
			userMsgCount++
		}
	}
	if userMsgCount >= maxConversationMessages {
		phone := ""
		if s.clinicStore != nil && pc.req.OrgID != "" {
			if cfg, err := s.clinicStore.Get(ctx, pc.req.OrgID); err == nil && cfg != nil && cfg.Phone != "" {
				phone = cfg.Phone
			}
		}
		msg := "This conversation has been quite long! For the best experience, please call us directly"
		if phone != "" {
			msg += " at " + phone
		}
		msg += " and our team will be happy to help."
		s.logger.Warn("conversation message limit reached",
			"conversation_id", pc.req.ConversationID,
			"user_messages", userMsgCount,
			"limit", maxConversationMessages,
		)
		return &Response{
			ConversationID: pc.req.ConversationID,
			Message:        msg,
			Timestamp:      time.Now().UTC(),
		}, nil
	}

	s.logger.Info("ProcessMessage: history loaded",
		"conversation_id", pc.req.ConversationID,
		"history_length", len(history),
	)
	return nil, nil
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
