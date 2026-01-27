package notify

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
)

// Mock implementations

type mockEmailSender struct {
	sent    []EmailMessage
	failOn  string // fail if To matches this
	callErr error
}

func (m *mockEmailSender) Send(ctx context.Context, msg EmailMessage) error {
	if m.callErr != nil {
		return m.callErr
	}
	if m.failOn != "" && msg.To == m.failOn {
		return errors.New("mock email error")
	}
	m.sent = append(m.sent, msg)
	return nil
}

type mockSMSSender struct {
	sent    []struct{ to, body string }
	failOn  string
	callErr error
}

func (m *mockSMSSender) SendSMS(ctx context.Context, to, body string) error {
	if m.callErr != nil {
		return m.callErr
	}
	if m.failOn != "" && to == m.failOn {
		return errors.New("mock SMS error")
	}
	m.sent = append(m.sent, struct{ to, body string }{to, body})
	return nil
}

type mockClinicStore struct {
	configs map[string]*clinic.Config
	err     error
}

func (m *mockClinicStore) Get(ctx context.Context, orgID string) (*clinic.Config, error) {
	if m.err != nil {
		return nil, m.err
	}
	if cfg, ok := m.configs[orgID]; ok {
		return cfg, nil
	}
	return clinic.DefaultConfig(orgID), nil
}

type mockLeadsRepo struct {
	leads map[string]*leads.Lead
	err   error
}

func (m *mockLeadsRepo) GetByID(ctx context.Context, orgID, id string) (*leads.Lead, error) {
	if m.err != nil {
		return nil, m.err
	}
	key := orgID + ":" + id
	if lead, ok := m.leads[key]; ok {
		return lead, nil
	}
	return nil, leads.ErrLeadNotFound
}

func (m *mockLeadsRepo) Create(ctx context.Context, req *leads.CreateLeadRequest) (*leads.Lead, error) {
	return nil, nil
}

func (m *mockLeadsRepo) GetOrCreateByPhone(ctx context.Context, orgID, phone, source, defaultName string) (*leads.Lead, error) {
	return nil, nil
}

func (m *mockLeadsRepo) UpdateSchedulingPreferences(ctx context.Context, leadID string, prefs leads.SchedulingPreferences) error {
	return nil
}

func (m *mockLeadsRepo) UpdateDepositStatus(ctx context.Context, leadID, status, priority string) error {
	return nil
}

func (m *mockLeadsRepo) ListByOrg(ctx context.Context, orgID string, filter leads.ListLeadsFilter) ([]*leads.Lead, error) {
	return nil, nil
}

// Tests

func TestService_NotifyPaymentSuccess_NilClinicStore(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil)

	err := svc.NotifyPaymentSuccess(context.Background(), events.PaymentSucceededV1{
		OrgID:  "org-123",
		LeadID: "lead-456",
	})

	if err != nil {
		t.Errorf("expected no error when clinic store is nil, got: %v", err)
	}
}

func TestService_NotifyPaymentSuccess_NotificationsDisabled(t *testing.T) {
	emailSender := &mockEmailSender{}
	smsSender := &mockSMSSender{}
	clinicStore := &mockClinicStore{
		configs: map[string]*clinic.Config{
			"org-123": {
				OrgID: "org-123",
				Name:  "Test Clinic",
				Notifications: clinic.NotificationPrefs{
					NotifyOnPayment: false, // disabled
				},
			},
		},
	}

	svc := NewService(emailSender, smsSender, clinicStore, nil, nil)

	err := svc.NotifyPaymentSuccess(context.Background(), events.PaymentSucceededV1{
		OrgID:       "org-123",
		LeadID:      "lead-456",
		AmountCents: 5000,
	})

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if len(emailSender.sent) > 0 {
		t.Error("expected no emails sent when notifications disabled")
	}
	if len(smsSender.sent) > 0 {
		t.Error("expected no SMS sent when notifications disabled")
	}
}

