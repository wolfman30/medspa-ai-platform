package compliance

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditService_LogPromptInjection(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectExec("INSERT INTO compliance_audit_events").
		WillReturnResult(sqlmock.NewResult(1, 1))

	service := NewAuditService(db)
	err = service.LogPromptInjection(context.Background(), "org-1", "conv-1", "lead-1", []string{"system_prompt_leak"})
	assert.NoError(t, err)
}

func TestAuditService_LogDisclaimerSent(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectExec("INSERT INTO compliance_audit_events").
		WillReturnResult(sqlmock.NewResult(1, 1))

	service := NewAuditService(db)
	err = service.LogDisclaimerSent(context.Background(), "org-1", "conv-1", "lead-1", "short", "disclaimer text")
	assert.NoError(t, err)
}

func TestAuditService_LogSupervisorReview(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectExec("INSERT INTO compliance_audit_events").
		WillReturnResult(sqlmock.NewResult(1, 1))

	service := NewAuditService(db)
	err = service.LogSupervisorReview(context.Background(), "org-1", "conv-1", "lead-1", "block", "original response", "modified response", "unsafe content")
	assert.NoError(t, err)
}
