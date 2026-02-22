package conversation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// VoiceSMSHandoff manages the transition from voice to SMS when payment or
// links need to be sent. Since patients can't click links or enter card
// details during a phone call, we send an SMS with the relevant information.
type VoiceSMSHandoff struct {
	messenger ReplyMessenger
	logger    *logging.Logger
}

// NewVoiceSMSHandoff creates a handoff service.
func NewVoiceSMSHandoff(messenger ReplyMessenger, logger *logging.Logger) *VoiceSMSHandoff {
	if logger == nil {
		logger = logging.Default()
	}
	return &VoiceSMSHandoff{
		messenger: messenger,
		logger:    logger,
	}
}

// SendPaymentLinkSMS sends an SMS with a payment/deposit link during a voice call.
// This is triggered when the voice conversation engine detects a deposit intent.
func (h *VoiceSMSHandoff) SendPaymentLinkSMS(ctx context.Context, req VoiceHandoffRequest) error {
	if h.messenger == nil {
		return fmt.Errorf("voice handoff: messenger not configured")
	}
	if req.PatientPhone == "" || req.ClinicPhone == "" {
		return fmt.Errorf("voice handoff: patient and clinic phone required")
	}

	body := req.Message
	if body == "" {
		body = fmt.Sprintf("Hi! As discussed on the phone, here's your secure deposit link for your upcoming appointment at %s. We'll confirm your exact time once the deposit is received.",
			req.ClinicName)
	}

	reply := OutboundReply{
		OrgID:          req.OrgID,
		LeadID:         req.LeadID,
		ConversationID: req.ConversationID,
		To:             req.PatientPhone,
		From:           req.ClinicPhone,
		Body:           body,
		Metadata: map[string]string{
			"source":   "voice_handoff",
			"voice_id": req.VoiceCallID,
		},
	}

	if err := h.messenger.SendReply(ctx, reply); err != nil {
		h.logger.Error("voice handoff: failed to send SMS",
			"error", err,
			"patient_phone", maskPhone(req.PatientPhone),
			"org_id", req.OrgID,
		)
		return fmt.Errorf("voice handoff: send sms: %w", err)
	}

	h.logger.Info("voice handoff: SMS sent",
		"org_id", req.OrgID,
		"conversation_id", req.ConversationID,
		"voice_call_id", req.VoiceCallID,
	)
	return nil
}

// VoiceHandoffRequest contains the data needed for a voice-to-SMS handoff.
type VoiceHandoffRequest struct {
	OrgID          string
	LeadID         string
	ConversationID string
	VoiceCallID    string
	PatientPhone   string
	ClinicPhone    string
	ClinicName     string
	Message        string // Optional custom message; defaults to a standard handoff message.
}

// maskPhone returns the last 4 digits of a phone number for logging.
func maskPhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if len(phone) <= 4 {
		return "****"
	}
	return "***" + phone[len(phone)-4:]
}

// VoiceCallSummary represents a completed voice call for admin portal display.
type VoiceCallSummary struct {
	CallID         string                     `json:"call_id"`
	OrgID          string                     `json:"org_id"`
	CallerPhone    string                     `json:"caller_phone"`
	ConversationID string                     `json:"conversation_id"`
	LeadID         string                     `json:"lead_id"`
	Status         string                     `json:"status"`
	Outcome        string                     `json:"outcome"`
	DurationSec    int                        `json:"duration_sec"`
	TurnCount      int                        `json:"turn_count"`
	Transcript     []VoiceCallTranscriptEntry `json:"transcript,omitempty"`
	StartedAt      time.Time                  `json:"started_at"`
	EndedAt        time.Time                  `json:"ended_at,omitempty"`
}
