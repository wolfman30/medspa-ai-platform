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
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

var squareTracer = otel.Tracer("medspa.internal.payments.square")

// CredentialsProvider retrieves Square credentials for a specific org.
type CredentialsProvider interface {
	GetCredentials(ctx context.Context, orgID string) (*SquareCredentials, error)
}

// SquareCheckoutService creates hosted payment links for deposits.
type SquareCheckoutService struct {
	// Fallback credentials (used when no per-org credentials exist)
	accessToken string
	locationID  string
	successURL  string
	cancelURL   string
	baseURL     string
	httpClient  *http.Client
	logger      *logging.Logger
	// Per-org credentials provider (optional)
	credsProvider CredentialsProvider
}

type CheckoutParams struct {
	OrgID           string
	LeadID          string
	AmountCents     int32
	BookingIntentID uuid.UUID
	Description     string
	SuccessURL      string
	CancelURL       string
	ScheduledFor    *time.Time
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
	baseURL := "https://connect.squareup.com"
	return &SquareCheckoutService{
		accessToken: accessToken,
		locationID:  locationID,
		successURL:  successURL,
		cancelURL:   cancelURL,
		baseURL:     baseURL,
		httpClient:  client,
		logger:      logger,
	}
}

// WithBaseURL overrides the Square API host (e.g., sandbox).
func (s *SquareCheckoutService) WithBaseURL(baseURL string) *SquareCheckoutService {
	if baseURL == "" {
		return s
	}
	s.baseURL = strings.TrimRight(baseURL, "/")
	return s
}

// WithCredentialsProvider sets a per-org credentials provider.
func (s *SquareCheckoutService) WithCredentialsProvider(provider CredentialsProvider) *SquareCheckoutService {
	s.credsProvider = provider
	return s
}

// getCredentialsForOrg retrieves credentials for a specific org, falling back to default if not found.
func (s *SquareCheckoutService) getCredentialsForOrg(ctx context.Context, orgID string) (accessToken, locationID string, err error) {
	// Try per-org credentials first
	if s.credsProvider != nil && orgID != "" {
		creds, err := s.credsProvider.GetCredentials(ctx, orgID)
		if err == nil && creds != nil && creds.AccessToken != "" {
			locationID := creds.LocationID
			if locationID == "" {
				// If no location ID stored, we'd need to fetch it from Square
				// For now, log a warning
				s.logger.Warn("no location_id for org, payment may fail", "org_id", orgID)
			}
			return creds.AccessToken, locationID, nil
		}
		// Log but don't fail - fall through to default credentials
		if err != nil {
			s.logger.Debug("no per-org square credentials, using default", "org_id", orgID, "error", err)
		}
	}

	// Fall back to default credentials
	if s.accessToken == "" {
		return "", "", fmt.Errorf("payments: no square credentials configured")
	}
	return s.accessToken, s.locationID, nil
}

func (s *SquareCheckoutService) CreatePaymentLink(ctx context.Context, params CheckoutParams) (*CheckoutResponse, error) {
	// Get credentials for this org (or fallback to default)
	accessToken, locationID, err := s.getCredentialsForOrg(ctx, params.OrgID)
	if err != nil {
		return nil, err
	}
	if locationID == "" {
		return nil, fmt.Errorf("payments: no location_id configured for org %s", params.OrgID)
	}

	ctx, span := squareTracer.Start(ctx, "square.create_link")
	defer span.End()
	span.SetAttributes(
		attribute.String("medspa.org_id", params.OrgID),
		attribute.String("medspa.lead_id", params.LeadID),
		attribute.Int("medspa.amount_cents", int(params.AmountCents)),
	)

	successURL := params.SuccessURL
	if successURL == "" {
		successURL = s.successURL
	}

	idempotency := params.BookingIntentID.String()
	if params.BookingIntentID == uuid.Nil {
		idempotency = buildIdempotencyKey(params.OrgID, params.LeadID, params.AmountCents)
	}
	name := params.Description
	if strings.TrimSpace(name) == "" {
		name = "Deposit"
	}
	var scheduledStr string
	if params.ScheduledFor != nil {
		scheduledStr = params.ScheduledFor.UTC().Format(time.RFC3339)
	}
	meta := map[string]string{
		"org_id":            params.OrgID,
		"lead_id":           params.LeadID,
		"booking_intent_id": params.BookingIntentID.String(),
	}
	if scheduledStr != "" {
		meta["scheduled_for"] = scheduledStr
	}

	body := map[string]any{
		"idempotency_key": idempotency,
		"order": map[string]any{
			"location_id": locationID,
			"metadata":    meta,
			"line_items": []map[string]any{
				{
					"name":     name,
					"quantity": "1",
					"base_price_money": map[string]any{
						"amount":   params.AmountCents,
						"currency": "USD",
					},
				},
			},
		},
		"checkout_options": map[string]any{
			"redirect_url":             successURL,
			"ask_for_shipping_address": false,
		},
		// Redundant metadata on the link for completeness.
		"metadata": meta,
	}

	reqBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("payments: square payload: %w", err)
	}

	apiURL := s.baseURL + "/v2/online-checkout/payment-links"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("payments: square request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
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
	input := fmt.Sprintf("%s:%s:%d:%s", orgID, leadID, amount, time.Now().UTC().Format("2006-01-02T15"))
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}
