package handlers

import (
	"fmt"
	"strings"
)

func phoneDigitsCandidates(raw string) []string {
	digits := sanitizeDigits(raw)
	if digits == "" {
		return nil
	}
	candidates := []string{digits}
	if len(digits) == 10 {
		candidates = append(candidates, "1"+digits)
	} else if len(digits) == 11 && strings.HasPrefix(digits, "1") {
		candidates = append(candidates, digits[1:])
	}
	return uniqueStrings(candidates)
}

func appendPhoneDigitsFilter(columnExpr string, digits []string, args *[]any, argNum *int) string {
	if len(digits) == 0 {
		return ""
	}
	placeholders := make([]string, 0, len(digits))
	for _, d := range digits {
		placeholders = append(placeholders, fmt.Sprintf("$%d", *argNum))
		*args = append(*args, d)
		*argNum++
	}
	return fmt.Sprintf(" AND %s IN (%s)", columnExpr, strings.Join(placeholders, ","))
}

func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
