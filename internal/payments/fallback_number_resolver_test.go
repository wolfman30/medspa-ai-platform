package payments

import "testing"

type fallbackStubResolver struct {
	number string
}

func (s fallbackStubResolver) DefaultFromNumber(string) string {
	return s.number
}

func TestFallbackNumberResolver_UsesPrimaryWhenSet(t *testing.T) {
	primary := fallbackStubResolver{number: "+15550001111"}
	resolver := NewFallbackNumberResolver(primary, "+15559998888")

	got := resolver.DefaultFromNumber("org-1")
	if got != "+15550001111" {
		t.Fatalf("expected primary number, got %q", got)
	}
}

func TestFallbackNumberResolver_UsesFallbackWhenPrimaryEmpty(t *testing.T) {
	primary := fallbackStubResolver{number: ""}
	resolver := NewFallbackNumberResolver(primary, "+15559998888")

	got := resolver.DefaultFromNumber("org-1")
	if got != "+15559998888" {
		t.Fatalf("expected fallback number, got %q", got)
	}
}
