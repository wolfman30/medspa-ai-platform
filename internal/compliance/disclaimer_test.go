package compliance

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultDisclaimerConfig(t *testing.T) {
	cfg := DefaultDisclaimerConfig()
	assert.Equal(t, DisclaimerMedium, cfg.Level)
	assert.True(t, cfg.Enabled)
	assert.False(t, cfg.FirstMessageOnly)
}

func TestDisclaimerService_GetDisclaimerText(t *testing.T) {
	tests := []struct {
		name   string
		config DisclaimerConfig
		want   string
	}{
		{"short", DisclaimerConfig{Level: DisclaimerShort}, disclaimerShortText},
		{"medium", DisclaimerConfig{Level: DisclaimerMedium}, disclaimerMediumText},
		{"full", DisclaimerConfig{Level: DisclaimerFull}, disclaimerFullText},
		{"default", DisclaimerConfig{Level: "unknown"}, disclaimerMediumText},
		{"custom", DisclaimerConfig{CustomText: "Custom disclaimer."}, "Custom disclaimer."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewDisclaimerService(nil, tt.config)
			assert.Equal(t, tt.want, s.GetDisclaimerText())
		})
	}
}

func TestDisclaimerService_AddDisclaimer(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		config      DisclaimerConfig
		message     string
		opts        DisclaimerOptions
		wantContain string
		wantExact   string
	}{
		{
			name:      "disabled",
			config:    DisclaimerConfig{Enabled: false},
			message:   "Hello",
			wantExact: "Hello",
		},
		{
			name:        "enabled adds disclaimer",
			config:      DisclaimerConfig{Enabled: true, Level: DisclaimerShort},
			message:     "Hello there",
			wantContain: disclaimerShortText,
		},
		{
			name:      "first message only - not first",
			config:    DisclaimerConfig{Enabled: true, FirstMessageOnly: true},
			message:   "Hello",
			opts:      DisclaimerOptions{IsFirstMessage: false},
			wantExact: "Hello",
		},
		{
			name:        "first message only - is first",
			config:      DisclaimerConfig{Enabled: true, FirstMessageOnly: true, Level: DisclaimerShort},
			message:     "Hello",
			opts:        DisclaimerOptions{IsFirstMessage: true},
			wantContain: disclaimerShortText,
		},
		{
			name:      "already contains disclaimer",
			config:    DisclaimerConfig{Enabled: true, Level: DisclaimerShort},
			message:   "Hello\n\n" + disclaimerShortText,
			wantExact: "Hello\n\n" + disclaimerShortText,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewDisclaimerService(nil, tt.config)
			got, err := s.AddDisclaimer(ctx, tt.message, tt.opts)
			require.NoError(t, err)
			if tt.wantExact != "" {
				assert.Equal(t, tt.wantExact, got)
			}
			if tt.wantContain != "" {
				assert.Contains(t, got, tt.wantContain)
			}
		})
	}
}

func TestDisclaimerService_AddDisclaimer_WithAudit(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectExec("INSERT INTO compliance_audit_events").
		WillReturnResult(sqlmock.NewResult(1, 1))

	audit := NewAuditService(db)
	s := NewDisclaimerService(audit, DisclaimerConfig{Enabled: true, Level: DisclaimerShort})

	ctx := context.Background()
	opts := DisclaimerOptions{OrgID: "org-1", ConversationID: "conv-1", LeadID: "lead-1", IsFirstMessage: true}
	got, err := s.AddDisclaimer(ctx, "Hello", opts)
	require.NoError(t, err)
	assert.Contains(t, got, disclaimerShortText)
}

func TestDisclaimerService_MustAddDisclaimer(t *testing.T) {
	s := NewDisclaimerService(nil, DisclaimerConfig{Enabled: true, Level: DisclaimerShort})
	got := s.MustAddDisclaimer(context.Background(), "Hello", DisclaimerOptions{})
	assert.Contains(t, got, disclaimerShortText)
}

func TestDisclaimerService_ShouldAddDisclaimer(t *testing.T) {
	tests := []struct {
		name           string
		config         DisclaimerConfig
		isFirstMessage bool
		want           bool
	}{
		{"disabled", DisclaimerConfig{Enabled: false}, true, false},
		{"enabled always", DisclaimerConfig{Enabled: true}, false, true},
		{"first only - is first", DisclaimerConfig{Enabled: true, FirstMessageOnly: true}, true, true},
		{"first only - not first", DisclaimerConfig{Enabled: true, FirstMessageOnly: true}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewDisclaimerService(nil, tt.config)
			assert.Equal(t, tt.want, s.ShouldAddDisclaimer(tt.isFirstMessage))
		})
	}
}
