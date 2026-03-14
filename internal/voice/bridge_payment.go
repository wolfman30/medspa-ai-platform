package voice

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// PaymentConfirmationChannel returns the Redis pub/sub channel name for a caller's payment.
func PaymentConfirmationChannel(callerPhone string) string {
	return "voice:payment:" + callerPhone
}

// listenForPaymentConfirmation subscribes to Redis for payment events during this call.
// When Stripe webhook processes a successful payment from this caller, it publishes
// to the channel, and we inject a confirmation message into Lauren's conversation.
func (b *Bridge) listenForPaymentConfirmation(ctx context.Context) {
	channel := PaymentConfirmationChannel(b.callerPhone)
	pubsub := b.redisClient.Subscribe(ctx, channel)
	defer pubsub.Close()

	b.logger.Info("bridge: listening for payment confirmation",
		"channel", channel, "caller", b.callerPhone)

	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}

			b.mu.Lock()
			alreadyAnnounced := b.paymentConfirmationAnnounced
			if !alreadyAnnounced {
				b.paymentConfirmed = true
				b.paymentConfirmationAnnounced = true
			}
			b.mu.Unlock()

			if alreadyAnnounced {
				b.logger.Info("bridge: duplicate payment confirmation suppressed",
					"caller", b.callerPhone,
					"payload", msg.Payload,
				)
				continue
			}

			b.logger.Info("bridge: payment confirmation received!",
				"caller", b.callerPhone, "payload", msg.Payload)

			// Inject confirmation text into Lauren's conversation
			confirmText := fmt.Sprintf(
				"[SYSTEM: The patient's payment has been confirmed. Their deposit was successfully processed. " +
					"Tell them: 'I just got confirmation that your payment went through! You're all booked. " +
					"You'll receive a confirmation text shortly. Is there anything else I can help with?']")
			if err := b.sidecar.InjectText(confirmText); err != nil {
				b.logger.Error("bridge: failed to inject payment confirmation", "error", err)
			}
		}
	}
}

var (
	// weekdayDateTimePattern matches slot confirmations like "Monday at 3:00 PM".
	weekdayDateTimePattern = regexp.MustCompile(`(?i)\b(monday|tuesday|wednesday|thursday|friday|saturday|sunday)\b[^\n]{0,80}\b(\d{1,2}:\d{2}|\d{1,2})\s*(am|pm)\b`)
	// monthDateTimePattern matches slot confirmations like "March 15th at 3:00 PM".
	monthDateTimePattern = regexp.MustCompile(`(?i)\b(january|february|march|april|may|june|july|august|september|october|november|december)\b[^\n]{0,40}\b\d{1,2}(st|nd|rd|th)?\b[^\n]{0,40}\b(\d{1,2}:\d{2}|\d{1,2})\s*(am|pm)\b`)
)

// maybeCaptureSlotSelection records when Lauren explicitly confirms a date+time slot.
func (b *Bridge) maybeCaptureSlotSelection(text string) {
	if !looksLikeExplicitSlotSelection(text) {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.slotSelectionCaptured = true
}

// looksLikeExplicitSlotSelection returns true if the text contains both a confirmation
// phrase and a specific date+time reference.
func looksLikeExplicitSlotSelection(text string) bool {
	normalized := strings.ToLower(text)
	if !(strings.Contains(normalized, "works") || strings.Contains(normalized, "perfect") || strings.Contains(normalized, "great") || strings.Contains(normalized, "awesome") || strings.Contains(normalized, "book")) {
		return false
	}
	return weekdayDateTimePattern.MatchString(text) || monthDateTimePattern.MatchString(text)
}

// parseTranscriptRoleAndText extracts the role prefix ([assistant] or [user]) and
// the remaining text from a raw transcript line.
func parseTranscriptRoleAndText(raw string) (role, text string) {
	trimmed := strings.TrimSpace(raw)
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(lower, "[assistant]"):
		return "assistant", strings.TrimSpace(trimmed[len("[assistant]"):])
	case strings.HasPrefix(lower, "[user]"):
		return "user", strings.TrimSpace(trimmed[len("[user]"):])
	default:
		return "assistant", trimmed
	}
}

// maybeFireDepositSMS checks if Lauren's transcript indicates she's sending a deposit link,
// and fires the actual SMS. This is the workaround for Nova Sonic tools being disabled.
func (b *Bridge) maybeFireDepositSMS(ctx context.Context, text string) {
	b.mu.Lock()
	if b.depositSMSSent {
		b.mu.Unlock()
		return
	}
	slotSelected := b.slotSelectionCaptured
	b.mu.Unlock()

	if !slotSelected {
		b.logger.Info("bridge: deposit intent ignored until slot is explicitly selected",
			"caller", b.callerPhone,
			"text", text,
		)
		return
	}

	lower := strings.ToLower(text)
	// Detect deposit link intent: Lauren says she'll text/send a deposit/payment link
	hasDeposit := strings.Contains(lower, "deposit") || strings.Contains(lower, "payment")
	hasSend := strings.Contains(lower, "text you") || strings.Contains(lower, "send you") || strings.Contains(lower, "sending")
	hasLink := strings.Contains(lower, "link") || strings.Contains(lower, "secure link") || strings.Contains(lower, "secure deposit")

	if !(hasDeposit && hasSend) && !(hasDeposit && hasLink) {
		return
	}

	b.mu.Lock()
	if b.depositSMSSent {
		b.mu.Unlock()
		return
	}
	b.depositSMSSent = true
	b.mu.Unlock()

	b.logger.Info("bridge: detected deposit SMS intent in transcript, firing SMS",
		"caller", b.callerPhone, "org_id", b.orgID, "text", text)

	// Fire SMS async so we don't block audio
	go func() {
		if err := b.toolHandler.SendDepositSMS(ctx, b.orgID, b.callerPhone); err != nil {
			b.logger.Error("bridge: deposit SMS failed", "error", err, "caller", b.callerPhone)
		} else {
			b.logger.Info("bridge: deposit SMS sent successfully", "caller", b.callerPhone)
		}
	}()
}

// shouldProcessAssistantText suppresses duplicate assistant transcripts that can arrive
// from sidecar retries/replays. It deduplicates normalized text within a short time window.
// Uses substring matching to catch near-duplicates where one message is a prefix/suffix of another.
func (b *Bridge) shouldProcessAssistantText(text string) bool {
	normalized := strings.TrimSpace(strings.ToLower(text))
	if normalized == "" {
		return false
	}

	// dedupWindow is the time window in which identical or near-identical transcripts are suppressed.
	const dedupWindow = 30 * time.Second
	now := time.Now()

	b.mu.Lock()
	defer b.mu.Unlock()

	for prev, ts := range b.recentAssistantText {
		if now.Sub(ts) > dedupWindow {
			delete(b.recentAssistantText, prev)
			continue
		}
		// Exact match
		if prev == normalized {
			return false
		}
		// Fuzzy: one contains the other (catches rephrased near-duplicates)
		if strings.Contains(normalized, prev) || strings.Contains(prev, normalized) {
			return false
		}
	}

	b.recentAssistantText[normalized] = now
	return true
}
