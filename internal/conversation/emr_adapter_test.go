package conversation

import (
	"context"
	"testing"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/emr"
)

// mockEMRClient is a test double for EMRClient
type mockEMRClient struct {
	slots    []emr.Slot
	patients []emr.Patient
	appt     *emr.Appointment
	err      error
}

func (m *mockEMRClient) GetAvailability(ctx context.Context, req emr.AvailabilityRequest) ([]emr.Slot, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.slots, nil
}

func (m *mockEMRClient) CreatePatient(ctx context.Context, patient emr.Patient) (*emr.Patient, error) {
	if m.err != nil {
		return nil, m.err
	}
	patient.ID = "patient-new-123"
	return &patient, nil
}

func (m *mockEMRClient) SearchPatients(ctx context.Context, query emr.PatientSearchQuery) ([]emr.Patient, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.patients, nil
}

func (m *mockEMRClient) CreateAppointment(ctx context.Context, req emr.AppointmentRequest) (*emr.Appointment, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.appt != nil {
		return m.appt, nil
	}
	return &emr.Appointment{
		ID:        "appt-123",
		PatientID: req.PatientID,
		Status:    "booked",
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}, nil
}

func TestEMRAdapter_GetUpcomingAvailability(t *testing.T) {
	now := time.Now()
	slots := []emr.Slot{
		{
			ID:           "slot-1",
			ProviderName: "Dr. Smith",
			StartTime:    now.Add(24 * time.Hour),
			EndTime:      now.Add(25 * time.Hour),
			Status:       "free",
			ServiceType:  "Consultation",
		},
		{
			ID:           "slot-2",
			ProviderName: "Dr. Jones",
			StartTime:    now.Add(48 * time.Hour),
			EndTime:      now.Add(49 * time.Hour),
			Status:       "free",
			ServiceType:  "Botox",
		},
		{
			ID:           "slot-3",
			ProviderName: "Dr. Smith",
			StartTime:    now.Add(72 * time.Hour),
			EndTime:      now.Add(73 * time.Hour),
			Status:       "busy", // Should be filtered out
		},
	}

	mock := &mockEMRClient{slots: slots}
	adapter := NewEMRAdapter(mock, "clinic-123")

	ctx := context.Background()
	result, err := adapter.GetUpcomingAvailability(ctx, 7, "")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 free slots, got %d", len(result))
	}
	if result[0].ProviderName != "Dr. Smith" {
		t.Errorf("expected first slot provider Dr. Smith, got %s", result[0].ProviderName)
	}
}

func TestEMRAdapter_IsConfigured(t *testing.T) {
	tests := []struct {
		name     string
		adapter  *EMRAdapter
		expected bool
	}{
		{
			name:     "nil adapter",
			adapter:  nil,
			expected: false,
		},
		{
			name:     "nil client",
			adapter:  &EMRAdapter{client: nil},
			expected: false,
		},
		{
			name:     "configured",
			adapter:  NewEMRAdapter(&mockEMRClient{}, "clinic-1"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.adapter.IsConfigured()
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestEMRAdapter_FindOrCreatePatient_ExistingByPhone(t *testing.T) {
	existingPatient := emr.Patient{
		ID:        "patient-existing",
		FirstName: "Jane",
		LastName:  "Doe",
		Phone:     "+15551234567",
	}

	mock := &mockEMRClient{patients: []emr.Patient{existingPatient}}
	adapter := NewEMRAdapter(mock, "clinic-123")

	ctx := context.Background()
	patient, err := adapter.FindOrCreatePatient(ctx, "John", "Smith", "+15551234567", "")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patient.ID != "patient-existing" {
		t.Errorf("expected existing patient ID, got %s", patient.ID)
	}
}

func TestEMRAdapter_FindOrCreatePatient_CreateNew(t *testing.T) {
	mock := &mockEMRClient{patients: []emr.Patient{}} // No existing patients
	adapter := NewEMRAdapter(mock, "clinic-123")

	ctx := context.Background()
	patient, err := adapter.FindOrCreatePatient(ctx, "John", "Smith", "+15551234567", "john@example.com")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patient.ID != "patient-new-123" {
		t.Errorf("expected new patient ID, got %s", patient.ID)
	}
}

func TestFormatSlotsForGPT(t *testing.T) {
	now := time.Date(2025, 1, 15, 14, 0, 0, 0, time.UTC)
	slots := []AvailabilitySlot{
		{
			ID:           "slot-1",
			ProviderName: "Dr. Smith",
			StartTime:    now,
			EndTime:      now.Add(time.Hour),
			ServiceType:  "Consultation",
		},
		{
			ID:           "slot-2",
			ProviderName: "Dr. Jones",
			StartTime:    now.Add(24 * time.Hour),
			EndTime:      now.Add(25 * time.Hour),
			ServiceType:  "",
		},
	}

	result := FormatSlotsForGPT(slots, 5)

	if result == "" {
		t.Error("expected non-empty result")
	}
	if !contains(result, "Dr. Smith") {
		t.Error("expected result to contain Dr. Smith")
	}
	if !contains(result, "Consultation") {
		t.Error("expected result to contain Consultation")
	}
}

func TestFormatSlotsForGPT_Empty(t *testing.T) {
	result := FormatSlotsForGPT(nil, 5)
	if !contains(result, "No available appointments") {
		t.Error("expected empty slots message")
	}
}

func TestFormatSlotsForGPT_MaxSlots(t *testing.T) {
	slots := make([]AvailabilitySlot, 10)
	for i := range slots {
		slots[i] = AvailabilitySlot{
			ID:        "slot-" + string(rune('0'+i)),
			StartTime: time.Now().Add(time.Duration(i) * time.Hour),
		}
	}

	result := FormatSlotsForGPT(slots, 3)

	// Count how many numbered items appear (should be max 3)
	count := 0
	for _, line := range splitLines(result) {
		if len(line) > 0 && line[0] >= '1' && line[0] <= '9' {
			count++
		}
	}
	if count > 3 {
		t.Errorf("expected max 3 slots, got %d", count)
	}
}

func TestContainsBookingIntent(t *testing.T) {
	tests := []struct {
		msg      string
		expected bool
	}{
		{"I want to book an appointment", true},
		{"What's available next week?", true},
		{"Can I schedule a consultation?", true},
		{"What time slots are open?", true},
		{"When can I come in?", true},
		{"Hello there!", false},
		{"What are your prices?", false},
		{"Do you accept insurance?", false},
	}

	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			result := containsBookingIntent(tt.msg)
			if result != tt.expected {
				t.Errorf("containsBookingIntent(%q) = %v, want %v", tt.msg, result, tt.expected)
			}
		})
	}
}

// Helper functions
func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr) >= 0
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