func TestService_NotifyPaymentSuccess_EmailOnly(t *testing.T) {
	emailSender := &mockEmailSender{}
	smsSender := &mockSMSSender{}
	clinicStore := &mockClinicStore{
		configs: map[string]*clinic.Config{
			"org-123": {
				OrgID: "org-123",
				Name:  "Glow MedSpa",
				Notifications: clinic.NotificationPrefs{
					EmailEnabled:    true,
					EmailRecipients: []string{"owner@clinic.com", "manager@clinic.com"},
					SMSEnabled:      false,
					NotifyOnPayment: true,
				},
			},
		},
	}
	leadsRepo := &mockLeadsRepo{
		leads: map[string]*leads.Lead{
			"org-123:lead-456": {
				ID:    "lead-456",
				Name:  "Jane Smith",
				Phone: "+19378962713",
			},
		},
	}

	svc := NewService(emailSender, smsSender, clinicStore, leadsRepo, nil)

	err := svc.NotifyPaymentSuccess(context.Background(), events.PaymentSucceededV1{
		OrgID:       "org-123",
		LeadID:      "lead-456",
		AmountCents: 5000,
		ProviderRef: "sq_payment_123",
	})

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if len(emailSender.sent) != 2 {
		t.Errorf("expected 2 emails sent, got %d", len(emailSender.sent))
	}
	if len(smsSender.sent) != 0 {
		t.Errorf("expected 0 SMS sent, got %d", len(smsSender.sent))
	}

	// Verify email content
	if len(emailSender.sent) > 0 {
		email := emailSender.sent[0]
		if email.To != "owner@clinic.com" {
			t.Errorf("expected email to owner@clinic.com, got %s", email.To)
		}
		if email.Subject == "" {
			t.Error("expected non-empty subject")
		}
	}
}

func TestService_NotifyPaymentSuccess_SMSOnly(t *testing.T) {
	emailSender := &mockEmailSender{}
	smsSender := &mockSMSSender{}
	clinicStore := &mockClinicStore{
		configs: map[string]*clinic.Config{
			"org-123": {
				OrgID: "org-123",
				Name:  "Glow MedSpa",
				Notifications: clinic.NotificationPrefs{
					EmailEnabled:    false,
					SMSEnabled:      true,
					SMSRecipient:    "+15551234567",
					NotifyOnPayment: true,
				},
			},
		},
	}

	svc := NewService(emailSender, smsSender, clinicStore, nil, nil)

	err := svc.NotifyPaymentSuccess(context.Background(), events.PaymentSucceededV1{
		OrgID:       "org-123",
		LeadID:      "lead-456",
		LeadPhone:   "+19378962713",
		AmountCents: 5000,
	})

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if len(emailSender.sent) != 0 {
		t.Errorf("expected 0 emails sent, got %d", len(emailSender.sent))
	}
	if len(smsSender.sent) != 1 {
		t.Errorf("expected 1 SMS sent, got %d", len(smsSender.sent))
	}

	// Verify SMS content
	if len(smsSender.sent) > 0 {
		sms := smsSender.sent[0]
		if sms.to != "+15551234567" {
			t.Errorf("expected SMS to +15551234567, got %s", sms.to)
		}
	}
}

func TestService_NotifyPaymentSuccess_BothChannels(t *testing.T) {
	emailSender := &mockEmailSender{}
	smsSender := &mockSMSSender{}
	clinicStore := &mockClinicStore{
		configs: map[string]*clinic.Config{
			"org-123": {
				OrgID: "org-123",
				Name:  "Glow MedSpa",
				Notifications: clinic.NotificationPrefs{
					EmailEnabled:    true,
					EmailRecipients: []string{"owner@clinic.com"},
					SMSEnabled:      true,
					SMSRecipient:    "+15551234567",
					NotifyOnPayment: true,
				},
			},
		},
	}

	svc := NewService(emailSender, smsSender, clinicStore, nil, nil)

	err := svc.NotifyPaymentSuccess(context.Background(), events.PaymentSucceededV1{
		OrgID:       "org-123",
		LeadID:      "lead-456",
		LeadPhone:   "+19378962713",
		AmountCents: 5000,
	})

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if len(emailSender.sent) != 1 {
		t.Errorf("expected 1 email sent, got %d", len(emailSender.sent))
	}
	if len(smsSender.sent) != 1 {
		t.Errorf("expected 1 SMS sent, got %d", len(smsSender.sent))
	}
}

