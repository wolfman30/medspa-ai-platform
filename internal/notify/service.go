package notify

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// SMSSender sends SMS messages to operators.
type SMSSender interface {
	SendSMS(ctx context.Context, to, body string) error
}

// ClinicConfigStore retrieves clinic configuration.
type ClinicConfigStore interface {
	Get(ctx context.Context, orgID string) (*clinic.Config, error)
}

// Service handles sending notifications to clinic operators.
type Service struct {
	email       EmailSender
	sms         SMSSender
	clinicStore ClinicConfigStore
	leadsRepo   leads.Repository
	logger      *logging.Logger
}

// NewService creates a notification service.
func NewService(email EmailSender, sms SMSSender, clinicStore ClinicConfigStore, leadsRepo leads.Repository, logger *logging.Logger) *Service {
	if logger == nil {
		logger = logging.Default()
	}
	return &Service{
		email:       email,
		sms:         sms,
		clinicStore: clinicStore,
		leadsRepo:   leadsRepo,
		logger:      logger,
	}
}

// NotifyPaymentSuccess sends notifications when a patient pays their deposit.
func (s *Service) NotifyPaymentSuccess(ctx context.Context, evt events.PaymentSucceededV1) error {
	if s.clinicStore == nil {
		s.logger.Debug("notify: clinic store not configured, skipping notifications")
		return nil
	}

	// Get clinic config to check notification preferences
	cfg, err := s.clinicStore.Get(ctx, evt.OrgID)
	if err != nil {
		s.logger.Error("notify: failed to get clinic config", "error", err, "org_id", evt.OrgID)
		return fmt.Errorf("notify: get clinic config: %w", err)
	}

	if !cfg.Notifications.NotifyOnPayment {
		s.logger.Debug("notify: payment notifications disabled for clinic", "org_id", evt.OrgID)
		return nil
	}

	// Try to get lead details for richer notifications
	var leadName, leadPhone, serviceInterest, patientType, pastServices, preferredDays, preferredTimes, schedulingNotes string
	if s.leadsRepo != nil && evt.LeadID != "" {
		lead, err := s.leadsRepo.GetByID(ctx, evt.OrgID, evt.LeadID)
		if err == nil && lead != nil {
			leadName = lead.Name
			leadPhone = lead.Phone
			serviceInterest = lead.ServiceInterest
			patientType = lead.PatientType
			pastServices = lead.PastServices
			preferredDays = lead.PreferredDays
			preferredTimes = lead.PreferredTimes
			schedulingNotes = lead.SchedulingNotes
		}
	}
	if leadPhone == "" {
		leadPhone = evt.LeadPhone
	}
	if leadName == "" {
		leadName = "A patient"
	}

	// Format amount
	amountStr := fmt.Sprintf("$%.2f", float64(evt.AmountCents)/100)

	// Build scheduled time info if available
	scheduledInfo := ""
	if evt.ScheduledFor != nil {
		scheduledInfo = fmt.Sprintf("\nScheduled for: %s", evt.ScheduledFor.Format("Monday, January 2 at 3:04 PM"))
	}

	// Format transaction time
	transactionTime := evt.OccurredAt.Format("January 2, 2006 at 3:04 PM")

	// Build service and scheduling preference info
	serviceInfo := ""
	if serviceInterest != "" {
		serviceInfo = fmt.Sprintf("\nService Interest: %s", serviceInterest)
	}
	patientTypeInfo := ""
	if patientType != "" {
		patientTypeInfo = fmt.Sprintf("\nPatient Type: %s", patientType)
	}
	pastServicesInfo := ""
	if pastServices != "" {
		pastServicesInfo = fmt.Sprintf("\nPrevious Services: %s", pastServices)
	}
	preferencesInfo := ""
	if preferredDays != "" || preferredTimes != "" {
		parts := []string{}
		if preferredDays != "" {
			parts = append(parts, preferredDays)
		}
		if preferredTimes != "" {
			parts = append(parts, preferredTimes)
		}
		preferencesInfo = fmt.Sprintf("\nTime Preferences: %s", strings.Join(parts, ", "))
	}
	notesInfo := ""
	if schedulingNotes != "" {
		notesInfo = fmt.Sprintf("\nNotes: %s", schedulingNotes)
	}

	var errs []error

	// Send email notifications
	if cfg.Notifications.EmailEnabled && s.email != nil && len(cfg.Notifications.EmailRecipients) > 0 {
		subject := fmt.Sprintf("💰 Deposit Received - %s", leadName)
		body := fmt.Sprintf(`%s has paid their %s deposit!

Patient: %s
Phone: %s
Amount: %s
Paid: %s%s%s%s%s%s%s
Payment ID: %s

This patient is now a priority lead. Please follow up to confirm their appointment.

— %s AI`, leadName, amountStr, leadName, leadPhone, amountStr, transactionTime, patientTypeInfo, pastServicesInfo, serviceInfo, preferencesInfo, scheduledInfo, notesInfo, evt.ProviderRef, cfg.Name)

		html := fmt.Sprintf(`<div style="font-family: sans-serif; max-width: 600px;">
<h2 style="color: #10b981;">💰 Deposit Received!</h2>
<p><strong>%s</strong> has paid their <strong>%s</strong> deposit.</p>
<table style="border-collapse: collapse; margin: 20px 0;">
  <tr><td style="padding: 8px; border-bottom: 1px solid #e5e7eb;"><strong>Patient:</strong></td><td style="padding: 8px; border-bottom: 1px solid #e5e7eb;">%s</td></tr>
  <tr><td style="padding: 8px; border-bottom: 1px solid #e5e7eb;"><strong>Phone:</strong></td><td style="padding: 8px; border-bottom: 1px solid #e5e7eb;"><a href="tel:%s">%s</a></td></tr>
  %s%s<tr><td style="padding: 8px; border-bottom: 1px solid #e5e7eb;"><strong>Amount:</strong></td><td style="padding: 8px; border-bottom: 1px solid #e5e7eb;">%s</td></tr>
  <tr><td style="padding: 8px; border-bottom: 1px solid #e5e7eb;"><strong>Paid:</strong></td><td style="padding: 8px; border-bottom: 1px solid #e5e7eb;">%s</td></tr>
  %s%s%s
</table>
<p style="background: #f0fdf4; padding: 12px; border-radius: 8px; border-left: 4px solid #10b981;">
  ⭐ <strong>Priority Lead</strong> — Please follow up to confirm their appointment.
</p>
<p style="color: #6b7280; font-size: 12px; margin-top: 20px;">— %s AI</p>
</div>`,
			leadName, amountStr, leadName, leadPhone, leadPhone,
			s.formatPatientTypeHTML(patientType), s.formatPastServicesHTML(pastServices),
			amountStr, transactionTime,
			s.formatServiceHTML(serviceInterest), s.formatPreferencesHTML(preferredDays, preferredTimes), s.formatScheduledHTML(evt.ScheduledFor), cfg.Name)

		for _, recipient := range cfg.Notifications.EmailRecipients {
			msg := EmailMessage{
				To:      recipient,
				Subject: subject,
				Body:    body,
				HTML:    html,
			}
			if err := s.email.Send(ctx, msg); err != nil {
				s.logger.Error("notify: failed to send email", "error", err, "to", recipient)
				errs = append(errs, err)
			} else {
				s.logger.Info("notify: payment email sent", "to", recipient, "lead_id", evt.LeadID)
			}
		}
	}

	// Send SMS to operators (supports multiple recipients)
	smsRecipients := cfg.Notifications.GetSMSRecipients()
	smsTransactionTime := evt.OccurredAt.Format("1/2 3:04PM")
	if cfg.Notifications.SMSEnabled && s.sms != nil && len(smsRecipients) > 0 {
		smsBody := fmt.Sprintf("💰 %s paid %s deposit at %s. Phone: %s%s%s%s. Please call to confirm appointment.",
			leadName, amountStr, smsTransactionTime, leadPhone, s.formatServiceSMS(serviceInterest), s.formatPreferencesSMS(preferredDays, preferredTimes), s.formatScheduledSMS(evt.ScheduledFor))

		for _, recipient := range smsRecipients {
			if err := s.sms.SendSMS(ctx, recipient, smsBody); err != nil {
				s.logger.Error("notify: failed to send operator SMS", "error", err, "to", recipient)
				errs = append(errs, err)
			} else {
				s.logger.Info("notify: payment SMS sent to operator", "to", recipient, "lead_id", evt.LeadID)
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("notify: %d notification(s) failed", len(errs))
	}
	return nil
}

func (s *Service) formatScheduledHTML(t *time.Time) string {
	if t == nil {
		return ""
	}
	return fmt.Sprintf(`<tr><td style="padding: 8px; border-bottom: 1px solid #e5e7eb;"><strong>Scheduled:</strong></td><td style="padding: 8px; border-bottom: 1px solid #e5e7eb;">%s</td></tr>`,
		t.Format("Monday, January 2 at 3:04 PM"))
}

func (s *Service) formatServiceHTML(service string) string {
	if service == "" {
		return ""
	}
	return fmt.Sprintf(`<tr><td style="padding: 8px; border-bottom: 1px solid #e5e7eb;"><strong>Service Interest:</strong></td><td style="padding: 8px; border-bottom: 1px solid #e5e7eb;">%s</td></tr>`, service)
}

func (s *Service) formatPreferencesHTML(days, times string) string {
	if days == "" && times == "" {
		return ""
	}
	parts := []string{}
	if days != "" {
		parts = append(parts, days)
	}
	if times != "" {
		parts = append(parts, times)
	}
	return fmt.Sprintf(`<tr><td style="padding: 8px; border-bottom: 1px solid #e5e7eb;"><strong>Time Preferences:</strong></td><td style="padding: 8px; border-bottom: 1px solid #e5e7eb;">%s</td></tr>`, strings.Join(parts, ", "))
}

func (s *Service) formatPatientTypeHTML(patientType string) string {
	if patientType == "" {
		return ""
	}
	label := "New Patient"
	if patientType == "existing" {
		label = "Existing Patient"
	}
	return fmt.Sprintf(`<tr><td style="padding: 8px; border-bottom: 1px solid #e5e7eb;"><strong>Patient Type:</strong></td><td style="padding: 8px; border-bottom: 1px solid #e5e7eb;">%s</td></tr>`, label)
}

func (s *Service) formatPastServicesHTML(pastServices string) string {
	if pastServices == "" {
		return ""
	}
	return fmt.Sprintf(`<tr><td style="padding: 8px; border-bottom: 1px solid #e5e7eb;"><strong>Previous Services:</strong></td><td style="padding: 8px; border-bottom: 1px solid #e5e7eb;">%s</td></tr>`, pastServices)
}

func (s *Service) formatScheduledSMS(t *time.Time) string {
	if t == nil {
		return ""
	}
	return fmt.Sprintf(" for %s", t.Format("Mon 1/2 3:04PM"))
}

func (s *Service) formatServiceSMS(service string) string {
	if service == "" {
		return ""
	}
	return fmt.Sprintf(". Service: %s", service)
}

func (s *Service) formatPreferencesSMS(days, times string) string {
	if days == "" && times == "" {
		return ""
	}
	parts := []string{}
	if days != "" {
		parts = append(parts, days)
	}
	if times != "" {
		parts = append(parts, times)
	}
	return fmt.Sprintf(". Prefers: %s", strings.Join(parts, ", "))
}

// NotifyNewLead sends notifications when a new lead is created.
func (s *Service) NotifyNewLead(ctx context.Context, orgID string, lead *leads.Lead) error {
	if s.clinicStore == nil {
		return nil
	}

	cfg, err := s.clinicStore.Get(ctx, orgID)
	if err != nil {
		return fmt.Errorf("notify: get clinic config: %w", err)
	}

	if !cfg.Notifications.NotifyOnNewLead {
		return nil
	}

	var errs []error

	// Send email notifications
	if cfg.Notifications.EmailEnabled && s.email != nil && len(cfg.Notifications.EmailRecipients) > 0 {
		subject := fmt.Sprintf("🆕 New Lead - %s", lead.Name)
		body := fmt.Sprintf(`A new lead has come in!

Name: %s
Phone: %s
Email: %s
Source: %s
Message: %s

— %s AI`, lead.Name, lead.Phone, lead.Email, lead.Source, lead.Message, cfg.Name)

		for _, recipient := range cfg.Notifications.EmailRecipients {
			msg := EmailMessage{
				To:      recipient,
				Subject: subject,
				Body:    body,
			}
			if err := s.email.Send(ctx, msg); err != nil {
				errs = append(errs, err)
			}
		}
	}

	// Send SMS to operators (supports multiple recipients)
	smsRecipients := cfg.Notifications.GetSMSRecipients()
	if cfg.Notifications.SMSEnabled && s.sms != nil && len(smsRecipients) > 0 {
		smsBody := fmt.Sprintf("🆕 New lead: %s (%s). Source: %s", lead.Name, lead.Phone, lead.Source)
		for _, recipient := range smsRecipients {
			if err := s.sms.SendSMS(ctx, recipient, smsBody); err != nil {
				errs = append(errs, err)
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("notify: %d notification(s) failed", len(errs))
	}
	return nil
}

// SimpleSMSSender provides a simple SMS sending implementation.
type SimpleSMSSender struct {
	sendFunc func(ctx context.Context, to, from, body string) error
	from     string
	logger   *logging.Logger
}

// NewSimpleSMSSender creates an SMS sender with a custom send function.
func NewSimpleSMSSender(from string, sendFunc func(ctx context.Context, to, from, body string) error, logger *logging.Logger) *SimpleSMSSender {
	if logger == nil {
		logger = logging.Default()
	}
	return &SimpleSMSSender{
		sendFunc: sendFunc,
		from:     from,
		logger:   logger,
	}
}

// SendSMS sends an SMS message.
func (s *SimpleSMSSender) SendSMS(ctx context.Context, to, body string) error {
	if s.sendFunc == nil {
		s.logger.Warn("notify: SMS sender not configured")
		return nil
	}
	return s.sendFunc(ctx, to, s.from, body)
}

// StubSMSSender is a no-op sender for testing.
type StubSMSSender struct {
	logger *logging.Logger
}

// NewStubSMSSender creates a stub SMS sender.
func NewStubSMSSender(logger *logging.Logger) *StubSMSSender {
	if logger == nil {
		logger = logging.Default()
	}
	return &StubSMSSender{logger: logger}
}

// SendSMS logs but doesn't send.
func (s *StubSMSSender) SendSMS(ctx context.Context, to, body string) error {
	s.logger.Info("stub SMS sender: would send", "to", to, "body_preview", truncate(body, 50))
	return nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// Ensure interface compliance
var _ SMSSender = (*SimpleSMSSender)(nil)
var _ SMSSender = (*StubSMSSender)(nil)
