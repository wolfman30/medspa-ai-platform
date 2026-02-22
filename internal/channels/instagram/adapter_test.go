package instagram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

type stubPublisher struct {
	mu       sync.Mutex
	messages []conversation.MessageRequest
}

func (s *stubPublisher) EnqueueMessage(_ context.Context, _ string, req conversation.MessageRequest, _ ...conversation.PublishOption) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, req)
	return nil
}

type stubOrgResolver struct {
	orgID string
}

func (r *stubOrgResolver) ResolveByInstagramPageID(_ context.Context, _ string) (string, error) {
	return r.orgID, nil
}

type stubLeadResolver struct{}

func (r *stubLeadResolver) FindOrCreateByInstagramID(_ context.Context, _, igSenderID string) (string, bool, error) {
	return "lead_" + igSenderID, true, nil
}

func TestAdapterEnqueuesMessage(t *testing.T) {
	pub := &stubPublisher{}
	adapter := NewAdapter(AdapterConfig{
		PageAccessToken: "token",
		AppSecret:       "secret",
		VerifyToken:     "verify",
		Publisher:       pub,
		OrgResolver:     &stubOrgResolver{orgID: "org_1"},
		LeadResolver:    &stubLeadResolver{},
		Logger:          logging.Default(),
	})

	msg := ParsedInboundMessage{
		SenderID:    "user_123",
		RecipientID: "page_456",
		Text:        "I want Botox",
		Timestamp:   time.Now(),
		MessageID:   "mid_001",
	}

	adapter.handleInboundMessage(msg)

	pub.mu.Lock()
	defer pub.mu.Unlock()

	if len(pub.messages) != 1 {
		t.Fatalf("expected 1 enqueued message, got %d", len(pub.messages))
	}
	m := pub.messages[0]
	if m.Channel != conversation.ChannelInstagram {
		t.Errorf("channel = %s, want instagram", m.Channel)
	}
	if m.Message != "I want Botox" {
		t.Errorf("message = %s, want 'I want Botox'", m.Message)
	}
	if m.OrgID != "org_1" {
		t.Errorf("org_id = %s, want org_1", m.OrgID)
	}
	if m.LeadID != "lead_user_123" {
		t.Errorf("lead_id = %s, want lead_user_123", m.LeadID)
	}
	if m.From != "user_123" {
		t.Errorf("from = %s, want user_123", m.From)
	}
}

func TestAdapterPostbackEnqueue(t *testing.T) {
	pub := &stubPublisher{}
	adapter := NewAdapter(AdapterConfig{
		PageAccessToken: "token",
		AppSecret:       "secret",
		VerifyToken:     "verify",
		Publisher:       pub,
		Logger:          logging.Default(),
	})

	msg := ParsedInboundMessage{
		SenderID:        "user_789",
		RecipientID:     "page_456",
		Text:            "Book Now",
		Timestamp:       time.Now(),
		IsPostback:      true,
		PostbackPayload: "BOOK_NOW",
		MessageID:       "mid_002",
	}

	adapter.handleInboundMessage(msg)

	pub.mu.Lock()
	defer pub.mu.Unlock()

	if len(pub.messages) != 1 {
		t.Fatalf("expected 1 enqueued message, got %d", len(pub.messages))
	}
	if pub.messages[0].Message != "BOOK_NOW" {
		t.Errorf("message = %s, want BOOK_NOW", pub.messages[0].Message)
	}
}

func TestAdapterSendReply(t *testing.T) {
	var sentTo, sentText string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req SendRequest
		json.NewDecoder(r.Body).Decode(&req)
		sentTo = req.Recipient.ID
		sentText = req.Message.Text
		resp := SendResponse{RecipientID: sentTo, MessageID: "mid_reply"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	adapter := NewAdapter(AdapterConfig{
		PageAccessToken: "token",
		AppSecret:       "secret",
		VerifyToken:     "verify",
		Logger:          logging.Default(),
	})
	adapter.SetGraphAPIBase(server.URL)

	err := adapter.SendReply(context.Background(), conversation.OutboundReply{
		To:   "user_123",
		Body: "Your Botox appointment is confirmed! 💉",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sentTo != "user_123" {
		t.Errorf("sent to = %s, want user_123", sentTo)
	}
	if sentText != "Your Botox appointment is confirmed! 💉" {
		t.Errorf("sent text = %s", sentText)
	}
}

func TestDeterministicConversationID(t *testing.T) {
	id1 := deterministicConversationID("org_1", "user_123")
	id2 := deterministicConversationID("org_1", "user_123")
	id3 := deterministicConversationID("org_1", "user_456")

	if id1 != id2 {
		t.Errorf("same inputs should give same ID: %s != %s", id1, id2)
	}
	if id1 == id3 {
		t.Error("different inputs should give different IDs")
	}
	if id1 != "ig_org_1_user_123" {
		t.Errorf("unexpected format: %s", id1)
	}
}