func TestService_NotifyPaymentSuccess_WithScheduledTime(t *testing.T) {
	emailSender := &mockEmailSender{}
	smsSender := &mockSMSSender{}
	clinicStore := &mockClinicStore{
		configs: map[string]*clinic.Config{
			"org-123": {
				OrgID: "org-123",
				Name:  "Glow MedSpa",
				Notifications: clinic.NotificationPrefs{
					EmailEnabled:    true,
					EmailRecipients: []string{"owner@clinic.com"},
					SMSEnabled:      true,
					SMSRecipient:    "+15551234567",
					NotifyOnPayment: true,
				},
			},
		},
	}

	scheduledTime := time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC)

	svc := NewService(emailSender, smsSender, clinicStore, nil, nil)

	err := svc.NotifyPaymentSuccess(context.Background(), events.PaymentSucceededV1{
		OrgID:        "org-123",
		LeadID:       "lead-456",
		LeadPhone:    "+19378962713",
		AmountCents:  5000,
		ScheduledFor: &scheduledTime,
	})

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if len(emailSender.sent) != 1 {
		t.Fatalf("expected 1 email sent, got %d", len(emailSender.sent))
	}

	// Check that scheduled time appears in HTML
	if emailSender.sent[0].HTML == "" {
		t.Error("expected HTML content")
	}
}

func TestService_NotifyPaymentSuccess_EmailUsesClinicTimezone(t *testing.T) {
	emailSender := &mockEmailSender{}
	clinicStore := &mockClinicStore{
		configs: map[string]*clinic.Config{
			"org-123": {
				OrgID:    "org-123",
				Name:     "Glow MedSpa",
				Timezone: "America/New_York",
				Notifications: clinic.NotificationPrefs{
					EmailEnabled:    true,
					EmailRecipients: []string{"owner@clinic.com"},
					NotifyOnPayment: true,
				},
			},
		},
	}

	svc := NewService(emailSender, nil, clinicStore, nil, nil)

	occurredAt := time.Date(2025, 1, 15, 15, 0, 0, 0, time.UTC) // 10:00 AM EST
	err := svc.NotifyPaymentSuccess(context.Background(), events.PaymentSucceededV1{
		OrgID:       "org-123",
		LeadID:      "lead-456",
		AmountCents: 5000,
		OccurredAt:  occurredAt,
	})

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if len(emailSender.sent) != 1 {
		t.Fatalf("expected 1 email sent, got %d", len(emailSender.sent))
	}

	body := emailSender.sent[0].Body
	if !strings.Contains(body, "January 15, 2025 at 10:00 AM EST") {
		t.Fatalf("expected email body to use EST timezone, got %q", body)
	}
}

func TestService_NotifyPaymentSuccess_EmailFailure(t *testing.T) {
	emailSender := &mockEmailSender{
		callErr: errors.New("sendgrid down"),
	}
	clinicStore := &mockClinicStore{
		configs: map[string]*clinic.Config{
			"org-123": {
				OrgID: "org-123",
				Name:  "Glow MedSpa",
				Notifications: clinic.NotificationPrefs{
					EmailEnabled:    true,
					EmailRecipients: []string{"owner@clinic.com"},
					NotifyOnPayment: true,
				},
			},
		},
	}

	svc := NewService(emailSender, nil, clinicStore, nil, nil)

	err := svc.NotifyPaymentSuccess(context.Background(), events.PaymentSucceededV1{
		OrgID:       "org-123",
		LeadID:      "lead-456",
		AmountCents: 5000,
	})

	if err == nil {
		t.Error("expected error when email fails")
	}
}

