package conversation

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestMaskPhone(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"+15551234567", "***4567"},
		{"1234", "****"},
		{"", "****"},
		{"+1555", "***1555"},
	}
	for _, tt := range tests {
		got := maskPhone(tt.input)
		if got != tt.want {
			t.Errorf("maskPhone(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestVoiceCallSummary_JSON(t *testing.T) {
	summary := VoiceCallSummary{
		CallID:         "call_123",
		OrgID:          "org_456",
		CallerPhone:    "+15551234567",
		ConversationID: "voice:org_456:15551234567",
		Status:         "ended",
		Outcome:        "booked",
		DurationSec:    142,
		TurnCount:      5,
		Transcript: []VoiceCallTranscriptEntry{
			{Role: "assistant", Text: "Hi! Thanks for calling.", Timestamp: time.Now()},
			{Role: "user", Text: "I'd like to book Botox.", Timestamp: time.Now()},
		},
		StartedAt: time.Now().Add(-3 * time.Minute),
		EndedAt:   time.Now(),
	}

	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded VoiceCallSummary
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Outcome != "booked" {
		t.Errorf("Outcome: got %q, want %q", decoded.Outcome, "booked")
	}
	if decoded.DurationSec != 142 {
		t.Errorf("DurationSec: got %d, want 142", decoded.DurationSec)
	}
	if len(decoded.Transcript) != 2 {
		t.Errorf("Transcript: got %d entries, want 2", len(decoded.Transcript))
	}
}

func TestVoiceSMSHandoff_NilMessenger(t *testing.T) {
	handoff := NewVoiceSMSHandoff(nil, nil)
	err := handoff.SendPaymentLinkSMS(context.Background(), VoiceHandoffRequest{
		PatientPhone: "+15551234567",
		ClinicPhone:  "+15559876543",
	})
	if err == nil {
		t.Error("expected error for nil messenger")
	}
}

func TestVoiceSMSHandoff_MissingPhones(t *testing.T) {
	handoff := NewVoiceSMSHandoff(&mockReplyMessenger{}, nil)

	tests := []struct {
		name string
		req  VoiceHandoffRequest
	}{
		{"missing patient phone", VoiceHandoffRequest{ClinicPhone: "+15559876543"}},
		{"missing clinic phone", VoiceHandoffRequest{PatientPhone: "+15551234567"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handoff.SendPaymentLinkSMS(context.Background(), tt.req)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}

// mockReplyMessenger records sent replies.
type mockReplyMessenger struct {
	replies []OutboundReply
	err     error
}

func (m *mockReplyMessenger) SendReply(_ context.Context, reply OutboundReply) error {
	m.replies = append(m.replies, reply)
	return m.err
}

func TestVoiceSMSHandoff_Success(t *testing.T) {
	mock := &mockReplyMessenger{}
	handoff := NewVoiceSMSHandoff(mock, nil)

	req := VoiceHandoffRequest{
		OrgID:          "org_123",
		LeadID:         "lead_456",
		ConversationID: "voice:org_123:15551234567",
		VoiceCallID:    "call_789",
		PatientPhone:   "+15551234567",
		ClinicPhone:    "+15559876543",
		ClinicName:     "Forever 22 Med Spa",
	}

	err := handoff.SendPaymentLinkSMS(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(mock.replies))
	}

	reply := mock.replies[0]
	if reply.To != req.PatientPhone {
		t.Errorf("To: got %q, want %q", reply.To, req.PatientPhone)
	}
	if reply.From != req.ClinicPhone {
		t.Errorf("From: got %q, want %q", reply.From, req.ClinicPhone)
	}
	if reply.OrgID != req.OrgID {
		t.Errorf("OrgID: got %q, want %q", reply.OrgID, req.OrgID)
	}
	if reply.Metadata["source"] != "voice_handoff" {
		t.Errorf("source metadata: got %q, want %q", reply.Metadata["source"], "voice_handoff")
	}
}
