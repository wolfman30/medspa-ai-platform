package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

var stripeTracer = otel.Tracer("medspa.internal.payments.stripe")

// StripeCheckoutService creates Stripe Checkout Sessions for deposit collection
// via Stripe Connect. Deposits go directly to the clinic's connected Stripe account.
type StripeCheckoutService struct {
	secretKey  string
	successURL string
	cancelURL  string
	baseURL    string
	apiVersion string
	httpClient *http.Client
	logger     *logging.Logger
	dryRun     bool

	// clinicAccountResolver looks up the connected Stripe account ID for an org.
	clinicAccountResolver StripeAccountResolver
}

// StripeAccountResolver returns the connected Stripe account ID for an org.
type StripeAccountResolver interface {
	GetStripeAccountID(ctx context.Context, orgID string) (string, error)
}

// NewStripeCheckoutService creates a new Stripe checkout service.
func NewStripeCheckoutService(secretKey, successURL, cancelURL string, logger *logging.Logger) *StripeCheckoutService {
	if logger == nil {
		logger = logging.Default()
	}
	dryRun := strings.EqualFold(os.Getenv("STRIPE_DRY_RUN"), "true") || os.Getenv("STRIPE_DRY_RUN") == "1"
	return &StripeCheckoutService{
		secretKey:  secretKey,
		successURL: successURL,
		cancelURL:  cancelURL,
		baseURL:    "https://api.stripe.com",
		apiVersion: "2024-12-18.acacia",
		httpClient: &http.Client{Timeout: 10 * time.Second},
		logger:     logger,
		dryRun:     dryRun,
	}
}

// WithBaseURL overrides the Stripe API base URL (for testing).
func (s *StripeCheckoutService) WithBaseURL(baseURL string) *StripeCheckoutService {
	if baseURL != "" {
		s.baseURL = strings.TrimRight(baseURL, "/")
	}
	return s
}

// WithDryRun enables dry-run mode (returns fake URLs without calling Stripe).
func (s *StripeCheckoutService) WithDryRun(enabled bool) *StripeCheckoutService {
	s.dryRun = enabled
	return s
}

// WithAccountResolver sets the resolver for looking up connected account IDs.
func (s *StripeCheckoutService) WithAccountResolver(resolver StripeAccountResolver) *StripeCheckoutService {
	s.clinicAccountResolver = resolver
	return s
}

// CreatePaymentLink implements checkoutLinkCreator for Stripe.
func (s *StripeCheckoutService) CreatePaymentLink(ctx context.Context, params CheckoutParams) (*CheckoutResponse, error) {
	ctx, span := stripeTracer.Start(ctx, "stripe.create_checkout_session")
	defer span.End()
	span.SetAttributes(
		attribute.String("medspa.org_id", params.OrgID),
		attribute.String("medspa.lead_id", params.LeadID),
		attribute.Int("medspa.amount_cents", int(params.AmountCents)),
	)

	if s.dryRun {
		fakeID := "cs_dryrun_" + uuid.New().String()[:8]
		s.logger.Info("stripe dry run: skipping checkout session creation",
			"org_id", params.OrgID, "lead_id", params.LeadID, "amount_cents", params.AmountCents)
		return &CheckoutResponse{
			URL:        fmt.Sprintf("https://checkout.stripe.com/dry-run/%s", fakeID),
			ProviderID: fakeID,
		}, nil
	}

	// Resolve connected account for this org
	var connectedAccountID string
	if s.clinicAccountResolver != nil && params.OrgID != "" {
		acctID, err := s.clinicAccountResolver.GetStripeAccountID(ctx, params.OrgID)
		if err != nil {
			return nil, fmt.Errorf("payments: stripe account lookup for org %s: %w", params.OrgID, err)
		}
		connectedAccountID = acctID
	}

	successURL := params.SuccessURL
	if successURL == "" {
		successURL = s.successURL
	}
	cancelURL := params.CancelURL
	if cancelURL == "" {
		cancelURL = s.cancelURL
	}

	description := params.Description
	if strings.TrimSpace(description) == "" {
		description = "Deposit"
	}

	var scheduledStr string
	if params.ScheduledFor != nil {
		scheduledStr = params.ScheduledFor.UTC().Format(time.RFC3339)
	}

	// Build form-encoded body for Stripe API
	form := url.Values{}
	form.Set("mode", "payment")
	form.Set("line_items[0][price_data][currency]", "usd")
	form.Set("line_items[0][price_data][unit_amount]", fmt.Sprintf("%d", params.AmountCents))
	form.Set("line_items[0][price_data][product_data][name]", description)
	form.Set("line_items[0][quantity]", "1")

	if successURL != "" {
		form.Set("success_url", successURL)
	}
	if cancelURL != "" {
		form.Set("cancel_url", cancelURL)
	}

	// Metadata for webhook processing
	form.Set("metadata[org_id]", params.OrgID)
	form.Set("metadata[lead_id]", params.LeadID)
	form.Set("metadata[booking_intent_id]", params.BookingIntentID.String())
	if scheduledStr != "" {
		form.Set("metadata[scheduled_for]", scheduledStr)
	}
	if fromNumber := strings.TrimSpace(params.FromNumber); fromNumber != "" {
		form.Set("metadata[from_number]", fromNumber)
	}

	// Also set metadata on the payment intent so it's accessible from payment objects
	form.Set("payment_intent_data[metadata][org_id]", params.OrgID)
	form.Set("payment_intent_data[metadata][lead_id]", params.LeadID)
	form.Set("payment_intent_data[metadata][booking_intent_id]", params.BookingIntentID.String())
	if scheduledStr != "" {
		form.Set("payment_intent_data[metadata][scheduled_for]", scheduledStr)
	}
	if fromNumber := strings.TrimSpace(params.FromNumber); fromNumber != "" {
		form.Set("payment_intent_data[metadata][from_number]", fromNumber)
	}

	// Transfer to connected account (Stripe Connect)
	if connectedAccountID != "" {
		form.Set("payment_intent_data[transfer_data][destination]", connectedAccountID)
	}

	apiURL := s.baseURL + "/v1/checkout/sessions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("payments: stripe request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.secretKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Stripe-Version", s.apiVersion)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("payments: stripe http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("payments: stripe api status %d: %s", resp.StatusCode, string(body))
	}

	var parsed stripeCheckoutSession
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("payments: stripe decode: %w", err)
	}
	if parsed.URL == "" {
		return nil, fmt.Errorf("payments: stripe response missing checkout url")
	}

	return &CheckoutResponse{
		URL:        parsed.URL,
		ProviderID: parsed.ID,
	}, nil
}

// stripeCheckoutSession is the subset of Stripe's Checkout Session we need.
type stripeCheckoutSession struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// stripeErrorResponse represents a Stripe API error.
type stripeErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// readStripeError reads and parses a Stripe error response body.
func readStripeError(body io.Reader) string {
	data, err := io.ReadAll(body)
	if err != nil {
		return "unknown error"
	}
	var buf bytes.Buffer
	if json.Indent(&buf, data, "", "  ") == nil {
		return buf.String()
	}
	return string(data)
}