func TestService_NotifyPaymentSuccess_SMSFailure(t *testing.T) {
	smsSender := &mockSMSSender{
		callErr: errors.New("twilio down"),
	}
	clinicStore := &mockClinicStore{
		configs: map[string]*clinic.Config{
			"org-123": {
				OrgID: "org-123",
				Name:  "Glow MedSpa",
				Notifications: clinic.NotificationPrefs{
					SMSEnabled:      true,
					SMSRecipient:    "+15551234567",
					NotifyOnPayment: true,
				},
			},
		},
	}

	svc := NewService(nil, smsSender, clinicStore, nil, nil)

	err := svc.NotifyPaymentSuccess(context.Background(), events.PaymentSucceededV1{
		OrgID:       "org-123",
		LeadID:      "lead-456",
		AmountCents: 5000,
	})

	if err == nil {
		t.Error("expected error when SMS fails")
	}
}

func TestService_NotifyPaymentSuccess_ClinicStoreError(t *testing.T) {
	clinicStore := &mockClinicStore{
		err: errors.New("redis connection failed"),
	}

	svc := NewService(nil, nil, clinicStore, nil, nil)

	err := svc.NotifyPaymentSuccess(context.Background(), events.PaymentSucceededV1{
		OrgID:  "org-123",
		LeadID: "lead-456",
	})

	if err == nil {
		t.Error("expected error when clinic store fails")
	}
}

func TestService_NotifyPaymentSuccess_LeadLookupFallback(t *testing.T) {
	emailSender := &mockEmailSender{}
	clinicStore := &mockClinicStore{
		configs: map[string]*clinic.Config{
			"org-123": {
				OrgID: "org-123",
				Name:  "Glow MedSpa",
				Notifications: clinic.NotificationPrefs{
					EmailEnabled:    true,
					EmailRecipients: []string{"owner@clinic.com"},
					NotifyOnPayment: true,
				},
			},
		},
	}
	// Lead repo returns not found
	leadsRepo := &mockLeadsRepo{
		err: leads.ErrLeadNotFound,
	}

	svc := NewService(emailSender, nil, clinicStore, leadsRepo, nil)

	err := svc.NotifyPaymentSuccess(context.Background(), events.PaymentSucceededV1{
		OrgID:       "org-123",
		LeadID:      "lead-456",
		LeadPhone:   "+19378962713", // Fallback phone from event
		AmountCents: 5000,
	})

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if len(emailSender.sent) != 1 {
		t.Errorf("expected 1 email sent, got %d", len(emailSender.sent))
	}
}

func TestService_NotifyNewLead_Disabled(t *testing.T) {
	emailSender := &mockEmailSender{}
	clinicStore := &mockClinicStore{
		configs: map[string]*clinic.Config{
			"org-123": {
				OrgID: "org-123",
				Notifications: clinic.NotificationPrefs{
					NotifyOnNewLead: false,
				},
			},
		},
	}

	svc := NewService(emailSender, nil, clinicStore, nil, nil)

	err := svc.NotifyNewLead(context.Background(), "org-123", &leads.Lead{
		Name:  "Test Lead",
		Phone: "+15551234567",
	})

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if len(emailSender.sent) > 0 {
		t.Error("expected no emails when new lead notifications disabled")
	}
}

func TestService_NotifyNewLead_Enabled(t *testing.T) {
	emailSender := &mockEmailSender{}
	smsSender := &mockSMSSender{}
	clinicStore := &mockClinicStore{
		configs: map[string]*clinic.Config{
			"org-123": {
				OrgID: "org-123",
				Name:  "Glow MedSpa",
				Notifications: clinic.NotificationPrefs{
					EmailEnabled:    true,
					EmailRecipients: []string{"owner@clinic.com"},
					SMSEnabled:      true,
					SMSRecipient:    "+15559999999",
					NotifyOnNewLead: true,
				},
			},
		},
	}

	svc := NewService(emailSender, smsSender, clinicStore, nil, nil)

	err := svc.NotifyNewLead(context.Background(), "org-123", &leads.Lead{
		Name:    "Jane Doe",
		Phone:   "+15551234567",
		Email:   "jane@example.com",
		Source:  "website",
		Message: "Interested in Botox",
	})

	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if len(emailSender.sent) != 1 {
		t.Errorf("expected 1 email sent, got %d", len(emailSender.sent))
	}
	if len(smsSender.sent) != 1 {
		t.Errorf("expected 1 SMS sent, got %d", len(smsSender.sent))
	}
}

