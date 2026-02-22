package conversation

import (
	"context"
	"strings"
	"time"
)

// sendInstagramReply sends an AI reply via Instagram DM.
func (w *Worker) sendInstagramReply(ctx context.Context, payload queuePayload, resp *Response) bool {
	if resp == nil || resp.Message == "" {
		return false
	}
	msg := payload.Message
	if msg.From == "" {
		return false
	}

	outboundText, blocked := w.applySupervisor(ctx, SupervisorRequest{
		OrgID:          msg.OrgID,
		ConversationID: msg.ConversationID,
		LeadID:         msg.LeadID,
		Channel:        msg.Channel,
		UserMessage:    msg.Message,
		DraftMessage:   resp.Message,
	})
	if blocked {
		resp = &Response{
			ConversationID: resp.ConversationID,
			Message:        outboundText,
			Timestamp:      time.Now().UTC(),
		}
	} else if outboundText != resp.Message {
		resp = &Response{
			ConversationID: resp.ConversationID,
			Message:        outboundText,
			Timestamp:      resp.Timestamp,
		}
	}

	// Output guard
	leakResult := ScanOutputForLeaks(resp.Message)
	if leakResult.Leaked {
		for _, reason := range leakResult.Reasons {
			w.events.OutputGuardTriggered(ctx, resp.ConversationID, msg.OrgID, reason)
		}
		w.logger.Warn("output guard: sensitive data leak detected (instagram)",
			"conversation_id", resp.ConversationID,
			"org_id", msg.OrgID,
			"reasons", leakResult.Reasons,
		)
		if leakResult.Sanitized == "" {
			resp = &Response{
				ConversationID: resp.ConversationID,
				Message:        defaultSupervisorFallback,
				Timestamp:      time.Now().UTC(),
			}
		} else {
			resp = &Response{
				ConversationID: resp.ConversationID,
				Message:        leakResult.Sanitized,
				Timestamp:      resp.Timestamp,
			}
		}
	}

	conversationID := strings.TrimSpace(resp.ConversationID)
	if conversationID == "" {
		conversationID = strings.TrimSpace(msg.ConversationID)
	}

	reply := OutboundReply{
		OrgID:          msg.OrgID,
		LeadID:         msg.LeadID,
		ConversationID: conversationID,
		To:             msg.From, // IG sender ID — reply back to them
		From:           msg.To,
		Body:           resp.Message,
		Metadata: map[string]string{
			"job_id":  payload.ID,
			"channel": "instagram",
		},
	}

	var sendErr error
	if w.igMessenger != nil {
		sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		if err := w.igMessenger.SendReply(sendCtx, reply); err != nil {
			sendErr = err
			w.logger.Error("failed to send instagram reply", "error", err, "job_id", payload.ID, "org_id", msg.OrgID)
		}
	} else {
		w.logger.Warn("instagram messenger not configured, cannot send reply",
			"job_id", payload.ID,
			"org_id", msg.OrgID,
		)
	}

	errorReason := ""
	if sendErr != nil {
		errorReason = sendErr.Error()
	}
	providerStatus := "sent"
	if sendErr != nil {
		providerStatus = "failed"
	}

	w.appendTranscript(ctx, conversationID, SMSTranscriptMessage{
		Role:        "assistant",
		From:        msg.To,
		To:          msg.From,
		Body:        resp.Message,
		Timestamp:   resp.Timestamp,
		Kind:        "instagram_reply",
		Status:      providerStatus,
		ErrorReason: errorReason,
	})
	return blocked
}
