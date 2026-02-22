package conversation

import (
	"strings"
	"testing"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
)

func TestVoiceGreeting_NilConfig(t *testing.T) {
	got := voiceGreeting(nil)
	if !strings.Contains(got, "Thanks for calling") {
		t.Errorf("expected default greeting, got %q", got)
	}
}

func TestVoiceGreeting_WithClinicName(t *testing.T) {
	cfg := &clinic.Config{Name: "Forever 22 Med Spa"}
	got := voiceGreeting(cfg)
	if !strings.Contains(got, "Forever 22 Med Spa") {
		t.Errorf("expected clinic name in greeting, got %q", got)
	}
}

func TestVoiceGreeting_CustomGreeting(t *testing.T) {
	cfg := &clinic.Config{
		Name: "Test Clinic",
		AIPersona: clinic.AIPersona{
			CustomGreeting: "Welcome to our amazing clinic!",
		},
	}
	got := voiceGreeting(cfg)
	if got != "Welcome to our amazing clinic!" {
		t.Errorf("expected custom greeting, got %q", got)
	}
}

func TestBuildVoiceSystemPrompt_ContainsVoiceInstructions(t *testing.T) {
	prompt := buildVoiceSystemPrompt(5000, false)

	checks := []string{
		"VOICE CHANNEL",
		"1-2 SHORT sentences",
		"I'll text you a link",
		"spoken language",
		"NEVER use emoji",
	}
	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("voice prompt missing %q", check)
		}
	}
}

func TestBuildVoiceSystemPrompt_ContainsBasePrompt(t *testing.T) {
	prompt := buildVoiceSystemPrompt(5000, false)
	if !strings.Contains(prompt, "MedSpa AI Concierge") {
		t.Error("voice prompt missing base prompt content")
	}
}

func TestIsVoiceChannel(t *testing.T) {
	if !isVoiceChannel(ChannelVoice) {
		t.Error("expected true for ChannelVoice")
	}
	if isVoiceChannel(ChannelSMS) {
		t.Error("expected false for ChannelSMS")
	}
	if isVoiceChannel(ChannelUnknown) {
		t.Error("expected false for ChannelUnknown")
	}
}

func TestVoiceConversationID(t *testing.T) {
	tests := []struct {
		orgID string
		phone string
		want  string
	}{
		{"org-123", "+15551234567", "voice:org-123:15551234567"},
		{"", "+15551234567", ""},
		{"org-123", "", ""},
	}
	for _, tt := range tests {
		got := voiceConversationID(tt.orgID, tt.phone)
		if got != tt.want {
			t.Errorf("voiceConversationID(%q, %q) = %q, want %q", tt.orgID, tt.phone, got, tt.want)
		}
	}
}
