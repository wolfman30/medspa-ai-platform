package messaging

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// ValidateTwilioSignature validates that a request came from Twilio
func ValidateTwilioSignature(r *http.Request, authToken, webhookURL string) bool {
	signature := r.Header.Get("X-Twilio-Signature")
	if signature == "" {
		return false
	}

	// Parse form data
	if err := r.ParseForm(); err != nil {
		return false
	}

	// Build the signature payload
	payload := buildSignaturePayload(webhookURL, r.PostForm)

	// Compute expected signature
	expectedSignature := computeSignature(payload, authToken)

	return signature == expectedSignature
}

// buildSignaturePayload creates the payload string for signature verification
func buildSignaturePayload(url string, params url.Values) string {
	// Get all keys and sort them
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build payload: URL + sorted params
	var payload strings.Builder
	payload.WriteString(url)

	for _, key := range keys {
		for _, value := range params[key] {
			payload.WriteString(key)
			payload.WriteString(value)
		}
	}

	return payload.String()
}

// computeSignature computes the HMAC-SHA1 signature
func computeSignature(data, key string) string {
	h := hmac.New(sha1.New, []byte(key))
	h.Write([]byte(data))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// TwilioWebhookRequest represents an incoming Twilio webhook
type TwilioWebhookRequest struct {
	MessageSid string
	AccountSid string
	From       string
	To         string
	Body       string
	NumMedia   string
	MediaURLs  []string
}

// ParseTwilioWebhook parses a Twilio webhook request
func ParseTwilioWebhook(r *http.Request) (*TwilioWebhookRequest, error) {
	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("failed to parse form: %w", err)
	}

	req := &TwilioWebhookRequest{
		MessageSid: r.FormValue("MessageSid"),
		AccountSid: r.FormValue("AccountSid"),
		From:       r.FormValue("From"),
		To:         r.FormValue("To"),
		Body:       r.FormValue("Body"),
		NumMedia:   r.FormValue("NumMedia"),
	}

	return req, nil
}
