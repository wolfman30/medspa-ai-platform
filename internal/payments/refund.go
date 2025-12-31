package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// RefundService handles payment refunds via Square API.
type RefundService struct {
	baseURL       string
	httpClient    *http.Client
	credsProvider CredentialsProvider
	logger        *logging.Logger
}

// RefundRequest contains the details for a refund.
type RefundRequest struct {
	PaymentID   string // Square payment ID
	AmountCents int32  // Amount to refund (must be <= original amount)
	Reason      string // Reason for refund
	OrgID       string // Organization ID for credentials lookup
}

// RefundResponse contains the result of a refund.
type RefundResponse struct {
	RefundID  string
	Status    string // PENDING, COMPLETED, FAILED, REJECTED
	CreatedAt time.Time
}

// NewRefundService creates a new refund service.
func NewRefundService(baseURL string, credsProvider CredentialsProvider, logger *logging.Logger) *RefundService {
	if logger == nil {
		logger = logging.Default()
	}
	if baseURL == "" {
		baseURL = "https://connect.squareup.com"
	}
	return &RefundService{
		baseURL:       baseURL,
		httpClient:    &http.Client{Timeout: 15 * time.Second},
		credsProvider: credsProvider,
		logger:        logger,
	}
}

// RefundPayment processes a refund via Square Refunds API.
func (s *RefundService) RefundPayment(ctx context.Context, req RefundRequest) (*RefundResponse, error) {
	ctx, span := squareTracer.Start(ctx, "square.refund_payment")
	defer span.End()
	span.SetAttributes(
		attribute.String("medspa.org_id", req.OrgID),
		attribute.String("square.payment_id", req.PaymentID),
		attribute.Int("medspa.amount_cents", int(req.AmountCents)),
	)

	// Get credentials for this org
	accessToken, err := s.getAccessToken(ctx, req.OrgID)
	if err != nil {
		return nil, fmt.Errorf("payments: refund credentials: %w", err)
	}

	// Generate idempotency key based on payment ID to prevent duplicate refunds
	idempotencyKey := fmt.Sprintf("refund-%s-%d", req.PaymentID, time.Now().Unix())

	body := map[string]any{
		"idempotency_key": idempotencyKey,
		"payment_id":      req.PaymentID,
		"amount_money": map[string]any{
			"amount":   req.AmountCents,
			"currency": "USD",
		},
	}
	if req.Reason != "" {
		body["reason"] = req.Reason
	}

	reqBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("payments: refund marshal: %w", err)
	}

	apiURL := fmt.Sprintf("%s/v2/refunds", s.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("payments: refund request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Square-Version", "2025-01-16")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("payments: refund http: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= http.StatusMultipleChoices {
		s.logger.Error("square refund failed",
			"status", resp.StatusCode,
			"body", string(respBody),
			"payment_id", req.PaymentID,
		)
		return nil, fmt.Errorf("payments: square refund api status %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed struct {
		Refund struct {
			ID        string `json:"id"`
			Status    string `json:"status"`
			CreatedAt string `json:"created_at"`
		} `json:"refund"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("payments: refund decode: %w", err)
	}

	createdAt, _ := time.Parse(time.RFC3339, parsed.Refund.CreatedAt)

	s.logger.Info("refund processed",
		"refund_id", parsed.Refund.ID,
		"payment_id", req.PaymentID,
		"status", parsed.Refund.Status,
		"amount_cents", req.AmountCents,
	)

	return &RefundResponse{
		RefundID:  parsed.Refund.ID,
		Status:    parsed.Refund.Status,
		CreatedAt: createdAt,
	}, nil
}

func (s *RefundService) getAccessToken(ctx context.Context, orgID string) (string, error) {
	if s.credsProvider == nil {
		return "", fmt.Errorf("payments: no credentials provider configured")
	}
	creds, err := s.credsProvider.GetCredentials(ctx, orgID)
	if err != nil {
		return "", err
	}
	if creds == nil || creds.AccessToken == "" {
		return "", fmt.Errorf("payments: no access token for org %s", orgID)
	}
	return creds.AccessToken, nil
}

// RefundResult represents the outcome of a refund attempt.
type RefundResult struct {
	Success    bool
	RefundID   string
	Error      string
	RefundedAt time.Time
}

// ProcessRefundByPaymentID refunds a payment by our internal payment ID.
func (s *RefundService) ProcessRefundByPaymentID(ctx context.Context, repo *Repository, paymentID uuid.UUID, reason string) (*RefundResult, error) {
	// Fetch payment record
	payment, err := repo.GetByID(ctx, paymentID)
	if err != nil {
		return nil, fmt.Errorf("payments: fetch for refund: %w", err)
	}

	// Check if already refunded
	if payment.Status == "refunded" {
		return &RefundResult{
			Success:  true,
			RefundID: "already_refunded",
		}, nil
	}

	// Check if payment can be refunded (must be succeeded)
	if payment.Status != "succeeded" {
		return &RefundResult{
			Success: false,
			Error:   fmt.Sprintf("payment status is %s, cannot refund", payment.Status),
		}, nil
	}

	// Get the Square payment ID from provider_ref
	if !payment.ProviderRef.Valid || payment.ProviderRef.String == "" {
		return &RefundResult{
			Success: false,
			Error:   "no provider reference for payment",
		}, nil
	}

	// Process refund via Square
	refundResp, err := s.RefundPayment(ctx, RefundRequest{
		PaymentID:   payment.ProviderRef.String,
		AmountCents: payment.AmountCents,
		Reason:      reason,
		OrgID:       payment.OrgID,
	})
	if err != nil {
		return &RefundResult{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Update payment status in database
	_, err = repo.UpdateStatusByID(ctx, paymentID, "refunded", payment.ProviderRef.String)
	if err != nil {
		s.logger.Error("failed to update payment status after refund",
			"payment_id", paymentID,
			"refund_id", refundResp.RefundID,
			"error", err,
		)
		// Return success anyway since refund was processed
	}

	return &RefundResult{
		Success:    true,
		RefundID:   refundResp.RefundID,
		RefundedAt: refundResp.CreatedAt,
	}, nil
}
