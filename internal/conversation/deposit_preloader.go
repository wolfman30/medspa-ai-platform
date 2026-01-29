package conversation

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/wolfman30/medspa-ai-platform/internal/payments"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// Regex patterns for detecting deposit agreement in user messages
var (
	preloadDepositAffirmativeRE = regexp.MustCompile(`(?i)(?:\b(?:yes|yeah|yea|sure|ok|okay|absolutely|definitely|proceed)\b|let'?s do it|i'?ll pay|i will pay|happy to pay|i can pay|ready to pay)`)
	preloadDepositKeywordRE     = regexp.MustCompile(`(?i)(?:\b(?:deposit|payment)\b|\bpay\b|secure (?:my|the|your) (?:spot|appointment)|hold (?:my|the|your) (?:spot|appointment)|confirm (?:my|the) (?:appointment|booking))`)
	preloadDepositNegativeRE    = regexp.MustCompile(`(?i)(?:no deposit|don'?t want|do not want|not paying|not now|maybe(?: later)?|later|skip|no thanks|nope|not ready)`)
)

// PreGeneratedCheckout holds a pre-generated Square checkout link
type PreGeneratedCheckout struct {
	URL          string
	ProviderID   string
	PrePaymentID uuid.UUID
	AmountCents  int32
	GeneratedAt  time.Time
	Error        error
}

// DepositPreloader manages parallel checkout link generation
type DepositPreloader struct {
	checkout      paymentLinkCreator
	logger        *logging.Logger
	defaultAmount int32

	// Cache of pre-generated links keyed by conversation ID
	cache sync.Map
}

// NewDepositPreloader creates a preloader for parallel deposit link generation
func NewDepositPreloader(checkout paymentLinkCreator, defaultAmount int32, logger *logging.Logger) *DepositPreloader {
	if logger == nil {
		logger = logging.Default()
	}
	return &DepositPreloader{
		checkout:      checkout,
		logger:        logger,
		defaultAmount: defaultAmount,
	}
}

// ShouldPreloadDeposit checks if the message looks like a deposit agreement
// and returns true if we should start pre-generating a checkout link.
func ShouldPreloadDeposit(message string) bool {
	message = strings.TrimSpace(message)
	if message == "" {
		return false
	}

	// Don't preload if message contains negative indicators
	if preloadDepositNegativeRE.MatchString(message) {
		return false
	}

	// Must have affirmative language
	if !preloadDepositAffirmativeRE.MatchString(message) {
		return false
	}

	// Must mention deposit/payment keywords
	if !preloadDepositKeywordRE.MatchString(message) {
		return false
	}

	return true
}

// StartPreload begins generating a checkout link in the background.
// Returns immediately; use GetPreloaded to retrieve the result.
func (p *DepositPreloader) StartPreload(ctx context.Context, conversationID, orgID, leadID, fromNumber string) {
	if p.checkout == nil {
		return
	}

	// Generate a payment ID upfront that we'll use for both the link and later the payment intent
	prePaymentID := uuid.New()

	p.logger.Info("deposit preloader: starting parallel checkout generation",
		"conversation_id", conversationID,
		"pre_payment_id", prePaymentID,
	)

	// Store a pending marker
	p.cache.Store(conversationID, &PreGeneratedCheckout{
		PrePaymentID: prePaymentID,
		AmountCents:  p.defaultAmount,
		GeneratedAt:  time.Now(),
	})

	go func() {
		// Use a fresh context with timeout since the parent may complete
		linkCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result := &PreGeneratedCheckout{
			PrePaymentID: prePaymentID,
			AmountCents:  p.defaultAmount,
			GeneratedAt:  time.Now(),
		}

		link, err := p.checkout.CreatePaymentLink(linkCtx, payments.CheckoutParams{
			OrgID:           orgID,
			LeadID:          leadID,
			AmountCents:     p.defaultAmount,
			BookingIntentID: prePaymentID,
			Description:     "Appointment deposit",
			FromNumber:      fromNumber,
		})

		if err != nil {
			result.Error = err
			p.logger.Warn("deposit preloader: checkout generation failed",
				"conversation_id", conversationID,
				"error", err,
			)
		} else {
			result.URL = link.URL
			result.ProviderID = link.ProviderID
			p.logger.Info("deposit preloader: checkout link ready",
				"conversation_id", conversationID,
				"pre_payment_id", prePaymentID,
				"latency_ms", time.Since(result.GeneratedAt).Milliseconds(),
			)
		}

		p.cache.Store(conversationID, result)
	}()
}

// GetPreloaded retrieves a pre-generated checkout link if available.
// Returns nil if no preload was started or if it hasn't completed.
func (p *DepositPreloader) GetPreloaded(conversationID string) *PreGeneratedCheckout {
	if val, ok := p.cache.Load(conversationID); ok {
		result := val.(*PreGeneratedCheckout)
		// Only return if link generation completed (has URL or error)
		if result.URL != "" || result.Error != nil {
			return result
		}
	}
	return nil
}

// WaitForPreloaded waits for a pre-generated checkout link with timeout.
// Returns the result or nil if timeout expires.
func (p *DepositPreloader) WaitForPreloaded(conversationID string, timeout time.Duration) *PreGeneratedCheckout {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if result := p.GetPreloaded(conversationID); result != nil {
			return result
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

// ClearPreloaded removes a pre-generated checkout from the cache.
func (p *DepositPreloader) ClearPreloaded(conversationID string) {
	p.cache.Delete(conversationID)
}
