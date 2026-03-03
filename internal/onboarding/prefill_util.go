package onboarding

import (
	"fmt"
	"net/url"
	"strings"
)

func normalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("website_url is required")
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("invalid website_url")
	}
	parsed.Fragment = ""
	parsed.RawQuery = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func titleize(raw string) string {
	words := strings.Fields(raw)
	if len(words) == 0 {
		return ""
	}
	acronyms := map[string]bool{"prp": true, "rf": true, "ipl": true}
	for i, w := range words {
		lower := strings.ToLower(w)
		if acronyms[lower] {
			words[i] = strings.ToUpper(lower)
			continue
		}
		words[i] = strings.ToUpper(lower[:1]) + lower[1:]
	}
	return strings.Join(words, " ")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		result = append(result, v)
	}
	return result
}
