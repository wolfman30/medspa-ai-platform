package payments

import (
	"context"
)

// PhoneNumberLookup provides phone number lookup for orgs.
type PhoneNumberLookup interface {
	GetPhoneNumber(ctx context.Context, orgID string) (string, error)
}

// DBOrgNumberResolver resolves org phone numbers from the database,
// with fallback to a static resolver.
type DBOrgNumberResolver struct {
	lookup   PhoneNumberLookup
	fallback OrgNumberResolver
}

// NewDBOrgNumberResolver creates a resolver that looks up phone numbers from the database.
// If lookup returns empty or fails, it falls back to the provided static resolver.
func NewDBOrgNumberResolver(lookup PhoneNumberLookup, fallback OrgNumberResolver) *DBOrgNumberResolver {
	return &DBOrgNumberResolver{
		lookup:   lookup,
		fallback: fallback,
	}
}

// DefaultFromNumber returns the phone number for sending SMS to the org.
// First checks the database, then falls back to the static mapping.
func (r *DBOrgNumberResolver) DefaultFromNumber(orgID string) string {
	if r.lookup != nil {
		ctx := context.Background()
		phone, err := r.lookup.GetPhoneNumber(ctx, orgID)
		if err == nil && phone != "" {
			return phone
		}
	}

	// Fallback to static resolver
	if r.fallback != nil {
		return r.fallback.DefaultFromNumber(orgID)
	}

	return ""
}