func TestService_NotifyNewLead_NilClinicStore(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil)

	err := svc.NotifyNewLead(context.Background(), "org-123", &leads.Lead{
		Name:  "Test Lead",
		Phone: "+15551234567",
	})

	if err != nil {
		t.Errorf("expected no error when clinic store is nil, got: %v", err)
	}
}

func TestSimpleSMSSender_SendSMS(t *testing.T) {
	var capturedTo, capturedFrom, capturedBody string
	sendFunc := func(ctx context.Context, to, from, body string) error {
		capturedTo = to
		capturedFrom = from
		capturedBody = body
		return nil
	}

	sender := NewSimpleSMSSender("+15551111111", sendFunc, nil)

	err := sender.SendSMS(context.Background(), "+15552222222", "Hello!")

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if capturedTo != "+15552222222" {
		t.Errorf("expected to +15552222222, got %s", capturedTo)
	}
	if capturedFrom != "+15551111111" {
		t.Errorf("expected from +15551111111, got %s", capturedFrom)
	}
	if capturedBody != "Hello!" {
		t.Errorf("expected body 'Hello!', got %s", capturedBody)
	}
}

func TestSimpleSMSSender_NilSendFunc(t *testing.T) {
	sender := NewSimpleSMSSender("+15551111111", nil, nil)

	err := sender.SendSMS(context.Background(), "+15552222222", "Hello!")

	// Should not error, just warn
	if err != nil {
		t.Errorf("expected no error with nil sendFunc, got: %v", err)
	}
}

func TestSimpleSMSSender_Error(t *testing.T) {
	sendFunc := func(ctx context.Context, to, from, body string) error {
		return errors.New("send failed")
	}

	sender := NewSimpleSMSSender("+15551111111", sendFunc, nil)

	err := sender.SendSMS(context.Background(), "+15552222222", "Hello!")

	if err == nil {
		t.Error("expected error from sendFunc")
	}
}

func TestStubSMSSender_SendSMS(t *testing.T) {
	sender := NewStubSMSSender(nil)

	err := sender.SendSMS(context.Background(), "+15552222222", "Hello!")

	if err != nil {
		t.Errorf("stub should not error, got: %v", err)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
		{"ab", 1, "a..."},
	}

	for _, tt := range tests {
		result := truncate(tt.input, tt.maxLen)
		if result != tt.expected {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
		}
	}
}

func TestService_FormatScheduledHTML(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil)

	// Nil time
	result := svc.formatScheduledHTML(nil, time.UTC)
	if result != "" {
		t.Errorf("expected empty string for nil time, got %q", result)
	}

	// Valid time
	tm := time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC)
	result = svc.formatScheduledHTML(&tm, time.UTC)
	if result == "" {
		t.Error("expected non-empty HTML for valid time")
	}
	if !containsSubstr(result, "Scheduled") {
		t.Error("expected 'Scheduled' in HTML output")
	}
}

func TestService_FormatScheduledSMS(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil)

	// Nil time
	result := svc.formatScheduledSMS(nil, time.UTC)
	if result != "" {
		t.Errorf("expected empty string for nil time, got %q", result)
	}

	// Valid time
	tm := time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC)
	result = svc.formatScheduledSMS(&tm, time.UTC)
	if result == "" {
		t.Error("expected non-empty string for valid time")
	}
	if !containsSubstr(result, "for") {
		t.Error("expected 'for' in SMS output")
	}
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
