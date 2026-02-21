package instagram

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// WebhookHandler handles Instagram webhook verification and inbound messages.
type WebhookHandler struct {
	verifyToken string
	appSecret   string
	onMessage   func(msg ParsedInboundMessage)
}

// NewWebhookHandler creates a new webhook handler.
// onMessage is called for each parsed inbound message or postback.
func NewWebhookHandler(verifyToken, appSecret string, onMessage func(ParsedInboundMessage)) *WebhookHandler {
	return &WebhookHandler{
		verifyToken: verifyToken,
		appSecret:   appSecret,
		onMessage:   onMessage,
	}
}

// HandleVerification handles the GET webhook verification challenge from Meta.
func (h *WebhookHandler) HandleVerification(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("hub.mode")
	token := r.URL.Query().Get("hub.verify_token")
	challenge := r.URL.Query().Get("hub.challenge")

	if mode == "subscribe" && token == h.verifyToken {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, challenge)
		return
	}

	http.Error(w, "Forbidden", http.StatusForbidden)
}

// HandleInbound handles POST webhook events (incoming messages).
func (h *WebhookHandler) HandleInbound(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// Verify signature
	signature := r.Header.Get("X-Hub-Signature-256")
	if !VerifySignature(h.appSecret, body, signature) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var event WebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// Must respond 200 quickly to avoid Meta retries
	w.WriteHeader(http.StatusOK)

	// Process messages
	messages := ParseWebhookEvent(event)
	for _, msg := range messages {
		if h.onMessage != nil {
			h.onMessage(msg)
		}
	}
}

// ParseWebhookEvent extracts ParsedInboundMessages from a webhook event.
func ParseWebhookEvent(event WebhookEvent) []ParsedInboundMessage {
	var messages []ParsedInboundMessage

	for _, entry := range event.Entry {
		for _, m := range entry.Messaging {
			parsed := ParsedInboundMessage{
				SenderID:  m.Sender.ID,
				Timestamp: time.UnixMilli(m.Timestamp),
			}

			if m.Message != nil {
				parsed.Text = m.Message.Text
				parsed.MessageID = m.Message.MID
			} else if m.Postback != nil {
				parsed.IsPostback = true
				parsed.Text = m.Postback.Title
				parsed.PostbackPayload = m.Postback.Payload
			} else {
				continue
			}

			messages = append(messages, parsed)
		}
	}

	return messages
}

// VerifySignature verifies the X-Hub-Signature-256 header.
func VerifySignature(appSecret string, body []byte, signature string) bool {
	if appSecret == "" || signature == "" {
		return false
	}

	// Signature format: "sha256=<hex>"
	const prefix = "sha256="
	if len(signature) <= len(prefix) {
		return false
	}
	sigHex := signature[len(prefix):]

	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expected), []byte(sigHex))
}
