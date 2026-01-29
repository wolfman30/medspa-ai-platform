package messaging

import (
	"context"
	"errors"

	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// FailoverMessenger attempts a primary send, then falls back to a secondary provider on error.
type FailoverMessenger struct {
	primary       conversation.ReplyMessenger
	secondary     conversation.ReplyMessenger
	primaryName   string
	secondaryName string
	logger        *logging.Logger
}

// NewFailoverMessenger builds a failover messenger with named providers.
func NewFailoverMessenger(primary conversation.ReplyMessenger, primaryName string, secondary conversation.ReplyMessenger, secondaryName string, logger *logging.Logger) *FailoverMessenger {
	if logger == nil {
		logger = logging.Default()
	}
	return &FailoverMessenger{
		primary:       primary,
		secondary:     secondary,
		primaryName:   primaryName,
		secondaryName: secondaryName,
		logger:        logger,
	}
}

var _ conversation.ReplyMessenger = (*FailoverMessenger)(nil)

// SendReply tries the primary provider first, then falls back to the secondary provider on failure.
func (f *FailoverMessenger) SendReply(ctx context.Context, reply conversation.OutboundReply) error {
	if f == nil || f.primary == nil {
		return errors.New("messaging: failover primary sender not configured")
	}
	if err := f.primary.SendReply(ctx, reply); err == nil {
		return nil
	} else if f.secondary == nil {
		return err
	} else {
		f.logger.Warn("primary sms send failed; attempting fallback",
			"provider", f.primaryName,
			"fallback", f.secondaryName,
			"error", err,
			"to", reply.To,
		)
		fallbackErr := f.secondary.SendReply(ctx, reply)
		if fallbackErr != nil {
			f.logger.Error("fallback sms send failed",
				"provider", f.secondaryName,
				"error", fallbackErr,
				"to", reply.To,
			)
			return fallbackErr
		}
		return nil
	}
}
