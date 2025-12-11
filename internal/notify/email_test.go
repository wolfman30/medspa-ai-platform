package notify

import (
	"context"
	"testing"
)

func TestNewSendGridSender_NilWithoutAPIKey(t *testing.T) {
	sender := NewSendGridSender(SendGridConfig{
		APIKey:    "",
		FromEmail: "test@example.com",
	}, nil)

	if sender != nil {
		t.Error("expected nil sender when API key is empty")
	}
}

func TestNewSendGridSender_DefaultFromName(t *testing.T) {
	sender := NewSendGridSender(SendGridConfig{
		APIKey:    "test-key",
		FromEmail: "test@example.com",
		FromName:  "",
	}, nil)

	if sender == nil {
		t.Fatal("expected non-nil sender")
	}
	if sender.fromName != "MedSpa AI" {
		t.Errorf("expected default from name 'MedSpa AI', got %q", sender.fromName)
	}
}

func TestNewSendGridSender_CustomFromName(t *testing.T) {
	sender := NewSendGridSender(SendGridConfig{
		APIKey:    "test-key",
		FromEmail: "test@example.com",
		FromName:  "Custom Name",
	}, nil)

	if sender == nil {
		t.Fatal("expected non-nil sender")
	}
	if sender.fromName != "Custom Name" {
		t.Errorf("expected from name 'Custom Name', got %q", sender.fromName)
	}
}

func TestSendGridSender_Send_NilClient(t *testing.T) {
	sender := &SendGridSender{
		client: nil,
	}

	err := sender.Send(context.Background(), EmailMessage{
		To:      "recipient@example.com",
		Subject: "Test",
		Body:    "Test body",
	})

	if err == nil {
		t.Error("expected error when client is nil")
	}
}

func TestStubEmailSender_Send(t *testing.T) {
	sender := NewStubEmailSender(nil)

	err := sender.Send(context.Background(), EmailMessage{
		To:      "recipient@example.com",
		Subject: "Test Subject",
		Body:    "Test body",
	})

	if err != nil {
		t.Errorf("stub sender should not return error, got: %v", err)
	}
}

func TestEmailMessage_Fields(t *testing.T) {
	msg := EmailMessage{
		To:      "recipient@example.com",
		ToName:  "John Doe",
		Subject: "Test Subject",
		Body:    "Plain text body",
		HTML:    "<p>HTML body</p>",
	}

	if msg.To != "recipient@example.com" {
		t.Errorf("unexpected To: %s", msg.To)
	}
	if msg.ToName != "John Doe" {
		t.Errorf("unexpected ToName: %s", msg.ToName)
	}
	if msg.Subject != "Test Subject" {
		t.Errorf("unexpected Subject: %s", msg.Subject)
	}
	if msg.Body != "Plain text body" {
		t.Errorf("unexpected Body: %s", msg.Body)
	}
	if msg.HTML != "<p>HTML body</p>" {
		t.Errorf("unexpected HTML: %s", msg.HTML)
	}
}
