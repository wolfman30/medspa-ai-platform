package payments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

var squareTracer = otel.Tracer("medspa.internal.payments.square")

// SquareCheckoutService creates hosted payment links for deposits.
type SquareCheckoutService struct {
	accessToken string
	locationID  string
	successURL  string
	cancelURL   string
	httpClient  *http.Client
	logger      *logging.Logger
}

type CheckoutParams struct {
	OrgID           string
	LeadID          string
	AmountCents     int32
	BookingIntentID uuid.UUID
	Description     string
}

type CheckoutResponse struct {
	URL        string
	ProviderID string
}

func NewSquareCheckoutService(accessToken, locationID, successURL, cancelURL string, logger *logging.Logger) *SquareCheckoutService {
	if logger == nil {
		logger = logging.Default()
	}
	client := &http.Client{Timeout: 10 * time.Second}
	return &SquareCheckoutService{
		accessToken: accessToken,
		locationID:  locationID,
		successURL:  successURL,
		cancelURL:   cancelURL,
		httpClient:  client,
		logger:      logger,
	}
}

func (s *SquareCheckoutService) CreatePaymentLink(ctx context.Context, params CheckoutParams) (*CheckoutResponse, error) {
	if s.accessToken == "" || s.locationID == "" {
		return nil, fmt.Errorf("payments: square not configured")
	}
	ctx, span := squareTracer.Start(ctx, "square.create_link")
	defer span.End()
	span.SetAttributes(
		attribute.String("medspa.org_id", params.OrgID),
		attribute.String("medspa.lead_id", params.LeadID),
		attribute.Int("medspa.amount_cents", int(params.AmountCents)),
	)

	idempotency := buildIdempotencyKey(params.OrgID, params.LeadID, params.AmountCents)
	body := map[string]any{
		"idempotency_key": idempotency,
		"quick_pay": map[string]any{
			"name":        params.Description,
			"price_money": map[string]any{"amount": params.AmountCents, "currency": "USD"},
			"location_id": s.locationID,
		},
		"checkout_options": map[string]any{
			"redirect_url":             s.successURL,
			"ask_for_shipping_address": false,
		},
		"metadata": map[string]string{
			"org_id":            params.OrgID,
			"lead_id":           params.LeadID,
			"booking_intent_id": params.BookingIntentID.String(),
		},
	}

	reqBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("payments: square payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://connect.squareup.com/v2/online-checkout/payment-links", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("payments: square request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("payments: square http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("payments: square api status %d: %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		PaymentLink struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"payment_link"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("payments: square decode: %w", err)
	}
	if parsed.PaymentLink.URL == "" {
		return nil, fmt.Errorf("payments: square response missing url")
	}

	return &CheckoutResponse{
		URL:        parsed.PaymentLink.URL,
		ProviderID: parsed.PaymentLink.ID,
	}, nil
}

func buildIdempotencyKey(orgID, leadID string, amount int32) string {
	input := fmt.Sprintf("%s:%s:%d:%s", orgID, leadID, amount, time.Now().UTC().Format("2006-01-02"))
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}
