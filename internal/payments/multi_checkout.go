package payments

import (
	"context"
	"fmt"
	"strings"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// CheckoutProvider creates payment links for a specific provider.
type CheckoutProvider interface {
	CreatePaymentLink(ctx context.Context, params CheckoutParams) (*CheckoutResponse, error)
}

// MultiCheckoutService delegates to the correct checkout provider based on clinic config.
type MultiCheckoutService struct {
	square      CheckoutProvider
	stripe      CheckoutProvider
	clinicStore *clinic.Store
	logger      *logging.Logger
}

// NewMultiCheckoutService creates a checkout service that routes to Square or Stripe
// based on the clinic's PaymentProvider configuration.
func NewMultiCheckoutService(square, stripe CheckoutProvider, clinicStore *clinic.Store, logger *logging.Logger) *MultiCheckoutService {
	if logger == nil {
		logger = logging.Default()
	}
	return &MultiCheckoutService{
		square:      square,
		stripe:      stripe,
		clinicStore: clinicStore,
		logger:      logger,
	}
}

// CreatePaymentLink looks up the clinic's payment provider and delegates accordingly.
func (m *MultiCheckoutService) CreatePaymentLink(ctx context.Context, params CheckoutParams) (*CheckoutResponse, error) {
	if m.clinicStore == nil {
		// No clinic store — fall back to whatever is available
		if m.square != nil {
			return m.square.CreatePaymentLink(ctx, params)
		}
		if m.stripe != nil {
			return m.stripe.CreatePaymentLink(ctx, params)
		}
		return nil, fmt.Errorf("no checkout provider configured")
	}

	cfg, err := m.clinicStore.Get(ctx, params.OrgID)
	if err != nil {
		m.logger.Warn("multi_checkout: failed to get clinic config, using default provider", "org_id", params.OrgID, "error", err)
		return m.defaultProvider(ctx, params)
	}

	provider := strings.ToLower(cfg.PaymentProvider)
	switch provider {
	case "stripe":
		if m.stripe == nil {
			return nil, fmt.Errorf("clinic %s configured for stripe but stripe not available", params.OrgID)
		}
		// Set the connected account ID on the params for Stripe Connect
		if cfg.StripeAccountID != "" {
			params.StripeAccountID = cfg.StripeAccountID
		}
		m.logger.Debug("multi_checkout: using stripe", "org_id", params.OrgID)
		return m.stripe.CreatePaymentLink(ctx, params)
	default:
		if m.square == nil {
			if m.stripe != nil {
				return m.stripe.CreatePaymentLink(ctx, params)
			}
			return nil, fmt.Errorf("clinic %s configured for square but square not available", params.OrgID)
		}
		return m.square.CreatePaymentLink(ctx, params)
	}
}

func (m *MultiCheckoutService) defaultProvider(ctx context.Context, params CheckoutParams) (*CheckoutResponse, error) {
	if m.square != nil {
		return m.square.CreatePaymentLink(ctx, params)
	}
	if m.stripe != nil {
		return m.stripe.CreatePaymentLink(ctx, params)
	}
	return nil, fmt.Errorf("no checkout provider configured")
}
