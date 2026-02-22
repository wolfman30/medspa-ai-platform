package payments

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// BillingHandler creates Stripe Checkout Sessions for SaaS subscriptions.
// This is for AI Wolf Solutions' own billing, NOT Stripe Connect.
type BillingHandler struct {
	secretKey  string
	priceID    string // Stripe Price ID for $497/mo
	successURL string
	cancelURL  string
	httpClient *http.Client
	logger     *logging.Logger
	db         *sql.DB
}

// BillingConfig holds configuration for the billing handler.
type BillingConfig struct {
	StripeSecretKey string
	StripePriceID   string // pre-created Price for $497/mo recurring
	SuccessURL      string
	CancelURL       string
	Logger          *logging.Logger
	DB              *sql.DB
}

// NewBillingHandler creates a new billing handler.
func NewBillingHandler(cfg BillingConfig) *BillingHandler {
	return &BillingHandler{
		secretKey:  cfg.StripeSecretKey,
		priceID:    cfg.StripePriceID,
		successURL: cfg.SuccessURL,
		cancelURL:  cfg.CancelURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		logger:     cfg.Logger,
		db:         cfg.DB,
	}
}

// HandleSubscribe creates a Stripe Checkout Session for a new subscription.
func (h *BillingHandler) HandleSubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionURL, err := h.createCheckoutSession(r.Context())
	if err != nil {
		h.logger.Error("billing: failed to create checkout session", "error", err)
		http.Error(w, `{"error":"failed to create checkout session"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": sessionURL})
}

func (h *BillingHandler) createCheckoutSession(ctx context.Context) (string, error) {
	params := fmt.Sprintf(
		"mode=subscription&success_url=%s&cancel_url=%s&line_items[0][price]=%s&line_items[0][quantity]=1&allow_promotion_codes=true",
		h.successURL, h.cancelURL, h.priceID,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.stripe.com/v1/checkout/sessions",
		strings.NewReader(params))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.SetBasicAuth(h.secretKey, "")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("stripe request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("stripe returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	return result.URL, nil
}

// BillingWebhookHandler handles Stripe webhook events for subscription lifecycle.
type BillingWebhookHandler struct {
	webhookSecret string
	db            *sql.DB
	logger        *logging.Logger
}

// NewBillingWebhookHandler creates a new webhook handler.
func NewBillingWebhookHandler(db *sql.DB, logger *logging.Logger) *BillingWebhookHandler {
	return &BillingWebhookHandler{
		webhookSecret: os.Getenv("STRIPE_BILLING_WEBHOOK_SECRET"),
		db:            db,
		logger:        logger,
	}
}

// Handle processes incoming Stripe billing webhook events.
func (h *BillingWebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 65536))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	if h.webhookSecret != "" {
		sig := r.Header.Get("Stripe-Signature")
		if !h.verifySignature(body, sig) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	var event struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	var obj struct {
		Object json.RawMessage `json:"object"`
	}
	json.Unmarshal(event.Data, &obj)

	switch event.Type {
	case "checkout.session.completed":
		h.handleCheckoutCompleted(r.Context(), obj.Object)
	case "invoice.payment_succeeded":
		h.handlePaymentSucceeded(r.Context(), obj.Object)
	case "invoice.payment_failed":
		h.handlePaymentFailed(r.Context(), obj.Object)
	case "customer.subscription.deleted":
		h.handleSubscriptionCancelled(r.Context(), obj.Object)
	default:
		h.logger.Info("billing webhook: unhandled event", "type", event.Type)
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"received":true}`))
}

func (h *BillingWebhookHandler) verifySignature(payload []byte, sigHeader string) bool {
	if sigHeader == "" || h.webhookSecret == "" {
		return false
	}
	// Parse t= and v1= from header
	var timestamp, sig string
	for _, part := range strings.Split(sigHeader, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			timestamp = kv[1]
		case "v1":
			sig = kv[1]
		}
	}
	if timestamp == "" || sig == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(h.webhookSecret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
}

func (h *BillingWebhookHandler) handleCheckoutCompleted(ctx context.Context, data json.RawMessage) {
	var session struct {
		ID              string `json:"id"`
		CustomerID      string `json:"customer"`
		Subscription    string `json:"subscription"`
		CustomerDetails struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"customer_details"`
	}
	if err := json.Unmarshal(data, &session); err != nil {
		h.logger.Error("billing: parse checkout session", "error", err)
		return
	}

	h.logger.Info("billing: checkout completed",
		"session_id", session.ID,
		"customer", session.CustomerID,
		"subscription", session.Subscription,
		"email", session.CustomerDetails.Email,
	)

	if h.db == nil {
		return
	}

	_, err := h.db.ExecContext(ctx, `
		INSERT INTO subscriptions (stripe_customer_id, stripe_subscription_id, email, customer_name, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'active', NOW(), NOW())
		ON CONFLICT (stripe_subscription_id) DO UPDATE SET
			status = 'active',
			email = EXCLUDED.email,
			customer_name = EXCLUDED.customer_name,
			updated_at = NOW()
	`, session.CustomerID, session.Subscription, session.CustomerDetails.Email, session.CustomerDetails.Name)
	if err != nil {
		h.logger.Error("billing: insert subscription", "error", err)
	}
}

func (h *BillingWebhookHandler) handlePaymentSucceeded(ctx context.Context, data json.RawMessage) {
	var invoice struct {
		Customer     string `json:"customer"`
		Subscription string `json:"subscription"`
	}
	if err := json.Unmarshal(data, &invoice); err != nil {
		return
	}
	h.logger.Info("billing: payment succeeded", "customer", invoice.Customer, "subscription", invoice.Subscription)

	if h.db == nil {
		return
	}
	h.db.ExecContext(ctx, `
		UPDATE subscriptions SET status = 'active', updated_at = NOW()
		WHERE stripe_subscription_id = $1
	`, invoice.Subscription)
}

func (h *BillingWebhookHandler) handlePaymentFailed(ctx context.Context, data json.RawMessage) {
	var invoice struct {
		Customer     string `json:"customer"`
		Subscription string `json:"subscription"`
	}
	if err := json.Unmarshal(data, &invoice); err != nil {
		return
	}
	h.logger.Error("billing: payment failed", "customer", invoice.Customer, "subscription", invoice.Subscription)

	if h.db == nil {
		return
	}
	h.db.ExecContext(ctx, `
		UPDATE subscriptions SET status = 'past_due', updated_at = NOW()
		WHERE stripe_subscription_id = $1
	`, invoice.Subscription)
}

func (h *BillingWebhookHandler) handleSubscriptionCancelled(ctx context.Context, data json.RawMessage) {
	var sub struct {
		ID       string `json:"id"`
		Customer string `json:"customer"`
	}
	if err := json.Unmarshal(data, &sub); err != nil {
		return
	}
	h.logger.Info("billing: subscription cancelled", "subscription", sub.ID, "customer", sub.Customer)

	if h.db == nil {
		return
	}
	h.db.ExecContext(ctx, `
		UPDATE subscriptions SET status = 'cancelled', updated_at = NOW()
		WHERE stripe_subscription_id = $1
	`, sub.ID)
}
