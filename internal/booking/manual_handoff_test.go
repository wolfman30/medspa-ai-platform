package booking

import (
	"context"
	"strings"
	"testing"
	"time"
)

// mockNotificationSender records all SMS and email calls.
type mockNotificationSender struct {
	smsCalls   []smsCall
	emailCalls []emailCall
	smsErr     error
	emailErr   error
}

type smsCall struct {
	To, Body string
}

type emailCall struct {
	To, Subject, HTMLBody string
}

func (m *mockNotificationSender) SendSMS(_ context.Context, to, body string) error {
	m.smsCalls = append(m.smsCalls, smsCall{To: to, Body: body})
	return m.smsErr
}

func (m *mockNotificationSender) SendEmail(_ context.Context, to, subject, htmlBody string) error {
	m.emailCalls = append(m.emailCalls, emailCall{To: to, Subject: subject, HTMLBody: htmlBody})
	return m.emailErr
}

func TestManualHandoffAdapter_Name(t *testing.T) {
	adapter := NewManualHandoffAdapter(nil, ManualHandoffConfig{}, nil)
	if adapter.Name() != "manual" {
		t.Errorf("expected name 'manual', got %q", adapter.Name())
	}
}

func TestManualHandoffAdapter_CheckAvailability(t *testing.T) {
	adapter := NewManualHandoffAdapter(nil, ManualHandoffConfig{}, nil)
	slots, err := adapter.CheckAvailability(context.Background(), LeadSummary{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if slots != nil {
		t.Errorf("expected nil slots, got %v", slots)
	}
}

func TestManualHandoffAdapter_CreateBooking_SMSAndEmail(t *testing.T) {
	sender := &mockNotificationSender{}
	cfg := ManualHandoffConfig{
		HandoffNotificationPhone: "+15551234567",
		HandoffNotificationEmail: "owner@clinic.com",
	}
	adapter := NewManualHandoffAdapter(sender, cfg, nil)

	lead := LeadSummary{
		OrgID:            "org-123",
		LeadID:           "lead-456",
		ConversationID:   "conv-789",
		ClinicName:       "Forever Young MedSpa",
		PatientName:      "Jane Doe",
		PatientPhone:     "+15559876543",
		PatientEmail:     "jane@example.com",
		ServiceRequested: "Botox",
		PatientType:      "new",
		PreferredDays:    "weekdays",
		PreferredTimes:   "morning",
		CollectedAt:      time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC),
	}

	result, err := adapter.CreateBooking(context.Background(), lead)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should not mark as booked
	if result.Booked {
		t.Error("expected Booked=false for manual handoff")
	}

	// Should have a handoff message
	if !strings.Contains(result.HandoffMessage, "Forever Young MedSpa") {
		t.Errorf("handoff message should contain clinic name, got: %q", result.HandoffMessage)
	}

	// SMS should have been sent
	if len(sender.smsCalls) != 1 {
		t.Fatalf("expected 1 SMS call, got %d", len(sender.smsCalls))
	}
	if sender.smsCalls[0].To != "+15551234567" {
		t.Errorf("SMS sent to wrong number: %s", sender.smsCalls[0].To)
	}
	if !strings.Contains(sender.smsCalls[0].Body, "Jane Doe") {
		t.Error("SMS body should contain patient name")
	}
	if !strings.Contains(sender.smsCalls[0].Body, "Botox") {
		t.Error("SMS body should contain service")
	}

	// Email should have been sent
	if len(sender.emailCalls) != 1 {
		t.Fatalf("expected 1 email call, got %d", len(sender.emailCalls))
	}
	if sender.emailCalls[0].To != "owner@clinic.com" {
		t.Errorf("email sent to wrong address: %s", sender.emailCalls[0].To)
	}
	if !strings.Contains(sender.emailCalls[0].Subject, "Jane Doe") {
		t.Error("email subject should contain patient name")
	}
}

func TestManualHandoffAdapter_CreateBooking_NoChannels(t *testing.T) {
	sender := &mockNotificationSender{}
	cfg := ManualHandoffConfig{} // No phone or email
	adapter := NewManualHandoffAdapter(sender, cfg, nil)

	lead := LeadSummary{
		ClinicName:  "Test Clinic",
		CollectedAt: time.Now(),
	}

	result, err := adapter.CreateBooking(context.Background(), lead)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Booked {
		t.Error("expected Booked=false")
	}
	if len(sender.smsCalls) != 0 || len(sender.emailCalls) != 0 {
		t.Error("no notifications should be sent when no channels configured")
	}
}

func TestManualHandoffAdapter_GetHandoffMessage(t *testing.T) {
	adapter := NewManualHandoffAdapter(nil, ManualHandoffConfig{}, nil)

	msg := adapter.GetHandoffMessage("Forever 22")
	if !strings.Contains(msg, "Forever 22") {
		t.Errorf("expected clinic name in message, got: %q", msg)
	}

	msg = adapter.GetHandoffMessage("")
	if !strings.Contains(msg, "the clinic") {
		t.Errorf("expected 'the clinic' fallback, got: %q", msg)
	}
}

func TestFormatLeadSummary(t *testing.T) {
	lead := LeadSummary{
		PatientName:       "Jane Doe",
		PatientPhone:      "+15559876543",
		PatientEmail:      "jane@example.com",
		ServiceRequested:  "Lip Filler",
		PatientType:       "returning",
		PreferredDays:     "weekends",
		PreferredTimes:    "afternoon",
		ConversationNotes: "Wants 1 syringe, had filler 6 months ago",
		CollectedAt:       time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC),
	}

	summary := FormatLeadSummary(lead)

	for _, expected := range []string{
		"Jane Doe",
		"+15559876543",
		"jane@example.com",
		"Lip Filler",
		"returning",
		"weekends",
		"afternoon",
		"1 syringe",
	} {
		if !strings.Contains(summary, expected) {
			t.Errorf("summary missing %q:\n%s", expected, summary)
		}
	}
}

func TestFormatLeadSummaryHTML(t *testing.T) {
	lead := LeadSummary{
		PatientName:      "Jane Doe",
		PatientPhone:     "+15559876543",
		ServiceRequested: "Botox",
		PatientType:      "new",
		CollectedAt:      time.Now(),
	}

	html := FormatLeadSummaryHTML(lead)
	if !strings.Contains(html, "Jane Doe") {
		t.Error("HTML should contain patient name")
	}
	if !strings.Contains(html, "<table") {
		t.Error("HTML should contain a table")
	}
}

func TestFormatLeadSummary_NAFallbacks(t *testing.T) {
	lead := LeadSummary{
		CollectedAt: time.Now(),
	}
	summary := FormatLeadSummary(lead)
	if !strings.Contains(summary, "N/A") {
		t.Error("empty fields should show N/A")
	}
}
