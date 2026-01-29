package messaging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

var telnyxSendTracer = otel.Tracer("medspa.internal.messaging.telnyx_send")

// TelnyxSender posts SMS messages using Telnyx's V2 API.
type TelnyxSender struct {
	apiKey             string
	messagingProfileID string
	httpClient         *http.Client
	logger             *logging.Logger
}

// NewTelnyxSender builds a sender for Telnyx V2 API.
func NewTelnyxSender(apiKey, messagingProfileID string, logger *logging.Logger) *TelnyxSender {
	if logger == nil {
		logger = logging.Default()
	}
	return &TelnyxSender{
		apiKey:             apiKey,
		messagingProfileID: messagingProfileID,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

var _ conversation.ReplyMessenger = (*TelnyxSender)(nil)

// SendReply dispatches a single SMS via Telnyx V2 API, retrying transient failures.
func (s *TelnyxSender) SendReply(ctx context.Context, msg conversation.OutboundReply) error {
	if s.apiKey == "" {
		return errors.New("messaging: telnyx api key missing")
	}
	if msg.To == "" {
		return errors.New("messaging: to required")
	}
	if msg.From == "" {
		return errors.New("messaging: from required")
	}
	if strings.TrimSpace(msg.Body) == "" {
		return errors.New("messaging: body required")
	}

	ctx, span := telnyxSendTracer.Start(ctx, "messaging.telnyx.send")
	defer span.End()
	span.SetAttributes(
		attribute.String("medspa.org_id", msg.OrgID),
		attribute.String("medspa.to", msg.To),
		attribute.String("medspa.from", msg.From),
	)

	payload := map[string]interface{}{
		"from": msg.From,
		"to":   msg.To,
		"text": msg.Body,
	}
	if s.messagingProfileID != "" {
		payload["messaging_profile_id"] = s.messagingProfileID
	}

	var attempt int
	var lastErr error
	for attempt = 1; attempt <= 3; attempt++ {
		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("messaging: failed to marshal telnyx payload: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telnyx.com/v2/messages", bytes.NewReader(bodyBytes))
		if err != nil {
			lastErr = err
			break
		}
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := s.httpClient.Do(req)
		if err != nil {
			lastErr = err
		} else {
			defer resp.Body.Close()
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				if msg.Metadata != nil && len(body) > 0 {
					var parsed struct {
						Data struct {
							ID     string `json:"id"`
							Status string `json:"status"`
						} `json:"data"`
					}
					if err := json.Unmarshal(body, &parsed); err == nil {
						if parsed.Data.ID != "" {
							msg.Metadata["provider_message_id"] = parsed.Data.ID
						}
						if parsed.Data.Status != "" {
							msg.Metadata["provider_status"] = parsed.Data.Status
						}
					}
				}
				s.logger.Info("telnyx sms sent", "org_id", msg.OrgID, "to", msg.To, "from", msg.From)
				return nil
			}
			// Read error response for better debugging
			var errorBody map[string]interface{}
			if len(body) > 0 && json.Unmarshal(body, &errorBody) == nil {
				lastErr = fmt.Errorf("telnyx send failed: status %d, body: %v", resp.StatusCode, errorBody)
			} else {
				lastErr = fmt.Errorf("telnyx send failed: status %d", resp.StatusCode)
			}
		}

		if attempt < 3 {
			sleep := time.Duration(200+rand.Intn(300)) * time.Millisecond
			time.Sleep(sleep)
		}
	}

	if lastErr != nil {
		span.RecordError(lastErr)
		s.logger.Error("failed to send telnyx sms", "error", lastErr, "org_id", msg.OrgID, "to", msg.To)
	}
	return lastErr
}
