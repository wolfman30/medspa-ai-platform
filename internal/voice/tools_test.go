package voice

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
)

func TestToolHandlerSendDepositSMS_SendsExpectedContent(t *testing.T) {
	orgID := "org-tools-test"
	caller := "+15554443333"

	cfg := clinic.DefaultConfig(orgID)
	cfg.Name = "Glow Medspa"
	cfg.DepositAmountCents = 9000
	cfg.Phone = "+15550000001"
	cfg.SMSPhoneNumber = "+15550000002"

	store := setupClinicStore(t, cfg)
	messenger := newTestMessenger()

	h := NewToolHandler(orgID, caller, "+15557778888", &ToolDeps{
		Messenger:   messenger,
		ClinicStore: store,
	}, slog.Default())

	if err := h.SendDepositSMS(context.Background(), orgID, caller); err != nil {
		t.Fatalf("SendDepositSMS() error = %v", err)
	}

	if got := messenger.count(); got != 1 {
		t.Fatalf("expected 1 SMS send, got %d", got)
	}

	msg := messenger.firstReply()
	if msg.To != caller {
		t.Fatalf("To = %q, want %q", msg.To, caller)
	}
	if msg.From != "+15557778888" {
		t.Fatalf("From = %q, want called voice number %q", msg.From, "+15557778888")
	}
	if !strings.Contains(msg.Body, "Lauren from Glow Medspa") {
		t.Fatalf("expected clinic name in message body, got: %q", msg.Body)
	}
	if !strings.Contains(msg.Body, "$90") {
		t.Fatalf("expected deposit amount in message body, got: %q", msg.Body)
	}
	if !strings.Contains(msg.Body, "https://buy.stripe.com/") {
		t.Fatalf("expected Stripe deposit link in message body, got: %q", msg.Body)
	}
}

func TestToolHandlerSendDepositSMS_MissingClinicConfigReturnsError(t *testing.T) {
	h := NewToolHandler("org-tools-test", "+15551110000", "", &ToolDeps{
		Messenger: newTestMessenger(),
		// ClinicStore intentionally missing
	}, slog.Default())

	err := h.SendDepositSMS(context.Background(), "org-tools-test", "+15551110000")
	if err == nil {
		t.Fatal("expected error when clinic store is missing, got nil")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("expected not configured error, got: %v", err)
	}
}

func TestToolHandlerSendDepositSMS_FallsBackToMainPhoneWhenSMSPhoneMissing(t *testing.T) {
	orgID := "org-tools-fallback"
	caller := "+15556667777"

	cfg := clinic.DefaultConfig(orgID)
	cfg.Name = "Fallback Clinic"
	cfg.Phone = "+15559990000"
	cfg.SMSPhoneNumber = ""

	store := setupClinicStore(t, cfg)
	messenger := newTestMessenger()

	h := NewToolHandler(orgID, caller, "", &ToolDeps{
		Messenger:   messenger,
		ClinicStore: store,
	}, slog.Default())

	if err := h.SendDepositSMS(context.Background(), orgID, caller); err != nil {
		t.Fatalf("SendDepositSMS() error = %v", err)
	}

	msg := messenger.firstReply()
	if msg.From != cfg.Phone {
		t.Fatalf("From = %q, want fallback main phone %q", msg.From, cfg.Phone)
	}
}

func TestToolHandlerSendDepositSMS_PrefersSMSPhoneWhenAvailable(t *testing.T) {
	orgID := "org-tools-prefer-sms"
	caller := "+15558889999"

	cfg := clinic.DefaultConfig(orgID)
	cfg.Phone = "+15550001111"
	cfg.SMSPhoneNumber = "+15550002222"

	store := setupClinicStore(t, cfg)
	messenger := newTestMessenger()

	h := NewToolHandler(orgID, caller, "", &ToolDeps{
		Messenger:   messenger,
		ClinicStore: store,
	}, slog.Default())

	if err := h.SendDepositSMS(context.Background(), orgID, caller); err != nil {
		t.Fatalf("SendDepositSMS() error = %v", err)
	}

	msg := messenger.firstReply()
	if msg.From != cfg.SMSPhoneNumber {
		t.Fatalf("From = %q, want SMS phone %q", msg.From, cfg.SMSPhoneNumber)
	}
}

func TestToolHandlerCheckAvailability_ReturnsFallbackWithoutClinicStore(t *testing.T) {
	h := NewToolHandler("org-tools-check", "+15551112222", "", &ToolDeps{}, slog.Default())

	input, err := json.Marshal(map[string]string{"service": "Botox"})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	result, err := h.checkAvailability(context.Background(), input)
	if err != nil {
		t.Fatalf("checkAvailability() error = %v", err)
	}

	if !strings.Contains(result, "don't have access to the scheduling system") {
		t.Fatalf("checkAvailability() = %q, want fallback scheduling message", result)
	}
}

func TestToolHandlerCheckAvailability_ReturnsParseErrorForInvalidJSON(t *testing.T) {
	h := NewToolHandler("org-tools-check", "+15551112222", "", &ToolDeps{}, slog.Default())

	_, err := h.checkAvailability(context.Background(), json.RawMessage("{"))
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}

	if !strings.Contains(err.Error(), "parse input") {
		t.Fatalf("error = %v, want parse input error", err)
	}
}
