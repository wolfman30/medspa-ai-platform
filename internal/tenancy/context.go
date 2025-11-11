package tenancy

import "context"

type ctxKey string

const orgKey ctxKey = "medspa.org_id"

// WithOrgID stores the org id in context.
func WithOrgID(ctx context.Context, orgID string) context.Context {
	return context.WithValue(ctx, orgKey, orgID)
}

// OrgIDFromContext extracts the org id if present.
func OrgIDFromContext(ctx context.Context) (string, bool) {
	val := ctx.Value(orgKey)
	if val == nil {
		return "", false
	}
	orgID, ok := val.(string)
	return orgID, ok && orgID != ""
}
