package booking

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// NotificationSender abstracts the channel used to notify the clinic about a
// qualified lead (SMS or email). The manual handoff adapter calls whichever
// channels are configured for the clinic.
type NotificationSender interface {
	// SendSMS sends an SMS to the given phone number.
	SendSMS(ctx context.Context, to, body string) error
	// SendEmail sends an email with the given subject and body.
	SendEmail(ctx context.Context, to, subject, htmlBody string) error
}

// ManualHandoffConfig holds the clinic-specific notification targets.
type ManualHandoffConfig struct {
	HandoffNotificationPhone string
	HandoffNotificationEmail string
}

// ManualHandoffAdapter implements BookingAdapter for clinics that don't have
// an automated EMR integration. It generates a qualified lead summary and
// notifies the clinic owner via SMS and/or email so they can manually book
// the patient.
type ManualHandoffAdapter struct {
	sender NotificationSender
	config ManualHandoffConfig
	logger *logging.Logger
}

// NewManualHandoffAdapter creates a new manual handoff adapter.
func NewManualHandoffAdapter(sender NotificationSender, cfg ManualHandoffConfig, logger *logging.Logger) *ManualHandoffAdapter {
	if logger == nil {
		logger = logging.Default()
	}
	return &ManualHandoffAdapter{
		sender: sender,
		config: cfg,
		logger: logger,
	}
}

// Name returns "manual".
func (a *ManualHandoffAdapter) Name() string { return "manual" }

// CheckAvailability is a no-op for manual handoff — the clinic manages their
// own schedule.
func (a *ManualHandoffAdapter) CheckAvailability(_ context.Context, _ LeadSummary) ([]AvailabilitySlot, error) {
	return nil, nil
}

// CreateBooking generates a qualified lead summary and sends it to the clinic
// via the configured notification channels. It returns a HandoffMessage for the
// patient confirming that the clinic will reach out.
func (a *ManualHandoffAdapter) CreateBooking(ctx context.Context, lead LeadSummary) (*BookingResult, error) {
	summary := FormatLeadSummary(lead)

	var errs []string

	// Send SMS notification to clinic
	if a.config.HandoffNotificationPhone != "" && a.sender != nil {
		smsBody := fmt.Sprintf("📋 New Qualified Lead for %s\n\n%s", lead.ClinicName, summary)
		if err := a.sender.SendSMS(ctx, a.config.HandoffNotificationPhone, smsBody); err != nil {
			a.logger.Error("manual handoff: failed to send SMS notification",
				"error", err,
				"org_id", lead.OrgID,
				"lead_id", lead.LeadID,
				"to", a.config.HandoffNotificationPhone,
			)
			errs = append(errs, fmt.Sprintf("sms: %v", err))
		} else {
			a.logger.Info("manual handoff: SMS notification sent",
				"org_id", lead.OrgID,
				"lead_id", lead.LeadID,
				"to", a.config.HandoffNotificationPhone,
			)
		}
	}

	// Send email notification to clinic
	if a.config.HandoffNotificationEmail != "" && a.sender != nil {
		subject := fmt.Sprintf("New Qualified Lead — %s (%s)", lead.PatientName, lead.ServiceRequested)
		htmlBody := FormatLeadSummaryHTML(lead)
		if err := a.sender.SendEmail(ctx, a.config.HandoffNotificationEmail, subject, htmlBody); err != nil {
			a.logger.Error("manual handoff: failed to send email notification",
				"error", err,
				"org_id", lead.OrgID,
				"lead_id", lead.LeadID,
				"to", a.config.HandoffNotificationEmail,
			)
			errs = append(errs, fmt.Sprintf("email: %v", err))
		} else {
			a.logger.Info("manual handoff: email notification sent",
				"org_id", lead.OrgID,
				"lead_id", lead.LeadID,
				"to", a.config.HandoffNotificationEmail,
			)
		}
	}

	// If no notification channels were configured, log a warning
	if a.config.HandoffNotificationPhone == "" && a.config.HandoffNotificationEmail == "" {
		a.logger.Warn("manual handoff: no notification channels configured",
			"org_id", lead.OrgID,
			"lead_id", lead.LeadID,
		)
	}

	result := &BookingResult{
		Booked:         false,
		HandoffMessage: a.GetHandoffMessage(lead.ClinicName),
	}

	if len(errs) > 0 {
		return result, fmt.Errorf("manual handoff notification errors: %s", strings.Join(errs, "; "))
	}
	return result, nil
}

