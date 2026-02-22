package instagram

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifySignature(t *testing.T) {
	secret := "test_app_secret"
	body := []byte(`{"object":"instagram","entry":[]}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	tests := []struct {
		name      string
		secret    string
		body      []byte
		signature string
		want      bool
	}{
		{"valid signature", secret, body, validSig, true},
		{"wrong signature", secret, body, "sha256=0000000000000000000000000000000000000000000000000000000000000000", false},
		{"empty signature", secret, body, "", false},
		{"empty secret", "", body, validSig, false},
		{"missing prefix", secret, body, "abcdef", false},
		{"tampered body", secret, []byte(`tampered`), validSig, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VerifySignature(tt.secret, tt.body, tt.signature)
			if got != tt.want {
				t.Errorf("VerifySignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHandleVerification(t *testing.T) {
	h := NewWebhookHandler("my_verify_token", "secret", nil)

	t.Run("valid challenge", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/webhooks/instagram?hub.mode=subscribe&hub.verify_token=my_verify_token&hub.challenge=CHALLENGE_123",
			nil)
		w := httptest.NewRecorder()
		h.HandleVerification(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if w.Body.String() != "CHALLENGE_123" {
			t.Fatalf("expected CHALLENGE_123, got %s", w.Body.String())
		}
	})

	t.Run("wrong token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/webhooks/instagram?hub.mode=subscribe&hub.verify_token=wrong&hub.challenge=X",
			nil)
		w := httptest.NewRecorder()
		h.HandleVerification(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", w.Code)
		}
	})

	t.Run("wrong mode", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/webhooks/instagram?hub.mode=unsubscribe&hub.verify_token=my_verify_token&hub.challenge=X",
			nil)
		w := httptest.NewRecorder()
		h.HandleVerification(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", w.Code)
		}
	})
}

func TestParseWebhookEvent(t *testing.T) {
	t.Run("text message", func(t *testing.T) {
		event := WebhookEvent{
			Object: "instagram",
			Entry: []Entry{
				{
					ID:   "page_123",
					Time: 1700000000000,
					Messaging: []Messaging{
						{
							Sender:    Sender{ID: "user_456"},
							Recipient: Recipient{ID: "page_123"},
							Timestamp: 1700000000000,
							Message:   &Message{MID: "mid_001", Text: "I want Botox"},
						},
					},
				},
			},
		}

		msgs := ParseWebhookEvent(event)
		if len(msgs) != 1 {
			t.Fatalf("expected 1 message, got %d", len(msgs))
		}
		if msgs[0].SenderID != "user_456" {
			t.Errorf("sender = %s, want user_456", msgs[0].SenderID)
		}
		if msgs[0].RecipientID != "page_123" {
			t.Errorf("recipient = %s, want page_123", msgs[0].RecipientID)
		}
		if msgs[0].Text != "I want Botox" {
			t.Errorf("text = %s, want 'I want Botox'", msgs[0].Text)
		}
		if msgs[0].IsPostback {
			t.Error("expected IsPostback=false")
		}
		if msgs[0].MessageID != "mid_001" {
			t.Errorf("message_id = %s, want mid_001", msgs[0].MessageID)
		}
	})

	t.Run("postback", func(t *testing.T) {
		event := WebhookEvent{
			Object: "instagram",
			Entry: []Entry{
				{
					Messaging: []Messaging{
						{
							Sender:    Sender{ID: "user_789"},
							Timestamp: 1700000001000,
							Postback:  &Postback{Title: "Book Now", Payload: "BOOK_NOW"},
						},
					},
				},
			},
		}

		msgs := ParseWebhookEvent(event)
		if len(msgs) != 1 {
			t.Fatalf("expected 1 message, got %d", len(msgs))
		}
		if !msgs[0].IsPostback {
			t.Error("expected IsPostback=true")
		}
		if msgs[0].PostbackPayload != "BOOK_NOW" {
			t.Errorf("payload = %s, want BOOK_NOW", msgs[0].PostbackPayload)
		}
	})

	t.Run("empty messaging", func(t *testing.T) {
		event := WebhookEvent{
			Object: "instagram",
			Entry: []Entry{
				{Messaging: []Messaging{{Sender: Sender{ID: "x"}, Timestamp: 0}}},
			},
		}
		msgs := ParseWebhookEvent(event)
		if len(msgs) != 0 {
			t.Fatalf("expected 0 messages, got %d", len(msgs))
		}
	})
}

func TestHandleInbound(t *testing.T) {
	appSecret := "test_secret"
	var received []ParsedInboundMessage

	h := NewWebhookHandler("token", appSecret, func(msg ParsedInboundMessage) {
		received = append(received, msg)
	})

	event := WebhookEvent{
		Object: "instagram",
		Entry: []Entry{{
			Messaging: []Messaging{{
				Sender:    Sender{ID: "sender_1"},
				Timestamp: 1700000000000,
				Message:   &Message{MID: "m1", Text: "Hello"},
			}},
		}},
	}

	body, _ := json.Marshal(event)
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/webhooks/instagram", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sig)
	w := httptest.NewRecorder()

	h.HandleInbound(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(received) != 1 {
		t.Fatalf("expected 1 received message, got %d", len(received))
	}
	if received[0].Text != "Hello" {
		t.Errorf("text = %s, want Hello", received[0].Text)
	}
}

func TestHandleInboundBadSignature(t *testing.T) {
	h := NewWebhookHandler("token", "secret", nil)

	body := []byte(`{"object":"instagram","entry":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/instagram", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", "sha256=bad")
	w := httptest.NewRecorder()

	h.HandleInbound(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
