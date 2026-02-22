package payments

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestBillingHandler_HandleSubscribe_MethodNotAllowed(t *testing.T) {
	h := NewBillingHandler(BillingConfig{
		StripeSecretKey: "sk_test_xxx",
		StripePriceID:   "price_xxx",
		SuccessURL:      "https://example.com/welcome.html",
		CancelURL:       "https://example.com/pricing.html",
		Logger:          logging.New("error"),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/subscribe", nil)
	w := httptest.NewRecorder()
	h.HandleSubscribe(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestBillingWebhook_VerifySignature(t *testing.T) {
	h := &BillingWebhookHandler{
		webhookSecret: "whsec_test123",
		logger:        logging.New("error"),
	}

	// Empty sig should fail
	if h.verifySignature([]byte("test"), "") {
		t.Error("expected empty sig to fail")
	}

	// Malformed sig should fail
	if h.verifySignature([]byte("test"), "invalid") {
		t.Error("expected malformed sig to fail")
	}
}

func TestBillingWebhook_HandleUnknownEvent(t *testing.T) {
	h := &BillingWebhookHandler{
		webhookSecret: "",
		logger:        logging.New("error"),
	}

	event := `{"type":"unknown.event","data":{"object":{}}}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe-billing", strings.NewReader(event))
	w := httptest.NewRecorder()
	h.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]bool
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp["received"] {
		t.Error("expected received:true")
	}
}
