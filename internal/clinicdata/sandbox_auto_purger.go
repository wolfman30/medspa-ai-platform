package clinicdata

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

type AutoPurgeConfig struct {
	Enabled            bool
	AllowedPhoneDigits []string
	Delay              time.Duration
}

// SandboxAutoPurger performs best-effort purging of configured test numbers after successful sandbox payments.
// It is intended for dev/demo usage only.
type SandboxAutoPurger struct {
	purger  *Purger
	allowed map[string]struct{}
	delay   time.Duration
	enabled bool
	logger  *logging.Logger
}

func NewSandboxAutoPurger(purger *Purger, cfg AutoPurgeConfig, logger *logging.Logger) *SandboxAutoPurger {
	if logger == nil {
		logger = logging.Default()
	}
	allowed := make(map[string]struct{})
	for _, raw := range cfg.AllowedPhoneDigits {
		d := normalizeUSDigits(sanitizeDigits(raw))
		if d == "" {
			continue
		}
		allowed[d] = struct{}{}
	}
	return &SandboxAutoPurger{
		purger:  purger,
		allowed: allowed,
		delay:   cfg.Delay,
		enabled: cfg.Enabled && purger != nil && len(allowed) > 0,
		logger:  logger,
	}
}

// ParsePhoneDigitsList parses a comma-separated list of phone numbers into normalized digits.
func ParsePhoneDigitsList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, part := range strings.Split(raw, ",") {
		d := normalizeUSDigits(sanitizeDigits(part))
		if d == "" {
			continue
		}
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}
	return out
}

// MaybePurgeAfterPayment purges configured test data after a successful Square payment event.
// This is best-effort: errors are returned for logging, but callers should not fail the payment flow.
func (p *SandboxAutoPurger) MaybePurgeAfterPayment(ctx context.Context, evt events.PaymentSucceededV1) error {
	if p == nil || !p.enabled {
		return nil
	}
	if strings.ToLower(strings.TrimSpace(evt.Provider)) != "square" {
		return nil
	}
	digits := normalizeUSDigits(sanitizeDigits(evt.LeadPhone))
	if digits == "" {
		return nil
	}
	if _, ok := p.allowed[digits]; !ok {
		return nil
	}

	run := func() error {
		purgeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := p.purger.PurgePhone(purgeCtx, evt.OrgID, digits); err != nil {
			return err
		}
		p.logger.Info("sandbox auto purge completed", "org_id", evt.OrgID, "phone_last4", last4(digits), "provider_ref", evt.ProviderRef)
		return nil
	}

	if p.delay <= 0 {
		return run()
	}

	go func() {
		time.Sleep(p.delay)
		if err := run(); err != nil {
			p.logger.Warn("sandbox auto purge failed", "error", err, "org_id", evt.OrgID, "phone_last4", last4(digits), "provider_ref", evt.ProviderRef)
		}
	}()
	return nil
}

func last4(digits string) string {
	digits = strings.TrimSpace(digits)
	if len(digits) <= 4 {
		return digits
	}
	return fmt.Sprintf("...%s", digits[len(digits)-4:])
}
