package onboarding

import (
	"fmt"
	"html"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"

	nethtml "golang.org/x/net/html"
)

// Type aliases for golang.org/x/net/html used across prefill files.
type htmlNode = nethtml.Node

const elementNode = nethtml.ElementNode

func parseHTML(htmlBody string) (*htmlNode, error) {
	return nethtml.Parse(strings.NewReader(htmlBody))
}

func extractTitle(htmlBody string) string {
	re := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	match := re.FindStringSubmatch(htmlBody)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(html.UnescapeString(match[1]))
}

func extractText(htmlBody string) string {
	clean := htmlBody
	for _, pattern := range []string{
		`(?is)<script[^>]*>.*?</script>`,
		`(?is)<style[^>]*>.*?</style>`,
		`(?is)<noscript[^>]*>.*?</noscript>`,
	} {
		re := regexp.MustCompile(pattern)
		clean = re.ReplaceAllString(clean, " ")
	}
	reTags := regexp.MustCompile(`(?s)<[^>]+>`)
	clean = reTags.ReplaceAllString(clean, " ")
	clean = html.UnescapeString(clean)
	reSpace := regexp.MustCompile(`\s+`)
	clean = reSpace.ReplaceAllString(clean, " ")
	return strings.TrimSpace(clean)
}

func extractClinicName(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	parts := strings.Split(title, "|")
	candidates := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lower := strings.ToLower(part)
		if strings.Contains(lower, "contact") || strings.Contains(lower, "services") || strings.Contains(lower, "home") {
			continue
		}
		candidates = append(candidates, part)
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return len(candidates[i]) > len(candidates[j])
	})
	return candidates[0]
}

func deriveNameFromHost(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(parsed.Hostname(), "www.")
	if host == "" {
		return ""
	}
	host = strings.ReplaceAll(host, "-", " ")
	host = strings.ReplaceAll(host, ".", " ")
	return titleize(host)
}

func buildServicesFromText(text string) []PrefillService {
	if text == "" {
		return nil
	}
	common := []string{
		"Botox", "Wrinkle Relaxers", "Dermal Fillers", "Microneedling", "Chemical Peels",
		"Laser Hair Removal", "PRP", "Vitamin Injections", "Hydrafacial", "Weight Loss",
	}
	services := []PrefillService{}
	normalized := normalizeWhitespace(text)
	for _, name := range common {
		if strings.Contains(strings.ToLower(normalized), strings.ToLower(name)) {
			description := findSentenceContaining(normalized, name)
			if description == "" {
				description = truncateText(normalized, 240)
			}
			if description == "" {
				description = fmt.Sprintf("%s treatments are offered at this clinic.", name)
			}
			duration := parseDurationMinutes(description)
			if duration == 0 {
				duration = 30
			}
			priceRange := parsePriceRange(description)
			if priceRange == "" {
				priceRange = "Varies"
			}
			services = append(services, PrefillService{
				Name:            name,
				Description:     description,
				DurationMinutes: duration,
				PriceRange:      priceRange,
			})
		}
	}
	return services
}

func normalizeWhitespace(text string) string {
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "\uFEFF", "")
	text = strings.ReplaceAll(text, "\u200B", "")
	reSpace := regexp.MustCompile(`\s+`)
	return strings.TrimSpace(reSpace.ReplaceAllString(text, " "))
}

func isBoilerplateParagraph(text string) bool {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "forever 22 delivers aesthetic care") {
		return true
	}
	if strings.Contains(lower, "all rights reserved") {
		return true
	}
	if strings.Contains(lower, "address:") || strings.Contains(lower, "phone") || strings.Contains(lower, "email") {
		return true
	}
	if strings.Contains(lower, "website optimized and designed by") || strings.Contains(lower, "bluefoot") {
		return true
	}
	return false
}

func splitSentences(text string) []string {
	if text == "" {
		return nil
	}
	sentences := []string{}
	var current strings.Builder
	for _, r := range text {
		current.WriteRune(r)
		if r == '.' || r == '!' || r == '?' {
			sentence := strings.TrimSpace(current.String())
			if sentence != "" {
				sentences = append(sentences, sentence)
			}
			current.Reset()
		}
	}
	if current.Len() > 0 {
		sentence := strings.TrimSpace(current.String())
		if sentence != "" {
			sentences = append(sentences, sentence)
		}
	}
	return sentences
}

