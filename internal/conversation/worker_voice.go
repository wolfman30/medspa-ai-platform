package conversation

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// VoiceCallInitiator can start outbound AI voice calls.
type VoiceCallInitiator interface {
	InitiateCallback(ctx context.Context, req OutboundCallRequest) (*OutboundCallResponse, error)
}

// handleCallbackRequest detects callback requests in SMS conversations and
// initiates an outbound voice call. Returns true if a callback was triggered.
func (w *Worker) handleCallbackRequest(ctx context.Context, msg MessageRequest) bool {
	if w == nil || w.voiceCaller == nil {
		return false
	}
	if msg.Channel != ChannelSMS {
		return false
	}
	if !IsCallbackRequest(msg.Message) {
		return false
	}

	// Check if the clinic has voice AI enabled
	cfg := w.clinicConfig(ctx, msg.OrgID)
	if cfg == nil || !cfg.VoiceAIEnabled {
		return false
	}

	// Determine the AI assistant ID (per-clinic or global)
	assistantID := cfg.TelnyxAssistantID
	if assistantID == "" {
		w.logger.Warn("voice callback: no assistant ID configured",
			"org_id", msg.OrgID)
		return false
	}

	w.logger.Info("voice callback: initiating outbound call",
		"org_id", msg.OrgID,
		"conversation_id", msg.ConversationID,
		"to", maskPhone(msg.From),
	)

	// Send an SMS acknowledgment first
	if w.messenger != nil {
		ackReply := OutboundReply{
			OrgID:          msg.OrgID,
			LeadID:         msg.LeadID,
			ConversationID: msg.ConversationID,
			To:             msg.From,
			From:           msg.To,
			Body:           "Great, I'll call you right back! You should get a call in just a moment.",
		}
		sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := w.messenger.SendReply(sendCtx, ackReply); err != nil {
			w.logger.Error("voice callback: failed to send ack SMS", "error", err)
			// Continue with the call anyway
		}

		w.appendTranscript(ctx, msg.ConversationID, SMSTranscriptMessage{
			Role:      "assistant",
			From:      msg.To,
			To:        msg.From,
			Body:      ackReply.Body,
			Timestamp: time.Now(),
			Kind:      "voice_callback_ack",
		})
	}

	// Initiate the outbound call
	callReq := OutboundCallRequest{
		From:          msg.To,   // clinic number
		To:            msg.From, // patient number
		AIAssistantID: assistantID,
	}

	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resp, err := w.voiceCaller.InitiateCallback(callCtx, callReq)
	if err != nil {
		w.logger.Error("voice callback: failed to initiate call",
			"error", err,
			"org_id", msg.OrgID,
			"to", maskPhone(msg.From),
		)
		// Send failure SMS
		if w.messenger != nil {
			failReply := OutboundReply{
				OrgID:          msg.OrgID,
				LeadID:         msg.LeadID,
				ConversationID: msg.ConversationID,
				To:             msg.From,
				From:           msg.To,
				Body:           "I'm sorry, I wasn't able to call you right now. Let's continue by text — how can I help?",
			}
			failCtx, failCancel := context.WithTimeout(ctx, 5*time.Second)
			defer failCancel()
			_ = w.messenger.SendReply(failCtx, failReply)

			w.appendTranscript(ctx, msg.ConversationID, SMSTranscriptMessage{
				Role:      "assistant",
				From:      msg.To,
				To:        msg.From,
				Body:      failReply.Body,
				Timestamp: time.Now(),
				Kind:      "voice_callback_failed",
			})
		}
		return true // We handled it (even though it failed)
	}

	w.logger.Info("voice callback: call initiated",
		"org_id", msg.OrgID,
		"call_control_id", resp.CallControlID,
		"to", maskPhone(msg.From),
	)

	// Log the callback event
	w.appendTranscript(ctx, msg.ConversationID, SMSTranscriptMessage{
		Role:      "system",
		From:      msg.To,
		To:        msg.From,
		Body:      fmt.Sprintf("Voice callback initiated (call_id: %s)", resp.CallControlID),
		Timestamp: time.Now(),
		Kind:      "voice_callback_initiated",
	})

	return true
}

// voiceConvID generates a voice conversation ID from an SMS conversation context.
func voiceConvID(orgID, phone string) string {
	orgID = strings.TrimSpace(orgID)
	phone = strings.TrimSpace(phone)
	if orgID == "" || phone == "" {
		return ""
	}
	digits := sanitizeDigits(phone)
	digits = normalizeUSDigits(digits)
	return fmt.Sprintf("voice:%s:%s", orgID, digits)
}
