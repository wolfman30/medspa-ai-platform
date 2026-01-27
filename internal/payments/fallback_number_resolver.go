package payments

import "strings"

// FallbackNumberResolver uses a primary resolver first, then falls back to a configured default number.
type FallbackNumberResolver struct {
	primary  OrgNumberResolver
	fallback string
}

// NewFallbackNumberResolver wraps a primary resolver with a default fallback number.
// If fallback is empty, the primary resolver is returned unchanged.
func NewFallbackNumberResolver(primary OrgNumberResolver, fallback string) OrgNumberResolver {
	fallback = strings.TrimSpace(fallback)
	if fallback == "" {
		return primary
	}
	return &FallbackNumberResolver{
		primary:  primary,
		fallback: fallback,
	}
}

// DefaultFromNumber returns the primary resolver number when available,
// otherwise it returns the fallback number.
func (r *FallbackNumberResolver) DefaultFromNumber(orgID string) string {
	if r == nil {
		return ""
	}
	if r.primary != nil {
		if number := strings.TrimSpace(r.primary.DefaultFromNumber(orgID)); number != "" {
			return number
		}
	}
	return r.fallback
}