func findSentenceContaining(text, needle string) string {
	if text == "" || needle == "" {
		return ""
	}
	needle = strings.ToLower(needle)
	for _, sentence := range splitSentences(text) {
		if strings.Contains(strings.ToLower(sentence), needle) {
			return sentence
		}
	}
	return ""
}

func splitServiceTitle(title string) []string {
	title = normalizeWhitespace(title)
	if title == "" {
		return nil
	}
	lower := strings.ToLower(title)
	if strings.Contains(lower, "prp") && strings.Contains(lower, "pbf") && (strings.Contains(title, ",") || strings.Contains(lower, " and ")) {
		title = strings.TrimSuffix(strings.TrimSuffix(title, " services"), " service")
		reAnd := regexp.MustCompile(`(?i)\s+and\s+`)
		parts := []string{}
		for _, piece := range strings.Split(title, ",") {
			subparts := reAnd.Split(piece, -1)
			for _, sub := range subparts {
				sub = strings.TrimSpace(sub)
				if sub != "" {
					parts = append(parts, sub)
				}
			}
		}
		if len(parts) > 0 {
			return parts
		}
	}
	return []string{title}
}

func cleanServiceName(name string) string {
	name = normalizeWhitespace(name)
	if name == "" {
		return ""
	}
	rePRP := regexp.MustCompile(`(?i)\s*\(platelet-rich plasma\)`)
	name = rePRP.ReplaceAllString(name, "")
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, " services") {
		name = strings.TrimSpace(name[:len(name)-len(" services")])
		lower = strings.ToLower(name)
	}
	if strings.HasSuffix(lower, " service") {
		name = strings.TrimSpace(name[:len(name)-len(" service")])
	}
	name = strings.TrimSuffix(name, ".")
	name = strings.TrimSpace(name)
	return normalizeServiceNameCase(name)
}

func normalizeServiceNameCase(name string) string {
	if name == "" {
		return ""
	}
	words := strings.Fields(name)
	for i, word := range words {
		prefix, core, suffix := splitWordPunctuation(word)
		if core == "" {
			continue
		}
		if !isAlphabeticWord(core) {
			continue
		}
		if isAllUpperWord(core) && len([]rune(core)) > 3 {
			core = toTitleCaseWord(core)
		} else if hasMixedCaseWord(core) && !isTitleCaseWord(core) {
			core = toTitleCaseWord(core)
		}
		words[i] = prefix + core + suffix
	}
	return strings.Join(words, " ")
}

func splitWordPunctuation(word string) (string, string, string) {
	runes := []rune(word)
	if len(runes) == 0 {
		return "", "", ""
	}
	start := 0
	for start < len(runes) && !unicode.IsLetter(runes[start]) {
		start++
	}
	end := len(runes)
	for end > start && !unicode.IsLetter(runes[end-1]) {
		end--
	}
	return string(runes[:start]), string(runes[start:end]), string(runes[end:])
}

