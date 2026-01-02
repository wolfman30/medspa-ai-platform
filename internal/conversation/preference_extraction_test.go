package conversation

import (
	"context"
	"strings"
	"testing"

	"github.com/wolfman30/medspa-ai-platform/internal/leads"
)

type mockLeadsRepo struct {
	savedPrefs leads.SchedulingPreferences
	savedCount int
}

func (m *mockLeadsRepo) Create(ctx context.Context, req *leads.CreateLeadRequest) (*leads.Lead, error) {
	return nil, nil
}

func (m *mockLeadsRepo) GetByID(ctx context.Context, orgID string, id string) (*leads.Lead, error) {
	return nil, nil
}

func (m *mockLeadsRepo) GetOrCreateByPhone(ctx context.Context, orgID string, phone string, source string, defaultName string) (*leads.Lead, error) {
	return nil, nil
}

func (m *mockLeadsRepo) UpdateSchedulingPreferences(ctx context.Context, leadID string, prefs leads.SchedulingPreferences) error {
	m.savedPrefs = prefs
	m.savedCount++
	return nil
}

func (m *mockLeadsRepo) UpdateDepositStatus(ctx context.Context, leadID string, status string, priority string) error {
	return nil
}

func (m *mockLeadsRepo) ListByOrg(ctx context.Context, orgID string, filter leads.ListLeadsFilter) ([]*leads.Lead, error) {
	return nil, nil
}

func TestExtractAndSavePreferences(t *testing.T) {
	tests := []struct {
		name          string
		conversation  []ChatMessage
		expectService string
		expectPatient string
		expectDays    string
		expectTimes   string
		expectSaved   bool
	}{
		{
			name: "extracts botox and weekday afternoons",
			conversation: []ChatMessage{
				{Role: ChatRoleUser, Content: "I want to book botox"},
				{Role: ChatRoleAssistant, Content: "Great! What days work best?"},
				{Role: ChatRoleUser, Content: "Weekdays in the afternoon"},
			},
			expectService: "botox",
			expectDays:    "weekdays",
			expectTimes:   "afternoon",
			expectSaved:   true,
		},
		{
			name: "extracts filler and weekend mornings",
			conversation: []ChatMessage{
				{Role: ChatRoleUser, Content: "I'd like to schedule an appointment for dermal filler"},
				{Role: ChatRoleAssistant, Content: "Perfect! When works for you?"},
				{Role: ChatRoleUser, Content: "Weekend mornings would be great"},
			},
			expectService: "filler",
			expectDays:    "weekends",
			expectTimes:   "morning",
			expectSaved:   true,
		},
		{
			name: "extracts service from short reply",
			conversation: []ChatMessage{
				{Role: ChatRoleUser, Content: "Botox"},
			},
			expectService: "botox",
			expectSaved:   true,
		},
		{
			name: "extracts patient type from explicit phrase",
			conversation: []ChatMessage{
				{Role: ChatRoleUser, Content: "I'm a new patient"},
			},
			expectPatient: "new",
			expectSaved:   true,
		},
		{
			name: "extracts patient type from short reply",
			conversation: []ChatMessage{
				{Role: ChatRoleAssistant, Content: "Are you a new patient or have you visited us before?"},
				{Role: ChatRoleSystem, Content: "Business hours: weekdays 9-5"},
				{Role: ChatRoleUser, Content: "new"},
			},
			expectPatient: "new",
			expectSaved:   true,
		},
		{
			name: "extracts returning patient from short reply",
			conversation: []ChatMessage{
				{Role: ChatRoleAssistant, Content: "Are you a new or returning patient?"},
				{Role: ChatRoleUser, Content: "returning"},
			},
			expectPatient: "existing",
			expectSaved:   true,
		},
		{
			name: "no preferences mentioned",
			conversation: []ChatMessage{
				{Role: ChatRoleUser, Content: "What are your hours?"},
				{Role: ChatRoleAssistant, Content: "We're open 9-5 weekdays"},
			},
			expectSaved: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockLeadsRepo{}
			svc := &LLMService{
				leadsRepo: mock,
			}

			err := svc.extractAndSavePreferences(context.Background(), "lead-123", tt.conversation)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.expectSaved {
				if mock.savedCount != 1 {
					t.Errorf("expected 1 save, got %d", mock.savedCount)
				}
				if tt.expectService != "" && !strings.Contains(strings.ToLower(mock.savedPrefs.ServiceInterest), strings.ToLower(tt.expectService)) {
					t.Errorf("expected service %q, got %q", tt.expectService, mock.savedPrefs.ServiceInterest)
				}
				if tt.expectPatient != "" && mock.savedPrefs.PatientType != tt.expectPatient {
					t.Errorf("expected patient type %q, got %q", tt.expectPatient, mock.savedPrefs.PatientType)
				}
				if tt.expectDays != "" && mock.savedPrefs.PreferredDays != tt.expectDays {
					t.Errorf("expected days %q, got %q", tt.expectDays, mock.savedPrefs.PreferredDays)
				}
				if tt.expectTimes != "" && mock.savedPrefs.PreferredTimes != tt.expectTimes {
					t.Errorf("expected times %q, got %q", tt.expectTimes, mock.savedPrefs.PreferredTimes)
				}
			} else {
				if mock.savedCount != 0 {
					t.Errorf("expected no saves, got %d", mock.savedCount)
				}
			}
		})
	}
}
