package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// handleMessage decodes a queue message, dispatches it to the appropriate
// processor method (start, message, payment), and handles reply routing.
func (w *Worker) handleMessage(ctx context.Context, msg queueMessage) {
	var payload queuePayload
	if err := json.Unmarshal([]byte(msg.Body), &payload); err != nil {
		w.logger.Error("failed to decode conversation job", "error", err)
		w.deleteMessage(context.Background(), msg.ReceiptHandle)
		return
	}

	// Debug logging to track job processing
	w.logger.Info("worker processing job",
		"job_id", payload.ID,
		"kind", payload.Kind,
		"msg_id", msg.ID,
	)
	if payload.Kind == jobTypeMessage {
		w.logger.Info("worker job details",
			"job_id", payload.ID,
			"conversation_id", payload.Message.ConversationID,
			"message", payload.Message.Message,
			"from", payload.Message.From,
		)
	}

	if payload.Kind == jobTypeMessage && w.msgChecker != nil {
		providerID := providerMessageID(payload.Message.Metadata)
		if providerID != "" {
			exists, err := w.msgChecker.HasProviderMessage(ctx, providerID)
			if err != nil {
				w.logger.Warn("provider message lookup failed", "error", err, "provider_message_id", providerID, "job_id", payload.ID)
			} else if !exists {
				w.logger.Info("skipping conversation job: inbound message missing", "provider_message_id", providerID, "job_id", payload.ID)
				if payload.TrackStatus && w.jobs != nil {
					if storeErr := w.jobs.MarkFailed(ctx, payload.ID, "skipped: inbound message missing"); storeErr != nil {
						w.logger.Error("failed to update job status", "error", storeErr, "job_id", payload.ID)
					}
				}
				w.deleteMessage(context.Background(), msg.ReceiptHandle)
				return
			}
		}
	}

	var (
		err  error
		resp *Response
	)
	switch payload.Kind {
	case jobTypeStart:
		w.logger.Info("worker calling StartConversation", "job_id", payload.ID)
		resp, err = w.processor.StartConversation(ctx, payload.Start)
	case jobTypeMessage:
		resp, err = w.dispatchMessage(ctx, payload)
	case jobTypePayment:
		err = w.handlePaymentEvent(ctx, payload.Payment)
	case jobTypePaymentFailed:
		err = w.handlePaymentFailedEvent(ctx, payload.PaymentFailed)
	default:
		err = fmt.Errorf("conversation: unknown job type %q", payload.Kind)
	}

	w.finalizeJob(ctx, payload, resp, err)
	w.deleteMessage(context.Background(), msg.ReceiptHandle)
}

// dispatchMessage handles the jobTypeMessage case: voice callback check,
// deposit preloading, progress callback setup, and LLM processing.
func (w *Worker) dispatchMessage(ctx context.Context, payload queuePayload) (*Response, error) {
	// Check for voice callback request before LLM processing.
	if w.handleCallbackRequest(ctx, payload.Message) {
		w.logger.Info("voice callback handled, skipping LLM",
			"job_id", payload.ID,
			"conversation_id", payload.Message.ConversationID,
		)
		w.appendTranscript(ctx, payload.Message.ConversationID, SMSTranscriptMessage{
			Role:      "user",
			From:      payload.Message.From,
			To:        payload.Message.To,
			Body:      payload.Message.Message,
			Timestamp: time.Now(),
		})
		if payload.TrackStatus {
			if storeErr := w.jobs.MarkCompleted(ctx, payload.ID, nil, payload.Message.ConversationID); storeErr != nil {
				w.logger.Error("failed to update job status", "error", storeErr, "job_id", payload.ID)
			}
		}
		return nil, nil
	}

	// Pre-detect deposit intent and start parallel checkout generation.
	if w.depositPreloader != nil && ShouldPreloadDeposit(payload.Message.Message) {
		w.logger.Info("deposit preloader: detected potential deposit agreement, starting parallel generation",
			"job_id", payload.ID,
			"conversation_id", payload.Message.ConversationID,
		)
		w.depositPreloader.StartPreload(ctx, payload.Message.ConversationID, payload.Message.OrgID, payload.Message.LeadID, payload.Message.To)
	}

	// Set up progress callback to send intermediate SMS during long searches.
	w.attachProgressCallback(&payload)

	w.logger.Info("worker calling ProcessMessage", "job_id", payload.ID, "conversation_id", payload.Message.ConversationID)
	return w.processor.ProcessMessage(ctx, payload.Message)
}

