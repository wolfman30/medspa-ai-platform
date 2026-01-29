package payments

import "strings"

// UsePaymentLinks determines whether to use Square Payment Links based on a mode string.
// Modes:
// - "payment_links" (or "payment-links"): always use payment links
// - "legacy" (or "checkout"): use legacy checkout
// - "auto" or empty: use payment links in sandbox, legacy in production
func UsePaymentLinks(mode string, sandbox bool) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "payment_links", "payment-links", "paymentlinks":
		return true
	case "legacy", "checkout":
		return false
	case "auto", "":
		return sandbox
	default:
		return sandbox
	}
}
