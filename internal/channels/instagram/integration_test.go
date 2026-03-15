package instagram

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// TestE2EWebhookToEnqueue simulates a full inbound Instagram DM webhook flow:
// webhook payload → signature verification → parse → org resolution → lead resolution → enqueue
func TestE2EWebhookToEnqueue(t *testing.T) {
	appSecret := "e2e_test_secret"

	pub := &e2ePub{}
	orgResolver := &e2eOrgResolver{mapping: map[string]string{
		"page_100": "org_medspa_1",
	}}
	leadResolver := &e2eLeadResolver{}

	adapter := NewAdapter(AdapterConfig{
		PageAccessToken: "fake_token",
		AppSecret:       appSecret,
		VerifyToken:     "my_verify",
		Publisher:       pub,
		OrgResolver:     orgResolver,
		LeadResolver:    leadResolver,
		Logger:          logging.Default(),
	})

	// Simulate Meta webhook payload
	event := WebhookEvent{
		Object: "instagram",
		Entry: []Entry{{
			ID:   "page_100",
			Time: time.Now().UnixMilli(),
			Messaging: []Messaging{
				{
					Sender:    Sender{ID: "ig_user_42"},
					Recipient: Recipient{ID: "page_100"},
					Timestamp: time.Now().UnixMilli(),
					Message:   &Message{MID: "mid_abc123", Text: "Do you offer lip fillers?"},
				},
			},
		}},
	}

	body, _ := json.Marshal(event)
	sig := signPayload(appSecret, body)

	// POST to webhook
	req := httptest.NewRequest(http.MethodPost, "/webhooks/instagram", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sig)
	w := httptest.NewRecorder()
	adapter.HandleWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Allow async processing
	time.Sleep(50 * time.Millisecond)

	// Verify message was enqueued
	msgs := pub.getMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 enqueued message, got %d", len(msgs))
	}

	m := msgs[0]
	if m.OrgID != "org_medspa_1" {
		t.Errorf("OrgID = %s, want org_medspa_1", m.OrgID)
	}
	if m.Channel != conversation.ChannelInstagram {
		t.Errorf("Channel = %s, want instagram", m.Channel)
	}
	if m.Message != "Do you offer lip fillers?" {
		t.Errorf("Message = %q", m.Message)
	}
	if m.From != "ig_user_42" {
		t.Errorf("From = %s, want ig_user_42", m.From)
	}
	if m.To != "page_100" {
		t.Errorf("To = %s, want page_100", m.To)
	}
	if m.LeadID != "lead_ig_user_42" {
		t.Errorf("LeadID = %s, want lead_ig_user_42", m.LeadID)
	}
	if m.ConversationID != "ig_org_medspa_1_ig_user_42" {
		t.Errorf("ConversationID = %s, want ig_org_medspa_1_ig_user_42", m.ConversationID)
	}
}

// TestE2EVerificationChallenge tests the GET verification flow end-to-end.
func TestE2EVerificationChallenge(t *testing.T) {
	adapter := NewAdapter(AdapterConfig{
		PageAccessToken: "token",
		AppSecret:       "secret",
		VerifyToken:     "my_verify_token",
		Logger:          logging.Default(),
	})

	req := httptest.NewRequest(http.MethodGet,
		"/webhooks/instagram?hub.mode=subscribe&hub.verify_token=my_verify_token&hub.challenge=META_CHALLENGE_XYZ",
		nil)
	w := httptest.NewRecorder()
	adapter.HandleVerification(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "META_CHALLENGE_XYZ" {
		t.Errorf("body = %s, want META_CHALLENGE_XYZ", w.Body.String())
	}
}

// TestE2EBadSignatureRejected verifies tampered payloads are rejected.
func TestE2EBadSignatureRejected(t *testing.T) {
	adapter := NewAdapter(AdapterConfig{
		PageAccessToken: "token",
		AppSecret:       "real_secret",
		VerifyToken:     "verify",
		Logger:          logging.Default(),
	})

	body := []byte(`{"object":"instagram","entry":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/instagram", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", "sha256=tampered")
	w := httptest.NewRecorder()
	adapter.HandleWebhook(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// --- test helpers ---

func signPayload(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

type e2ePub struct {
	mu   sync.Mutex
	msgs []conversation.MessageRequest
}

func (p *e2ePub) EnqueueMessage(_ context.Context, _ string, req conversation.MessageRequest, _ ...conversation.PublishOption) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.msgs = append(p.msgs, req)
	return nil
}

func (p *e2ePub) getMessages() []conversation.MessageRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	cp := make([]conversation.MessageRequest, len(p.msgs))
	copy(cp, p.msgs)
	return cp
}

type e2eOrgResolver struct {
	mapping map[string]string
}

func (r *e2eOrgResolver) ResolveByInstagramPageID(_ context.Context, pageID string) (string, error) {
	if orgID, ok := r.mapping[pageID]; ok {
		return orgID, nil
	}
	return "default", nil
}

type e2eLeadResolver struct{}

func (r *e2eLeadResolver) FindOrCreateByInstagramID(_ context.Context, _, igSenderID string) (string, bool, error) {
	return "lead_" + igSenderID, true, nil
}
