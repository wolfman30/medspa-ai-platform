package payments

import (
	"context"
	"errors"
	"testing"
)

type stubPhoneLookup struct {
	phones map[string]string
	err    error
}

func (s *stubPhoneLookup) GetPhoneNumber(ctx context.Context, orgID string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.phones[orgID], nil
}

type stubFallbackResolver struct {
	numbers map[string]string
}

func (s *stubFallbackResolver) DefaultFromNumber(orgID string) string {
	if s == nil {
		return ""
	}
	return s.numbers[orgID]
}

func TestDBOrgNumberResolver_LookupSuccess(t *testing.T) {
	lookup := &stubPhoneLookup{
		phones: map[string]string{
			"org-1": "+15551234567",
		},
	}
	fallback := &stubFallbackResolver{
		numbers: map[string]string{
			"org-1": "+19999999999", // different number
		},
	}

	resolver := NewDBOrgNumberResolver(lookup, fallback)
	got := resolver.DefaultFromNumber("org-1")

	if got != "+15551234567" {
		t.Errorf("expected +15551234567, got %s", got)
	}
}

func TestDBOrgNumberResolver_FallbackOnEmpty(t *testing.T) {
	lookup := &stubPhoneLookup{
		phones: map[string]string{
			"org-1": "", // empty in DB
		},
	}
	fallback := &stubFallbackResolver{
		numbers: map[string]string{
			"org-1": "+19999999999",
		},
	}

	resolver := NewDBOrgNumberResolver(lookup, fallback)
	got := resolver.DefaultFromNumber("org-1")

	if got != "+19999999999" {
		t.Errorf("expected fallback +19999999999, got %s", got)
	}
}

func TestDBOrgNumberResolver_FallbackOnError(t *testing.T) {
	lookup := &stubPhoneLookup{
		err: errors.New("db error"),
	}
	fallback := &stubFallbackResolver{
		numbers: map[string]string{
			"org-1": "+19999999999",
		},
	}

	resolver := NewDBOrgNumberResolver(lookup, fallback)
	got := resolver.DefaultFromNumber("org-1")

	if got != "+19999999999" {
		t.Errorf("expected fallback +19999999999, got %s", got)
	}
}

func TestDBOrgNumberResolver_NoFallback(t *testing.T) {
	lookup := &stubPhoneLookup{
		phones: map[string]string{},
	}

	resolver := NewDBOrgNumberResolver(lookup, nil)
	got := resolver.DefaultFromNumber("org-1")

	if got != "" {
		t.Errorf("expected empty string, got %s", got)
	}
}

func TestDBOrgNumberResolver_NilLookup(t *testing.T) {
	fallback := &stubFallbackResolver{
		numbers: map[string]string{
			"org-1": "+19999999999",
		},
	}

	resolver := NewDBOrgNumberResolver(nil, fallback)
	got := resolver.DefaultFromNumber("org-1")

	if got != "+19999999999" {
		t.Errorf("expected fallback +19999999999, got %s", got)
	}
}