// attachProgressCallback wires a callback on the message payload that sends
// intermediate SMS messages during long-running availability searches.
func (w *Worker) attachProgressCallback(payload *queuePayload) {
	progressSent := make(map[string]bool)
	payload.Message.OnProgress = func(progressCtx context.Context, msg string) {
		if w.messenger == nil || progressSent[msg] {
			return
		}
		progressSent[msg] = true
		reply := OutboundReply{
			OrgID:          payload.Message.OrgID,
			LeadID:         payload.Message.LeadID,
			ConversationID: payload.Message.ConversationID,
			To:             payload.Message.From,
			From:           payload.Message.To,
			Body:           msg,
		}
		sendCtx, cancel := context.WithTimeout(progressCtx, 5*time.Second)
		defer cancel()
		if err := w.messenger.SendReply(sendCtx, reply); err != nil {
			w.logger.Warn("failed to send progress SMS", "error", err)
		}
		// Save progress messages to transcript so they appear in admin UI.
		progressMsg := SMSTranscriptMessage{
			Role:      "assistant",
			Body:      msg,
			From:      payload.Message.To,
			To:        payload.Message.From,
			Timestamp: time.Now(),
		}
		if w.transcript != nil {
			_ = w.transcript.Append(progressCtx, payload.Message.ConversationID, progressMsg)
		}
		if w.convStore != nil {
			_ = w.convStore.AppendMessage(progressCtx, payload.Message.ConversationID, progressMsg)
		}
	}
}

// finalizeJob handles post-processing after a job completes or fails:
// status tracking, fallback replies on error, and response routing.
func (w *Worker) finalizeJob(ctx context.Context, payload queuePayload, resp *Response, err error) {
	if err != nil {
		w.logger.Error("conversation job failed", "error", err, "job_id", payload.ID, "kind", payload.Kind)
		if payload.TrackStatus {
			if storeErr := w.jobs.MarkFailed(ctx, payload.ID, err.Error()); storeErr != nil {
				w.logger.Error("failed to update job status", "error", storeErr, "job_id", payload.ID)
			}
		}
		if payload.Kind == jobTypeMessage {
			w.logger.Warn("sending fallback reply after conversation failure", "job_id", payload.ID, "org_id", payload.Message.OrgID)
			w.sendReply(ctx, payload, &Response{
				ConversationID: payload.Message.ConversationID,
				Message:        "Sorry - I'm having trouble responding right now. Please reply again in a moment.",
				Timestamp:      time.Now().UTC(),
			})
		}
		return
	}

	w.logger.Debug("conversation job processed", "job_id", payload.ID, "kind", payload.Kind)
	var convID string
	if resp != nil {
		convID = resp.ConversationID
		if convID == "" && payload.Kind == jobTypeMessage {
			convID = payload.Message.ConversationID
		}
	}
	if payload.TrackStatus {
		if storeErr := w.jobs.MarkCompleted(ctx, payload.ID, resp, convID); storeErr != nil {
			w.logger.Error("failed to update job status", "error", storeErr, "job_id", payload.ID)
		}
	}

	// Handle time selection for StartConversation (first message had all qualifications).
	if payload.Kind == jobTypeStart && resp != nil && resp.TimeSelectionResponse != nil && resp.TimeSelectionResponse.SMSMessage != "" {
		w.sendReply(ctx, payload, resp)
		startMsg := MessageRequest{
			OrgID:          payload.Start.OrgID,
			LeadID:         payload.Start.LeadID,
			ConversationID: resp.ConversationID,
			From:           payload.Start.From,
			To:             payload.Start.To,
			Channel:        payload.Start.Channel,
		}
		w.handleTimeSelectionResponse(ctx, startMsg, resp)
	}

	if payload.Kind == jobTypeMessage {
		w.routeMessageResponse(ctx, payload, resp)
	}
}

// routeMessageResponse directs a successful message response to the appropriate
// handler: time selection, Moxie booking, or standard reply with deposit check.
func (w *Worker) routeMessageResponse(ctx context.Context, payload queuePayload, resp *Response) {
	if resp != nil && resp.TimeSelectionResponse != nil && resp.TimeSelectionResponse.SMSMessage != "" {
		w.handleTimeSelectionResponse(ctx, payload.Message, resp)
	} else if resp != nil && resp.BookingRequest != nil && w.deposits != nil {
		w.handleMoxieBooking(ctx, payload.Message, resp.BookingRequest)
	} else {
		blocked := w.sendReply(ctx, payload, resp)
		if !blocked {
			w.handleDepositIntent(ctx, payload.Message, resp)
		}
	}
}

// deleteMessage removes a processed message from the queue.
func (w *Worker) deleteMessage(ctx context.Context, receiptHandle string) {
	if receiptHandle == "" {
		return
	}

	deleteCtx, cancel := context.WithTimeout(ctx, deleteTimeoutSeconds*time.Second)
	defer cancel()

	if err := w.queue.Delete(deleteCtx, receiptHandle); err != nil {
		w.logger.Error("failed to delete conversation job", "error", err)
	}
}

// providerMessageID extracts the provider-assigned message identifier from
// inbound message metadata, checking both generic and Telnyx-specific keys.
func providerMessageID(metadata map[string]string) string {
	if metadata == nil {
		return ""
	}
	if value := strings.TrimSpace(metadata["provider_message_id"]); value != "" {
		return value
	}
	if value := strings.TrimSpace(metadata["telnyx_message_id"]); value != "" {
		return value
	}
	return ""
}
