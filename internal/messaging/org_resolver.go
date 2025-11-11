package messaging

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

var (
	// ErrOrgNotFound is returned when we can't map a Twilio number to an org.
	ErrOrgNotFound = errors.New("messaging: org not found for number")
	phoneDigitsRe  = regexp.MustCompile(`\d+`)
)

// OrgResolver resolves the destination org for a Twilio number.
type OrgResolver interface {
	ResolveOrgID(ctx context.Context, toNumber string) (string, error)
}

// StaticOrgResolver maps sanitized phone numbers to org IDs.
type StaticOrgResolver struct {
	mapping  map[string]string
	defaults map[string]string
}

// NewStaticOrgResolver constructs a resolver backed by an in-memory map.
func NewStaticOrgResolver(mapping map[string]string) *StaticOrgResolver {
	normalized := make(map[string]string, len(mapping))
	defaults := make(map[string]string)
	for raw, org := range mapping {
		clean := sanitizePhone(raw)
		if clean == "" || org == "" {
			continue
		}
		normalized[clean] = org
		if _, ok := defaults[org]; !ok {
			defaults[org] = normalizeE164(raw)
		}
	}
	return &StaticOrgResolver{mapping: normalized, defaults: defaults}
}

// ResolveOrgID implements OrgResolver.
func (r *StaticOrgResolver) ResolveOrgID(ctx context.Context, toNumber string) (string, error) {
	if r == nil {
		return "", ErrOrgNotFound
	}
	key := sanitizePhone(toNumber)
	if key == "" {
		return "", ErrOrgNotFound
	}
	org, ok := r.mapping[key]
	if !ok {
		return "", ErrOrgNotFound
	}
	return org, nil
}

func sanitizePhone(value string) string {
	if value == "" {
		return ""
	}
	digits := phoneDigitsRe.FindAllString(value, -1)
	return strings.Join(digits, "")
}

func normalizeE164(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "+") {
		return value
	}
	return "+" + sanitizePhone(value)
}

// DefaultFromNumber returns the preferred sending number for the org.
func (r *StaticOrgResolver) DefaultFromNumber(orgID string) string {
	if r == nil {
		return ""
	}
	return r.defaults[orgID]
}
