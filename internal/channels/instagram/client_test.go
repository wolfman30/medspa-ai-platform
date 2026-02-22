package instagram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendTextMessage(t *testing.T) {
	var received SendRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test_token" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		resp := SendResponse{RecipientID: "user_1", MessageID: "mid_001"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("test_token")
	client.SetGraphAPIBase(server.URL)

	resp, err := client.SendTextMessage(context.Background(), "user_1", "Hello from bot")
	if err != nil {
		t.Fatal(err)
	}
	if resp.RecipientID != "user_1" {
		t.Errorf("recipient = %s, want user_1", resp.RecipientID)
	}
	if received.Recipient.ID != "user_1" {
		t.Errorf("sent to = %s, want user_1", received.Recipient.ID)
	}
	if received.Message.Text != "Hello from bot" {
		t.Errorf("sent text = %s, want 'Hello from bot'", received.Message.Text)
	}
}

func TestSendButtonMessage(t *testing.T) {
	var received SendRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		resp := SendResponse{RecipientID: "user_2", MessageID: "mid_002"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("token")
	client.SetGraphAPIBase(server.URL)

	buttons := []Button{
		{Type: "web_url", Title: "Book Now", URL: "https://example.com/book"},
	}
	resp, err := client.SendButtonMessage(context.Background(), "user_2", "Ready to book?", buttons)
	if err != nil {
		t.Fatal(err)
	}
	if resp.MessageID != "mid_002" {
		t.Errorf("message_id = %s, want mid_002", resp.MessageID)
	}
	if received.Message.Attachment == nil {
		t.Fatal("expected attachment")
	}
	if len(received.Message.Attachment.Payload.Buttons) != 1 {
		t.Fatalf("expected 1 button, got %d", len(received.Message.Attachment.Payload.Buttons))
	}
}

func TestSendTextMessageAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := SendResponse{
			Error: &SendError{Code: 100, Message: "Invalid token", Type: "OAuthException"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient("bad_token")
	client.SetGraphAPIBase(server.URL)

	_, err := client.SendTextMessage(context.Background(), "user_1", "test")
	if err == nil {
		t.Fatal("expected error for API error response")
	}
}
