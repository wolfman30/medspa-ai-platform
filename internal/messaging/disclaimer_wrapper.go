package messaging

import (
	"context"
	"strings"
	"sync"

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
		"has_conversation_store", cfg.ConversationStore != nil,
		"has_transcript_store", cfg.TranscriptStore != nil,
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
	seen            sync.Map
}

// SendReply adds a disclaimer before sending when configured.
func (d *DisclaimerMessenger) SendReply(ctx context.Context, reply conversation.OutboundReply) error {
	if d == nil || d.service == nil || d.inner == nil {
		return nil
	}
	isFirst := true
	if d.firstOnly {
		isFirst = d.isFirstAssistantMessage(ctx, reply.ConversationID)
		d.logger.Info("disclaimer: first message check result", "conversation_id", reply.ConversationID, "is_first", isFirst, "first_only_mode", d.firstOnly)
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
		d.logger.Info("disclaimer: empty conversation ID, treating as first message")
		return true
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// Check in-memory cache first (most reliable for this session)
	if _, alreadySeen := d.seen.Load(conversationID); alreadySeen {
		d.logger.Info("disclaimer: already seen in memory", "conversation_id", conversationID)
		return false
	}

	// Check persistent stores as backup
	if d.conversation != nil {
		has, err := d.conversation.HasAssistantMessage(ctx, conversationID)
		if err == nil && has {
			d.logger.Info("disclaimer: conversation store has assistant", "conversation_id", conversationID)
			d.seen.Store(conversationID, struct{}{})
			return false
		}
		if err != nil {
			d.logger.Warn("disclaimer: conversation store check failed", "error", err, "conversation_id", conversationID)
		}
	}
	if d.transcriptStore != nil {
		has, err := d.transcriptStore.HasAssistantMessage(ctx, conversationID)
		if err == nil && has {
			d.logger.Info("disclaimer: transcript store has assistant", "conversation_id", conversationID)
			d.seen.Store(conversationID, struct{}{})
			return false
		}
		if err != nil {
			d.logger.Warn("disclaimer: transcript store check failed", "error", err, "conversation_id", conversationID)
		}
	}

	// First message for this conversation - mark as seen for future checks
	d.seen.Store(conversationID, struct{}{})
	d.logger.Info("disclaimer: first message, marking as seen", "conversation_id", conversationID)
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