// GetHandoffMessage returns the patient-facing confirmation message.
func (a *ManualHandoffAdapter) GetHandoffMessage(clinicName string) string {
	if clinicName == "" {
		clinicName = "the clinic"
	}
	return fmt.Sprintf(
		"Thank you! I've shared your information with %s and they'll reach out to confirm your appointment shortly.",
		clinicName,
	)
}

// FormatLeadSummary generates a plain-text qualified lead summary.
func FormatLeadSummary(lead LeadSummary) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("Patient: %s\n", valueOrNA(lead.PatientName)))
	b.WriteString(fmt.Sprintf("Phone: %s\n", valueOrNA(lead.PatientPhone)))
	if lead.PatientEmail != "" {
		b.WriteString(fmt.Sprintf("Email: %s\n", lead.PatientEmail))
	}
	b.WriteString(fmt.Sprintf("Service Requested: %s\n", valueOrNA(lead.ServiceRequested)))
	b.WriteString(fmt.Sprintf("Patient Type: %s\n", valueOrNA(lead.PatientType)))

	schedule := buildScheduleString(lead)
	if schedule != "" {
		b.WriteString(fmt.Sprintf("Schedule Preference: %s\n", schedule))
	}

	if lead.ConversationNotes != "" {
		b.WriteString(fmt.Sprintf("Notes: %s\n", lead.ConversationNotes))
	}

	b.WriteString(fmt.Sprintf("Collected: %s\n", lead.CollectedAt.Format(time.RFC1123)))

	return b.String()
}

// FormatLeadSummaryHTML generates an HTML-formatted qualified lead summary for email.
func FormatLeadSummaryHTML(lead LeadSummary) string {
	schedule := buildScheduleString(lead)

	var notesRow string
	if lead.ConversationNotes != "" {
		notesRow = fmt.Sprintf(`<tr><td style="padding:6px 12px;font-weight:bold;">Notes</td><td style="padding:6px 12px;">%s</td></tr>`, html.EscapeString(lead.ConversationNotes))
	}
	var emailRow string
	if lead.PatientEmail != "" {
		emailRow = fmt.Sprintf(`<tr><td style="padding:6px 12px;font-weight:bold;">Email</td><td style="padding:6px 12px;">%s</td></tr>`, html.EscapeString(lead.PatientEmail))
	}
	var scheduleRow string
	if schedule != "" {
		scheduleRow = fmt.Sprintf(`<tr><td style="padding:6px 12px;font-weight:bold;">Schedule Preference</td><td style="padding:6px 12px;">%s</td></tr>`, html.EscapeString(schedule))
	}

	return fmt.Sprintf(`<div style="font-family:sans-serif;max-width:600px;">
<h2 style="color:#333;">New Qualified Lead</h2>
<table style="border-collapse:collapse;width:100%%;">
<tr><td style="padding:6px 12px;font-weight:bold;">Patient</td><td style="padding:6px 12px;">%s</td></tr>
<tr><td style="padding:6px 12px;font-weight:bold;">Phone</td><td style="padding:6px 12px;"><a href="tel:%s">%s</a></td></tr>
%s
<tr><td style="padding:6px 12px;font-weight:bold;">Service</td><td style="padding:6px 12px;">%s</td></tr>
<tr><td style="padding:6px 12px;font-weight:bold;">Patient Type</td><td style="padding:6px 12px;">%s</td></tr>
%s
%s
<tr><td style="padding:6px 12px;font-weight:bold;">Collected</td><td style="padding:6px 12px;">%s</td></tr>
</table>
<p style="color:#666;font-size:12px;">This lead was qualified by AI assistant. Please reach out to confirm the appointment.</p>
</div>`,
		html.EscapeString(valueOrNA(lead.PatientName)),
		html.EscapeString(lead.PatientPhone), html.EscapeString(valueOrNA(lead.PatientPhone)),
		emailRow,
		html.EscapeString(valueOrNA(lead.ServiceRequested)),
		html.EscapeString(valueOrNA(lead.PatientType)),
		scheduleRow,
		notesRow,
		lead.CollectedAt.Format(time.RFC1123),
	)
}

func buildScheduleString(lead LeadSummary) string {
	if lead.SchedulePreference != "" {
		return lead.SchedulePreference
	}
	var parts []string
	if lead.PreferredDays != "" {
		parts = append(parts, lead.PreferredDays)
	}
	if lead.PreferredTimes != "" {
		parts = append(parts, lead.PreferredTimes)
	}
	return strings.Join(parts, ", ")
}

func valueOrNA(s string) string {
	if strings.TrimSpace(s) == "" {
		return "N/A"
	}
	return s
}
