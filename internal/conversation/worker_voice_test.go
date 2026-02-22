package conversation

import (
	"context"
	"testing"
)

type mockVoiceCaller struct {
	calls []OutboundCallRequest
	resp  *OutboundCallResponse
	err   error
}

func (m *mockVoiceCaller) InitiateCallback(_ context.Context, req OutboundCallRequest) (*OutboundCallResponse, error) {
	m.calls = append(m.calls, req)
	return m.resp, m.err
}

func TestHandleCallbackRequest_NotSMS(t *testing.T) {
	w := &Worker{
		voiceCaller: &mockVoiceCaller{},
	}
	msg := MessageRequest{
		Channel: ChannelVoice, // not SMS
		Message: "call me back",
	}
	if w.handleCallbackRequest(context.Background(), msg) {
		t.Error("should not handle callback for voice channel")
	}
}

func TestHandleCallbackRequest_NotCallbackMessage(t *testing.T) {
	w := &Worker{
		voiceCaller: &mockVoiceCaller{},
	}
	msg := MessageRequest{
		Channel: ChannelSMS,
		Message: "I want to book Botox",
	}
	if w.handleCallbackRequest(context.Background(), msg) {
		t.Error("should not trigger callback for non-callback message")
	}
}

func TestHandleCallbackRequest_NilVoiceCaller(t *testing.T) {
	w := &Worker{}
	msg := MessageRequest{
		Channel: ChannelSMS,
		Message: "call me back",
	}
	if w.handleCallbackRequest(context.Background(), msg) {
		t.Error("should not handle callback without voice caller")
	}
}

func TestVoiceConvID(t *testing.T) {
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
		got := voiceConvID(tt.orgID, tt.phone)
		if got != tt.want {
			t.Errorf("voiceConvID(%q, %q) = %q, want %q", tt.orgID, tt.phone, got, tt.want)
		}
	}
}
