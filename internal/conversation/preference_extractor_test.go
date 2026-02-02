package conversation

import (
	"reflect"
	"testing"
)

func TestExtractTimePreferences(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantDays   []int
		wantAfter  string
		wantBefore string
	}{
		{
			name:      "Mondays or Thursdays after 4pm",
			input:     "Mondays or Thursdays after 4pm",
			wantDays:  []int{1, 4},
			wantAfter: "16:00",
		},
		{
			name:       "Weekdays before noon",
			input:      "Weekdays before noon",
			wantDays:   []int{1, 2, 3, 4, 5},
			wantBefore: "12:00",
		},
		{
			name:      "Tuesdays and Fridays in the afternoon",
			input:     "Tuesdays and Fridays in the afternoon",
			wantDays:  []int{2, 5},
			wantAfter: "12:00",
		},
		{
			name:      "After 5pm any day",
			input:     "After 5pm any day",
			wantDays:  []int{0, 1, 2, 3, 4, 5, 6},
			wantAfter: "17:00",
		},
		{
			name:      "Weekends after 10am",
			input:     "Weekends after 10am",
			wantDays:  []int{0, 6},
			wantAfter: "10:00",
		},
		{
			name:       "Monday through Wednesday mornings",
			input:      "Monday through Wednesday mornings",
			wantDays:   []int{1, 3},
			wantBefore: "12:00",
		},
		{
			name:      "Thursday evenings",
			input:     "Thursday evenings",
			wantDays:  []int{4},
			wantAfter: "17:00",
		},
		{
			name:      "After work on Fridays",
			input:     "After work on Fridays",
			wantDays:  []int{5},
			wantAfter: "17:00",
		},
		{
			name:     "Anytime",
			input:    "Anytime",
			wantDays: []int{0, 1, 2, 3, 4, 5, 6},
		},
		{
			name:       "Early morning Monday",
			input:      "Early morning Monday",
			wantDays:   []int{1},
			wantBefore: "12:00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractTimePreferences(tt.input)

			if !reflect.DeepEqual(got.DaysOfWeek, tt.wantDays) {
				t.Errorf("DaysOfWeek = %v, want %v", got.DaysOfWeek, tt.wantDays)
			}

			if got.AfterTime != tt.wantAfter {
				t.Errorf("AfterTime = %q, want %q", got.AfterTime, tt.wantAfter)
			}

			if got.BeforeTime != tt.wantBefore {
				t.Errorf("BeforeTime = %q, want %q", got.BeforeTime, tt.wantBefore)
			}
		})
	}
}

func TestFormatPreferencesForLLM(t *testing.T) {
	tests := []struct {
		name  string
		prefs TimePreferences
		want  string
	}{
		{
			name: "Mondays/Thursdays after 4pm",
			prefs: TimePreferences{
				DaysOfWeek: []int{1, 4},
				AfterTime:  "16:00",
			},
			want: "Monday, Thursday after 04:00pm",
		},
		{
			name: "Weekdays before noon",
			prefs: TimePreferences{
				DaysOfWeek: []int{1, 2, 3, 4, 5},
				BeforeTime: "12:00",
			},
			want: "Monday, Tuesday, Wednesday, Thursday, Friday before 12:00pm",
		},
		{
			name: "Raw text fallback",
			prefs: TimePreferences{
				RawText: "sometime next week",
			},
			want: "sometime next week",
		},
		{
			name:  "Empty preferences",
			prefs: TimePreferences{},
			want:  "any day/time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatPreferencesForLLM(tt.prefs)
			if got != tt.want {
				t.Errorf("FormatPreferencesForLLM() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractEmail(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple email",
			input: "My email is john@example.com",
			want:  "john@example.com",
		},
		{
			name:  "email with subdomain",
			input: "Contact me at sarah.jones@mail.company.com please",
			want:  "sarah.jones@mail.company.com",
		},
		{
			name:  "email with plus sign",
			input: "Use test+tag@gmail.com",
			want:  "test+tag@gmail.com",
		},
		{
			name:  "email with numbers",
			input: "john123@example123.com",
			want:  "john123@example123.com",
		},
		{
			name:  "email uppercase converted to lowercase",
			input: "JOHN@EXAMPLE.COM",
			want:  "john@example.com",
		},
		{
			name:  "mixed case email",
			input: "John.Smith@Example.COM",
			want:  "john.smith@example.com",
		},
		{
			name:  "no email",
			input: "I don't have an email to share",
			want:  "",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "invalid email - no at sign",
			input: "johnexample.com",
			want:  "",
		},
		{
			name:  "invalid email - no domain",
			input: "john@",
			want:  "",
		},
		{
			name:  "email in sentence",
			input: "Sure, you can reach me at patient@clinic.org anytime",
			want:  "patient@clinic.org",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractEmail(tt.input)
			if got != tt.want {
				t.Errorf("ExtractEmail(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractEmailFromHistory(t *testing.T) {
	tests := []struct {
		name    string
		history []ChatMessage
		want    string
	}{
		{
			name: "email in first user message",
			history: []ChatMessage{
				{Role: ChatRoleUser, Content: "Hi, my email is sarah@example.com"},
				{Role: ChatRoleAssistant, Content: "Thanks Sarah!"},
			},
			want: "sarah@example.com",
		},
		{
			name: "email in later user message",
			history: []ChatMessage{
				{Role: ChatRoleUser, Content: "I want to book botox"},
				{Role: ChatRoleAssistant, Content: "What's your email?"},
				{Role: ChatRoleUser, Content: "It's john@gmail.com"},
			},
			want: "john@gmail.com",
		},
		{
			name: "no email in history",
			history: []ChatMessage{
				{Role: ChatRoleUser, Content: "I want to book an appointment"},
				{Role: ChatRoleAssistant, Content: "Sure, what's your name?"},
				{Role: ChatRoleUser, Content: "Sarah"},
			},
			want: "",
		},
		{
			name: "email in assistant message ignored",
			history: []ChatMessage{
				{Role: ChatRoleAssistant, Content: "Please email us at clinic@example.com"},
				{Role: ChatRoleUser, Content: "Ok thanks"},
			},
			want: "",
		},
		{
			name:    "empty history",
			history: []ChatMessage{},
			want:    "",
		},
		{
			name:    "nil history",
			history: nil,
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractEmailFromHistory(tt.history)
			if got != tt.want {
				t.Errorf("ExtractEmailFromHistory() = %q, want %q", got, tt.want)
			}
		})
	}
}
