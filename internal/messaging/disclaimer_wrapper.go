package messaging

import (
	"context"
	"strings"

	"github.com/wolfman30/medspa-ai-platform/internal/compliance"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// AssistantMessageChecker checks whether a conversation already has assistant messages.
type AssistantMessageChecker interface {
	HasAssistantMessage(ctx context.Context, conversationID string) (bool, error)
}

// DisclaimerWrapperConfig configures compliance disclaimer wrapping.
type DisclaimerWrapperConfig struct {
	Enabled           bool
	Level             string
	FirstMessageOnly  bool
	Logger            *logging.Logger
	Audit             *compliance.AuditService
	ConversationStore AssistantMessageChecker
	TranscriptStore   AssistantMessageChecker
}

// WrapWithDisclaimers optionally wraps a messenger to add HIPAA disclaimers.
func WrapWithDisclaimers(messenger conversation.ReplyMessenger, cfg DisclaimerWrapperConfig) conversation.ReplyMessenger {
	if !cfg.Enabled || messenger == nil {
		return messenger
	}
	if cfg.Logger == nil {
		cfg.Logger = logging.Default()
	}

	service := compliance.NewDisclaimerService(cfg.Audit, compliance.DisclaimerConfig{
		Level:            parseDisclaimerLevel(cfg.Level),
		Enabled:          true,
		FirstMessageOnly: cfg.FirstMessageOnly,
	})

	cfg.Logger.Info("disclaimer wrapper enabled for outbound SMS",
		"level", cfg.Level,
		"first_only", cfg.FirstMessageOnly,
	)

	return &DisclaimerMessenger{
		inner:           messenger,
		service:         service,
		logger:          cfg.Logger,
		firstOnly:       cfg.FirstMessageOnly,
		conversation:    cfg.ConversationStore,
		transcriptStore: cfg.TranscriptStore,
	}
}

// DisclaimerMessenger wraps a ReplyMessenger to append compliance disclaimers.
type DisclaimerMessenger struct {
	inner           conversation.ReplyMessenger
	service         *compliance.DisclaimerService
	logger          *logging.Logger
	firstOnly       bool
	conversation    AssistantMessageChecker
	transcriptStore AssistantMessageChecker
}

// SendReply adds a disclaimer before sending when configured.
func (d *DisclaimerMessenger) SendReply(ctx context.Context, reply conversation.OutboundReply) error {
	if d == nil || d.service == nil || d.inner == nil {
		return nil
	}
	isFirst := true
	if d.firstOnly {
		isFirst = d.isFirstAssistantMessage(ctx, reply.ConversationID)
		d.logger.Debug("disclaimer: first message check result", "conversation_id", reply.ConversationID, "is_first", isFirst, "first_only_mode", d.firstOnly)
	}
	body, err := d.service.AddDisclaimer(ctx, reply.Body, compliance.DisclaimerOptions{
		OrgID:          reply.OrgID,
		ConversationID: reply.ConversationID,
		LeadID:         reply.LeadID,
		IsFirstMessage: isFirst,
	})
	if err != nil {
		d.logger.Warn("failed to apply disclaimer", "error", err, "conversation_id", reply.ConversationID)
	} else {
		if body != reply.Body {
			d.logger.Info("disclaimer: added to message", "conversation_id", reply.ConversationID, "is_first", isFirst)
		}
		reply.Body = body
	}
	return d.inner.SendReply(ctx, reply)
}

func (d *DisclaimerMessenger) isFirstAssistantMessage(ctx context.Context, conversationID string) bool {
	if strings.TrimSpace(conversationID) == "" {
		d.logger.Debug("disclaimer: empty conversation ID, treating as first message")
		return true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if d.conversation != nil {
		has, err := d.conversation.HasAssistantMessage(ctx, conversationID)
		if err == nil {
			d.logger.Debug("disclaimer: conversation store check", "conversation_id", conversationID, "has_assistant", has)
			return !has
		}
		d.logger.Warn("disclaimer: conversation store check failed", "error", err, "conversation_id", conversationID)
	}
	if d.transcriptStore != nil {
		has, err := d.transcriptStore.HasAssistantMessage(ctx, conversationID)
		if err == nil {
			d.logger.Debug("disclaimer: transcript store check", "conversation_id", conversationID, "has_assistant", has)
			return !has
		}
		d.logger.Warn("disclaimer: transcript store check failed", "error", err, "conversation_id", conversationID)
	}
	d.logger.Debug("disclaimer: no stores available, treating as first message", "conversation_id", conversationID)
	return true
}

func parseDisclaimerLevel(raw string) compliance.DisclaimerLevel {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "short":
		return compliance.DisclaimerShort
	case "full":
		return compliance.DisclaimerFull
	default:
		return compliance.DisclaimerMedium
	}
}
