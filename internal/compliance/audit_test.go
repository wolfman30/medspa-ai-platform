package compliance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditService_LogEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	service := NewAuditService(db)

	tests := []struct {
		name    string
		event   AuditEvent
		wantErr bool
	}{
		{
			name: "log medical advice refused",
			event: AuditEvent{
				EventType:      EventMedicalAdviceRefused,
				OrgID:          uuid.New().String(),
				ConversationID: "conv-123",
				UserMessage:    "What medication should I take?",
				AIResponse:     "I cannot provide medical advice.",
			},
			wantErr: false,
		},
		{
			name: "log PHI detected",
			event: AuditEvent{
				EventType:      EventPHIDetected,
				OrgID:          uuid.New().String(),
				ConversationID: "conv-456",
				UserMessage:    "My SSN is 123-45-6789",
				Details:        json.RawMessage(`{"phi_type": "ssn"}`),
			},
			wantErr: false,
		},
		{
			name: "log disclaimer sent",
			event: AuditEvent{
				EventType:      EventDisclaimerSent,
				OrgID:          uuid.New().String(),
				ConversationID: "conv-789",
				Details:        json.RawMessage(`{"level": "medium"}`),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock.ExpectExec("INSERT INTO compliance_audit_events").
				WillReturnResult(sqlmock.NewResult(1, 1))

			err := service.LogEvent(context.Background(), tt.event)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAuditService_LogMedicalAdviceRefused(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	service := NewAuditService(db)

	mock.ExpectExec("INSERT INTO compliance_audit_events").
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = service.LogMedicalAdviceRefused(
		context.Background(),
		"org-123",
		"conv-456",
		"lead-123",
		"What medication?",
		[]string{"medication_question"},
	)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAuditService_LogPHIDetected(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	service := NewAuditService(db)

	mock.ExpectExec("INSERT INTO compliance_audit_events").
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = service.LogPHIDetected(
		context.Background(),
		"org-123",
		"conv-456",
		"lead-123",
		"My SSN is 123-45-6789",
		"ssn",
	)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAuditService_QueryEvents(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	service := NewAuditService(db)

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "event_type", "org_id", "conversation_id", "lead_id",
		"user_message", "ai_response", "details", "created_at",
	}).AddRow(
		uuid.New(), EventMedicalAdviceRefused, "org-123", "conv-456", nil,
		"What medication?", "Cannot provide advice", []byte(`{}`), now,
	)

	mock.ExpectQuery("SELECT (.+) FROM compliance_audit_events").
		WillReturnRows(rows)

	filter := AuditFilter{
		OrgID:     "org-123",
		StartTime: now.Add(-24 * time.Hour),
		EndTime:   now,
		Limit:     100,
	}

	events, err := service.QueryEvents(context.Background(), filter)
	assert.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, EventMedicalAdviceRefused, events[0].EventType)
}

func TestAuditEventType_String(t *testing.T) {
	tests := []struct {
		eventType AuditEventType
		expected  string
	}{
		{EventMedicalAdviceRefused, "compliance.medical_advice_refused"},
		{EventPHIDetected, "compliance.phi_detected"},
		{EventDisclaimerSent, "compliance.disclaimer_sent"},
		{EventSupervisorReview, "compliance.supervisor_review"},
		{EventResponseModified, "compliance.response_modified"},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.eventType))
		})
	}
}
