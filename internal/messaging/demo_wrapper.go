package messaging

import (
	"context"
	"strings"

	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// DemoModeMessenger wraps a ReplyMessenger to add 10DLC-compliant demo disclaimers.
// This is used during testing with a registered 10DLC campaign to ensure messages
// match the registered sample messages (prefix + content + suffix).
type DemoModeMessenger struct {
	inner  conversation.ReplyMessenger
	prefix string // e.g., "AI Wolf Solutions: "
	suffix string // e.g., " Demo only; no real services. Reply STOP to opt out."
	logger *logging.Logger
}

// DemoModeConfig configures the demo mode wrapper.
type DemoModeConfig struct {
	Enabled bool
	Prefix  string
	Suffix  string
	Logger  *logging.Logger
}

// WrapWithDemoMode optionally wraps a messenger with demo mode disclaimers.
// If not enabled, returns the original messenger unchanged.
func WrapWithDemoMode(messenger conversation.ReplyMessenger, cfg DemoModeConfig) conversation.ReplyMessenger {
	if !cfg.Enabled || messenger == nil {
		return messenger
	}

	if cfg.Logger == nil {
		cfg.Logger = logging.Default()
	}

	cfg.Logger.Info("demo mode enabled for outbound SMS",
		"prefix", cfg.Prefix,
		"suffix_length", len(cfg.Suffix),
	)

	return &DemoModeMessenger{
		inner:  messenger,
		prefix: cfg.Prefix,
		suffix: cfg.Suffix,
		logger: cfg.Logger,
	}
}

// SendReply wraps the message body with demo disclaimers before sending.
func (d *DemoModeMessenger) SendReply(ctx context.Context, reply conversation.OutboundReply) error {
	// Wrap the message with prefix and suffix
	originalBody := strings.TrimSpace(reply.Body)
	wrappedBody := d.prefix + originalBody + d.suffix

	// SMS limit is 1600 chars for concatenated messages, but aim for under 160 for single segment
	// Log if we're getting long
	if len(wrappedBody) > 160 {
		d.logger.Debug("demo mode message exceeds single SMS segment",
			"original_len", len(originalBody),
			"wrapped_len", len(wrappedBody),
		)
	}

	reply.Body = wrappedBody
	return d.inner.SendReply(ctx, reply)
}
