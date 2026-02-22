package conversation

import (
	"encoding/json"
	"testing"
	"time"
)

func TestVoiceCallState_JSON(t *testing.T) {
	state := &VoiceCallState{
		CallID:         "call_123",
		OrgID:          "org_456",
		CallerPhone:    "+15551234567",
		ClinicPhone:    "+15559876543",
		ConversationID: "voice:org_456:15551234567",
		Status:         VoiceCallStatusActive,
		TurnCount:      3,
		StartedAt:      time.Date(2026, 2, 22, 15, 0, 0, 0, time.UTC),
		LastActivityAt: time.Date(2026, 2, 22, 15, 2, 0, 0, time.UTC),
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded VoiceCallState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.CallID != state.CallID {
		t.Errorf("CallID: got %q, want %q", decoded.CallID, state.CallID)
	}
	if decoded.Status != VoiceCallStatusActive {
		t.Errorf("Status: got %q, want %q", decoded.Status, VoiceCallStatusActive)
	}
	if decoded.TurnCount != 3 {
		t.Errorf("TurnCount: got %d, want 3", decoded.TurnCount)
	}
}

func TestVoiceCallTranscriptEntry_JSON(t *testing.T) {
	entry := VoiceCallTranscriptEntry{
		Role:      "user",
		Text:      "I'd like to book Botox",
		Timestamp: time.Date(2026, 2, 22, 15, 0, 30, 0, time.UTC),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded VoiceCallTranscriptEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Role != "user" {
		t.Errorf("Role: got %q, want %q", decoded.Role, "user")
	}
	if decoded.Text != entry.Text {
		t.Errorf("Text: got %q, want %q", decoded.Text, entry.Text)
	}
}

func TestVoiceCallKey(t *testing.T) {
	key := voiceCallKey("call_abc")
	want := "voice:call:call_abc"
	if key != want {
		t.Errorf("got %q, want %q", key, want)
	}
}

func TestVoiceTranscriptKey(t *testing.T) {
	key := voiceTranscriptKey("call_abc")
	want := "voice:transcript:call_abc"
	if key != want {
		t.Errorf("got %q, want %q", key, want)
	}
}

func TestVoiceCallStatusConstants(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"ringing", VoiceCallStatusRinging, "ringing"},
		{"active", VoiceCallStatusActive, "active"},
		{"ended", VoiceCallStatusEnded, "ended"},
		{"sms_handoff", VoiceCallStatusSMSHandoff, "sms_handoff"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}
