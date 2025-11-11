package tenancy

import (
	"context"
	"testing"
)

func TestWithOrgIDAndOrgIDFromContext(t *testing.T) {
	ctx := context.Background()
	ctx = WithOrgID(ctx, "org-123")

	got, ok := OrgIDFromContext(ctx)
	if !ok {
		t.Fatalf("expected org id to be present")
	}
	if got != "org-123" {
		t.Fatalf("expected org-123, got %s", got)
	}
}

func TestOrgIDFromContext_EmptyOrMissing(t *testing.T) {
	ctx := context.Background()
	if _, ok := OrgIDFromContext(ctx); ok {
		t.Fatalf("expected missing org id to return false")
	}

	ctx = context.WithValue(ctx, orgKey, 42)
	if _, ok := OrgIDFromContext(ctx); ok {
		t.Fatalf("expected non-string org id to return false")
	}

	ctx = WithOrgID(context.Background(), "")
	if _, ok := OrgIDFromContext(ctx); ok {
		t.Fatalf("expected empty org id to return false")
	}
}
