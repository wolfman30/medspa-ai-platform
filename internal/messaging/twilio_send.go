package messaging

import (
	"context"
	"errors"
	"fmt"
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
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				s.logger.Info("twilio sms sent", "org_id", msg.OrgID, "to", msg.To)
				return nil
			}
			lastErr = fmt.Errorf("twilio send failed: status %d", resp.StatusCode)
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
