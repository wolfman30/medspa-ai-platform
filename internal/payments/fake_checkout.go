package payments

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// FakeCheckoutService is a dev/demo checkout provider that generates an internal URL
// and lets the user "complete" the deposit without Square credentials.
//
// This MUST be gated by configuration (e.g. ALLOW_FAKE_PAYMENTS) and should never be
// enabled in production.
type FakeCheckoutService struct {
	publicBaseURL string
	logger        *logging.Logger
}

func NewFakeCheckoutService(publicBaseURL string, logger *logging.Logger) *FakeCheckoutService {
	if logger == nil {
		logger = logging.Default()
	}
	return &FakeCheckoutService{
		publicBaseURL: strings.TrimRight(strings.TrimSpace(publicBaseURL), "/"),
		logger:        logger,
	}
}

func (s *FakeCheckoutService) CreatePaymentLink(ctx context.Context, params CheckoutParams) (*CheckoutResponse, error) {
	_ = ctx
	if params.BookingIntentID == uuid.Nil {
		return nil, fmt.Errorf("payments: fake checkout requires booking intent id")
	}
	if s.publicBaseURL == "" {
		return nil, fmt.Errorf("payments: fake checkout requires PUBLIC_BASE_URL")
	}
	if !isValidBaseURL(s.publicBaseURL) {
		return nil, fmt.Errorf("payments: fake checkout PUBLIC_BASE_URL must be an absolute http(s) URL")
	}

	checkoutURL := fmt.Sprintf("%s/payments/fake/%s", s.publicBaseURL, params.BookingIntentID)
	return &CheckoutResponse{
		URL:        checkoutURL,
		ProviderID: "fake:" + params.BookingIntentID.String(),
	}, nil
}

func isValidBaseURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return true
	default:
		return false
	}
}