func isAlphabeticWord(word string) bool {
	if word == "" {
		return false
	}
	for _, r := range word {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

func isAllUpperWord(word string) bool {
	if word == "" {
		return false
	}
	for _, r := range word {
		if !unicode.IsUpper(r) {
			return false
		}
	}
	return true
}

func hasMixedCaseWord(word string) bool {
	if word == "" {
		return false
	}
	hasUpper := false
	hasLower := false
	for _, r := range word {
		if unicode.IsUpper(r) {
			hasUpper = true
		} else if unicode.IsLower(r) {
			hasLower = true
		}
	}
	return hasUpper && hasLower
}

func isTitleCaseWord(word string) bool {
	runes := []rune(word)
	if len(runes) == 0 {
		return false
	}
	if !unicode.IsUpper(runes[0]) {
		return false
	}
	for _, r := range runes[1:] {
		if !unicode.IsLower(r) {
			return false
		}
	}
	return true
}

func toTitleCaseWord(word string) string {
	runes := []rune(strings.ToLower(word))
	if len(runes) == 0 {
		return ""
	}
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func serviceNameFromURL(serviceURL string) string {
	parsed, err := url.Parse(serviceURL)
	if err != nil {
		return ""
	}
	segment := path.Base(strings.TrimSuffix(parsed.Path, "/"))
	if segment == "" || segment == "." {
		return ""
	}
	return titleize(strings.ReplaceAll(segment, "-", " "))
}

func parseDurationMinutes(text string) int {
	re := regexp.MustCompile(`(?i)\b(\d{1,3})\s*(hours?|hrs?|minutes?|mins?|min)\b`)
	match := re.FindStringSubmatch(text)
	if len(match) < 3 {
		return 0
	}
	value := atoi(match[1])
	unit := strings.ToLower(match[2])
	if strings.HasPrefix(unit, "hour") || strings.HasPrefix(unit, "hr") {
		return value * 60
	}
	return value
}

func parsePriceRange(text string) string {
	if text == "" {
		return ""
	}
	rangeRe := regexp.MustCompile(`\$\s?\d{1,3}(?:,\d{3})*(?:\.\d{2})?\s*-\s*\$\s?\d{1,3}(?:,\d{3})*(?:\.\d{2})?`)
	if match := rangeRe.FindString(text); match != "" {
		return normalizeWhitespace(match)
	}
	priceRe := regexp.MustCompile(`\$\s?\d{1,3}(?:,\d{3})*(?:\.\d{2})?`)
	match := priceRe.FindString(text)
	if match == "" {
		return ""
	}
	price := normalizeWhitespace(match)
	normalized := strings.ToLower(normalizeWhitespace(text))
	priceLower := strings.ToLower(price)
	if strings.Contains(normalized, "starting at "+priceLower) ||
		strings.Contains(normalized, "starting at"+priceLower) ||
		strings.Contains(normalized, "from "+priceLower) ||
		strings.Contains(normalized, "from"+priceLower) ||
		strings.Contains(normalized, "as low as "+priceLower) {
		return price + "+"
	}
	return price
}

func truncateText(text string, max int) string {
	text = normalizeWhitespace(text)
	if text == "" || max <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	cut := string(runes[:max])
	if idx := strings.LastIndexAny(cut, ".!?"); idx > 80 {
		return strings.TrimSpace(cut[:idx+1])
	}
	if idx := strings.LastIndex(cut, " "); idx > 0 {
		cut = cut[:idx]
	}
	return strings.TrimSpace(cut)
}

func getHTMLAttr(n *htmlNode, key string) string {
	for _, attr := range n.Attr {
		if strings.EqualFold(attr.Key, key) {
			return attr.Val
		}
	}
	return ""
}

func nodeText(n *htmlNode) string {
	if n == nil {
		return ""
	}
	var builder strings.Builder
	var walk func(*htmlNode)
	walk = func(node *htmlNode) {
		if node.Type == nethtml.TextNode {
			builder.WriteString(node.Data)
		}
		if node.Type == elementNode {
			switch node.Data {
			case "script", "style", "noscript":
				return
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return strings.TrimSpace(html.UnescapeString(builder.String()))
}

func extractEmail(text string) string {
	re := regexp.MustCompile(`[\w._%+\-]+@[\w.\-]+\.[A-Za-z]{2,}`)
	match := re.FindString(text)
	return strings.TrimSpace(match)
}

func extractPhone(text string) string {
	re := regexp.MustCompile(`\+?1?[\s\-.()]*\d{3}[\s\-.()]*\d{3}[\s\-.()]*\d{4}`)
	match := re.FindString(text)
	return strings.TrimSpace(match)
}

func extractAddress(text string) (string, string, string, string) {
	addressText := extractBetween(text, "Address:", []string{"Hours", "Phone", "Email", "Connect", "Contact"})
	if addressText == "" {
		addressText = text
	}
	re := regexp.MustCompile(`(?i)(\d+\s+[^,]+?)[,\s]+([A-Za-z .]+),\s*([A-Z]{2})\s*(\d{5})`)
	match := re.FindStringSubmatch(addressText)
	if len(match) < 5 {
		return "", "", "", ""
	}
	return strings.TrimSpace(match[1]), strings.TrimSpace(match[2]), strings.TrimSpace(match[3]), strings.TrimSpace(match[4])
}

func extractBetween(text, start string, stops []string) string {
	lower := strings.ToLower(text)
	idx := strings.Index(lower, strings.ToLower(start))
	if idx == -1 {
		return ""
	}
	segment := text[idx+len(start):]
	stopIdx := len(segment)
	for _, stop := range stops {
		if stop == "" {
			continue
		}
		if i := strings.Index(strings.ToLower(segment), strings.ToLower(stop)); i >= 0 && i < stopIdx {
			stopIdx = i
		}
	}
	return strings.TrimSpace(segment[:stopIdx])
}
