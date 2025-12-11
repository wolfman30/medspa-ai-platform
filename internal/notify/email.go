package notify

import (
	"context"
	"fmt"

	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// EmailSender defines the interface for sending emails.
// Implementations can be swapped (SendGrid, SES, SMTP) without changing callers.
type EmailSender interface {
	Send(ctx context.Context, msg EmailMessage) error
}

// EmailMessage represents an email to be sent.
type EmailMessage struct {
	To      string
	ToName  string
	Subject string
	Body    string // Plain text body
	HTML    string // Optional HTML body
}

// SendGridSender sends emails via SendGrid API.
type SendGridSender struct {
	client    *sendgrid.Client
	fromEmail string
	fromName  string
	logger    *logging.Logger
}

// SendGridConfig holds configuration for SendGrid.
type SendGridConfig struct {
	APIKey    string
	FromEmail string
	FromName  string
}

// NewSendGridSender creates a new SendGrid email sender.
func NewSendGridSender(cfg SendGridConfig, logger *logging.Logger) *SendGridSender {
	if cfg.APIKey == "" {
		return nil
	}
	if logger == nil {
		logger = logging.Default()
	}
	if cfg.FromName == "" {
		cfg.FromName = "MedSpa AI"
	}
	return &SendGridSender{
		client:    sendgrid.NewSendClient(cfg.APIKey),
		fromEmail: cfg.FromEmail,
		fromName:  cfg.FromName,
		logger:    logger,
	}
}

// Send sends an email via SendGrid.
func (s *SendGridSender) Send(ctx context.Context, msg EmailMessage) error {
	if s.client == nil {
		return fmt.Errorf("notify: sendgrid client not configured")
	}

	from := mail.NewEmail(s.fromName, s.fromEmail)
	to := mail.NewEmail(msg.ToName, msg.To)

	var message *mail.SGMailV3
	if msg.HTML != "" {
		message = mail.NewSingleEmail(from, msg.Subject, to, msg.Body, msg.HTML)
	} else {
		message = mail.NewSingleEmail(from, msg.Subject, to, msg.Body, msg.Body)
	}

	response, err := s.client.SendWithContext(ctx, message)
	if err != nil {
		s.logger.Error("sendgrid send failed", "error", err, "to", msg.To)
		return fmt.Errorf("notify: sendgrid send failed: %w", err)
	}

	if response.StatusCode >= 400 {
		s.logger.Error("sendgrid returned error status", "status", response.StatusCode, "body", response.Body, "to", msg.To)
		return fmt.Errorf("notify: sendgrid returned status %d", response.StatusCode)
	}

	s.logger.Info("email sent via sendgrid", "to", msg.To, "subject", msg.Subject, "status", response.StatusCode)
	return nil
}

// StubEmailSender is a no-op sender for testing or when email is disabled.
type StubEmailSender struct {
	logger *logging.Logger
}

// NewStubEmailSender creates a stub email sender that logs but doesn't send.
func NewStubEmailSender(logger *logging.Logger) *StubEmailSender {
	if logger == nil {
		logger = logging.Default()
	}
	return &StubEmailSender{logger: logger}
}

// Send logs the email but doesn't actually send it.
func (s *StubEmailSender) Send(ctx context.Context, msg EmailMessage) error {
	s.logger.Info("stub email sender: would send email", "to", msg.To, "subject", msg.Subject)
	return nil
}
