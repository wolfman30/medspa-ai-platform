package conversation

import (
	"testing"

	"github.com/wolfman30/medspa-ai-platform/internal/leads"
)

func TestExtractPreferences_DayAbbreviations(t *testing.T) {
	tests := []struct {
		name     string
		messages []ChatMessage
		wantDays string
	}{
		{
			"mon tues wednesday",
			[]ChatMessage{
				{Role: ChatRoleAssistant, Content: "What days and times work best?"},
				{Role: ChatRoleUser, Content: "Mon tues Wednesday after 1p"},
			},
			"monday, tuesday, wednesday",
		},
		{
			"thu fri",
			[]ChatMessage{
				{Role: ChatRoleAssistant, Content: "What days work?"},
				{Role: ChatRoleUser, Content: "thu or fri"},
			},
			"thursday, friday",
		},
		{
			"full names still work",
			[]ChatMessage{
				{Role: ChatRoleUser, Content: "monday and friday"},
			},
			"monday, friday",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefs, _ := extractPreferences(tt.messages, nil)
			if prefs.PreferredDays != tt.wantDays {
				t.Errorf("PreferredDays = %q, want %q", prefs.PreferredDays, tt.wantDays)
			}
		})
	}
}

var _ leads.SchedulingPreferences // keep import used
