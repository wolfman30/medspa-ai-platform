package conversation

import (
	"testing"
)

func TestDetectPatientType(t *testing.T) {
	tests := []struct {
		name     string
		messages string
		history  []ChatMessage
		want     string
	}{
		{"new patient explicit", "i'm a new patient ", nil, "new"},
		{"first time", "this is my first time visiting ", nil, "new"},
		{"returning via regex", "i'm a returning patient ", nil, "existing"},
		{"been before", "i've been there before ", nil, "existing"},
		{"new here", "i'm new here ", nil, "new"},
		{"never been", "i've never been ", nil, "new"},
		{"empty", "", nil, ""},
		{"short reply new", "", []ChatMessage{
			{Role: ChatRoleAssistant, Content: "Are you a new or existing patient?"},
			{Role: ChatRoleUser, Content: "New"},
		}, "new"},
		{"short reply existing", "", []ChatMessage{
			{Role: ChatRoleAssistant, Content: "Have you been here before?"},
			{Role: ChatRoleUser, Content: "yes i have"},
		}, "existing"},
		{"short reply no context", "", []ChatMessage{
			{Role: ChatRoleAssistant, Content: "What service are you interested in?"},
			{Role: ChatRoleUser, Content: "New"},
		}, ""},
		{"comma-separated new", "botox, new, thursdays ", nil, "new"},
		{"comma-separated returning", "filler, returning, mornings ", nil, "existing"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectPatientType(tt.messages, tt.history)
			if got != tt.want {
				t.Errorf("detectPatientType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMatchService(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		aliases map[string]string
		want    string
	}{
		{"botox from universal", "i want botox", nil, "Botox"},
		{"config alias", "i want tox", map[string]string{"tox": "Botox Treatment"}, "Botox Treatment"},
		{"config takes priority", "i want botox", map[string]string{"botox": "Neuromodulator"}, "Neuromodulator"},
		{"fallback when no alias match", "i want hydrafacial", map[string]string{"tox": "Botox"}, "HydraFacial"},
		{"lip filler", "interested in lip filler", nil, "lip filler"},
		{"consultation", "just a consultation", nil, "consultation"},
		{"no match", "hello there", nil, ""},
		{"longest match wins from config", "lip filler please", map[string]string{
			"lip filler": "Lip Augmentation",
			"lip":        "Lip Treatment",
			"filler":     "Dermal Filler",
		}, "Lip Augmentation"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchService(tt.text, tt.aliases)
			if got != tt.want {
				t.Errorf("matchService() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractPreferencesName(t *testing.T) {
	history := []ChatMessage{
		{Role: ChatRoleAssistant, Content: "What's your name?"},
		{Role: ChatRoleUser, Content: "My name is Jane Doe"},
	}
	prefs, ok := extractPreferences(history, nil)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if prefs.Name != "Jane Doe" {
		t.Errorf("Name = %q, want %q", prefs.Name, "Jane Doe")
	}
}

func TestExtractPreferencesSchedule(t *testing.T) {
	history := []ChatMessage{
		{Role: ChatRoleUser, Content: "I'd like botox on monday afternoon"},
	}
	prefs, ok := extractPreferences(history, nil)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if prefs.PreferredDays != "monday" {
		t.Errorf("PreferredDays = %q, want %q", prefs.PreferredDays, "monday")
	}
	if prefs.PreferredTimes != "afternoon" {
		t.Errorf("PreferredTimes = %q, want %q", prefs.PreferredTimes, "afternoon")
	}
}

func TestScheduleFromShortReply(t *testing.T) {
	tests := []struct {
		name    string
		history []ChatMessage
		want    string
	}{
		{"flexible reply", []ChatMessage{
			{Role: ChatRoleAssistant, Content: "What days and times work best for you?"},
			{Role: ChatRoleUser, Content: "whenever works"},
		}, "flexible"},
		{"no schedule question", []ChatMessage{
			{Role: ChatRoleAssistant, Content: "What service are you interested in?"},
			{Role: ChatRoleUser, Content: "whenever works"},
		}, ""},
		{"non-flexible reply", []ChatMessage{
			{Role: ChatRoleAssistant, Content: "What days and times work best for you?"},
			{Role: ChatRoleUser, Content: "Monday at 3pm"},
		}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scheduleFromShortReply(tt.history)
			if got != tt.want {
				t.Errorf("scheduleFromShortReply() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizePatientTypeReply(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"New", "new"},
		{"new patient", "new"},
		{"first time", "new"},
		{"Returning", "existing"},
		{"existing patient", "existing"},
		{"been before", "existing"},
		{"botox", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizePatientTypeReply(tt.input)
			if got != tt.want {
				t.Errorf("normalizePatientTypeReply(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
