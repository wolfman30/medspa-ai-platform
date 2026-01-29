package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

var twilioSendTracer = otel.Tracer("medspa.internal.messaging.twilio_send")

// TwilioSender posts SMS messages using Twilio's REST API.
type TwilioSender struct {
	accountSID string
	authToken  string
	from       string
	httpClient *http.Client
	logger     *logging.Logger
}

// NewTwilioSender builds a sender with sane defaults.
func NewTwilioSender(accountSID, authToken, defaultFrom string, logger *logging.Logger) *TwilioSender {
	if logger == nil {
		logger = logging.Default()
	}
	return &TwilioSender{
		accountSID: accountSID,
		authToken:  authToken,
		from:       defaultFrom,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

var _ conversation.ReplyMessenger = (*TwilioSender)(nil)

// SendReply dispatches a single SMS, retrying transient failures.
func (s *TwilioSender) SendReply(ctx context.Context, msg conversation.OutboundReply) error {
	if s.accountSID == "" || s.authToken == "" {
		return errors.New("messaging: twilio credentials missing")
	}
	if msg.To == "" {
		return errors.New("messaging: to required")
	}
	if msg.From == "" {
		msg.From = s.from
	}
	if msg.From == "" {
		return errors.New("messaging: from required")
	}
	if strings.TrimSpace(msg.Body) == "" {
		return errors.New("messaging: body required")
	}

	ctx, span := twilioSendTracer.Start(ctx, "messaging.twilio.send")
	defer span.End()
	span.SetAttributes(
		attribute.String("medspa.org_id", msg.OrgID),
		attribute.String("medspa.to", msg.To),
	)

	payload := url.Values{}
	payload.Set("To", msg.To)
	payload.Set("From", msg.From)
	payload.Set("Body", msg.Body)

	endpoint := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", s.accountSID)

	var attempt int
	var lastErr error
	for attempt = 1; attempt <= 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(payload.Encode()))
		if err != nil {
			lastErr = err
			break
		}
		req.SetBasicAuth(s.accountSID, s.authToken)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := s.httpClient.Do(req)
		if err != nil {
			lastErr = err
		} else {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				if msg.Metadata != nil && len(body) > 0 {
					var parsed struct {
						SID    string `json:"sid"`
						Status string `json:"status"`
					}
					if err := json.Unmarshal(body, &parsed); err == nil {
						if parsed.SID != "" {
							msg.Metadata["provider_message_id"] = parsed.SID
						}
						if parsed.Status != "" {
							msg.Metadata["provider_status"] = parsed.Status
						}
					}
				}
				s.logger.Info("twilio sms sent", "org_id", msg.OrgID, "to", msg.To)
				return nil
			}
			lastErr = fmt.Errorf("twilio send failed: %s", formatTwilioError(resp.StatusCode, body))
			// Don't retry non-rate-limit 4xx errors.
			if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 429 {
				break
			}
		}

		if attempt < 3 {
			sleep := time.Duration(200+rand.Intn(300)) * time.Millisecond
			time.Sleep(sleep)
		}
	}

	if lastErr != nil {
		span.RecordError(lastErr)
	}
	return lastErr
}

type twilioAPIError struct {
	Code     int    `json:"code"`
	Message  string `json:"message"`
	MoreInfo string `json:"more_info"`
	Status   int    `json:"status"`
}

func formatTwilioError(status int, body []byte) string {
	body = bytesTrimSpace(body)
	if len(body) == 0 {
		return fmt.Sprintf("status %d", status)
	}
	var parsed twilioAPIError
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Message != "" {
		if parsed.Code != 0 {
			return fmt.Sprintf("status %d code %d: %s", status, parsed.Code, parsed.Message)
		}
		return fmt.Sprintf("status %d: %s", status, parsed.Message)
	}
	// Fallback: return raw body (truncated by ReadAll limit).
	return fmt.Sprintf("status %d: %s", status, string(body))
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}
