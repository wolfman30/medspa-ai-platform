package notify

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// SESSender sends emails via AWS SES.
type SESSender struct {
	client    *sesv2.Client
	fromEmail string
	fromName  string
	logger    *logging.Logger
}

// SESConfig holds configuration for AWS SES.
type SESConfig struct {
	FromEmail string
	FromName  string
}

// NewSESSender creates a new AWS SES email sender.
func NewSESSender(client *sesv2.Client, cfg SESConfig, logger *logging.Logger) *SESSender {
	if client == nil {
		return nil
	}
	if logger == nil {
		logger = logging.Default()
	}
	if cfg.FromName == "" {
		cfg.FromName = "MedSpa AI"
	}
	return &SESSender{
		client:    client,
		fromEmail: cfg.FromEmail,
		fromName:  cfg.FromName,
		logger:    logger,
	}
}

// Send sends an email via AWS SES.
func (s *SESSender) Send(ctx context.Context, msg EmailMessage) error {
	if s.client == nil {
		return fmt.Errorf("notify: SES client not configured")
	}

	fromAddress := fmt.Sprintf("%s <%s>", s.fromName, s.fromEmail)

	input := &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(fromAddress),
		Destination: &types.Destination{
			ToAddresses: []string{msg.To},
		},
		Content: &types.EmailContent{
			Simple: &types.Message{
				Subject: &types.Content{
					Data:    aws.String(msg.Subject),
					Charset: aws.String("UTF-8"),
				},
				Body: &types.Body{},
			},
		},
	}

	// Add text body
	if msg.Body != "" {
		input.Content.Simple.Body.Text = &types.Content{
			Data:    aws.String(msg.Body),
			Charset: aws.String("UTF-8"),
		}
	}

	// Add HTML body if provided
	if msg.HTML != "" {
		input.Content.Simple.Body.Html = &types.Content{
			Data:    aws.String(msg.HTML),
			Charset: aws.String("UTF-8"),
		}
	}

	output, err := s.client.SendEmail(ctx, input)
	if err != nil {
		s.logger.Error("SES send failed", "error", err, "to", msg.To)
		return fmt.Errorf("notify: SES send failed: %w", err)
	}

	s.logger.Info("email sent via SES", "to", msg.To, "subject", msg.Subject, "message_id", aws.ToString(output.MessageId))
	return nil
}

// Ensure interface compliance
var _ EmailSender = (*SESSender)(nil)
