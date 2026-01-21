package onboarding

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	nethtml "golang.org/x/net/html"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
)

const (
	prefillTimeout   = 15 * time.Second
	prefillUserAgent = "Mozilla/5.0"
	maxPrefillPages  = 20
)

type PrefillService struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	DurationMinutes int    `json:"duration_minutes"`
	PriceRange      string `json:"price_range"`
	SourceURL       string `json:"source_url,omitempty"`
}

type PrefillClinicInfo struct {
	Name       string `json:"name,omitempty"`
	Email      string `json:"email,omitempty"`
	Phone      string `json:"phone,omitempty"`
	Address    string `json:"address,omitempty"`
	City       string `json:"city,omitempty"`
	State      string `json:"state,omitempty"`
	ZipCode    string `json:"zip_code,omitempty"`
	WebsiteURL string `json:"website_url,omitempty"`
	Timezone   string `json:"timezone,omitempty"`
}

type PrefillResult struct {
	ClinicInfo    PrefillClinicInfo    `json:"clinic_info"`
	Services      []PrefillService     `json:"services"`
	BusinessHours clinic.BusinessHours `json:"business_hours"`
	Sources       []string             `json:"sources,omitempty"`
	Warnings      []string             `json:"warnings,omitempty"`
}

type sitemapURLSet struct {
	URLs []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

func ScrapeClinicPrefill(ctx context.Context, rawURL string) (*PrefillResult, error) {
	baseURL, err := normalizeURL(rawURL)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: prefillTimeout}
	sitemapURLs, _ := fetchSitemapURLs(ctx, client, baseURL)
	contactURL := pickFirstURLContaining(sitemapURLs, "contact")
	if contactURL == "" {
		contactURL = joinURL(baseURL, "/contact-us")
	}
	servicesURL := pickFirstURLContaining(sitemapURLs, "/services")
	if servicesURL == "" {
		servicesURL = joinURL(baseURL, "/services")
	}

	baseTitle, _, _ := fetchText(ctx, client, baseURL)
	contactTitle, contactText, _ := fetchText(ctx, client, contactURL)
	servicesTitle, servicesText, _ := fetchText(ctx, client, servicesURL)

	clinicName := firstNonEmpty(
		extractClinicName(baseTitle),
		extractClinicName(contactTitle),
		extractClinicName(servicesTitle),
		deriveNameFromHost(baseURL),
	)

	clinicInfo := PrefillClinicInfo{
		Name:       clinicName,
		WebsiteURL: baseURL,
	}

	if contactText != "" {
		clinicInfo.Email = extractEmail(contactText)
		clinicInfo.Phone = extractPhone(contactText)
		addr, city, state, zip := extractAddress(contactText)
		clinicInfo.Address = addr
		clinicInfo.City = city
		clinicInfo.State = state
		clinicInfo.ZipCode = zip
		clinicInfo.Timezone = timezoneForState(state)
	}

	if clinicInfo.Timezone == "" {
		clinicInfo.Timezone = timezoneForState(clinicInfo.State)
	}
	if clinicInfo.Timezone == "" {
		clinicInfo.Timezone = "America/New_York"
	}

	serviceURLs := buildServiceURLsFromSitemap(sitemapURLs)
	services := scrapeServicesFromPages(ctx, client, serviceURLs)
	if len(services) == 0 {
		services = buildServicesFromText(servicesText)
	}

	hours := parseBusinessHours(contactText)

	sources := uniqueStrings([]string{baseURL, contactURL, servicesURL})
	return &PrefillResult{
		ClinicInfo:    clinicInfo,
		Services:      services,
		BusinessHours: hours,
		Sources:       sources,
	}, nil
}

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

func fetchSitemapURLs(ctx context.Context, client *http.Client, baseURL string) ([]string, error) {
	sitemapURL := joinURL(baseURL, "/sitemap.xml")
	body, err := fetchURL(ctx, client, sitemapURL)
	if err != nil {
		return nil, err
	}
	var sitemap sitemapURLSet
	if err := xml.Unmarshal([]byte(body), &sitemap); err != nil {
		return nil, err
	}
	urls := make([]string, 0, len(sitemap.URLs))
	for _, entry := range sitemap.URLs {
		if entry.Loc == "" {
			continue
		}
		urls = append(urls, strings.TrimSpace(entry.Loc))
	}
	return urls, nil
}

func fetchURL(ctx context.Context, client *http.Client, target string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", prefillUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("prefill fetch failed: %s", resp.Status)
	}
	reader := io.LimitReader(resp.Body, 3<<20)
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func fetchText(ctx context.Context, client *http.Client, target string) (string, string, error) {
	if target == "" {
		return "", "", nil
	}
	body, err := fetchURL(ctx, client, target)
	if err != nil {
		return "", "", err
	}
	title := extractTitle(body)
	text := extractText(body)
	return title, text, nil
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

func buildServiceURLsFromSitemap(urls []string) []string {
	if len(urls) == 0 {
		return nil
	}
	excluded := map[string]bool{
		"":            true,
		"/":           true,
		"/contact-us": true,
		"/contact":    true,
		"/about":      true,
		"/about-us":   true,
		"/financing":  true,
		"/events":     true,
		"/membership": true,
		"/shop":       true,
	}
	serviceURLs := []string{}
	seen := map[string]bool{}
	for _, raw := range urls {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Path == "" {
			continue
		}
		slug := strings.TrimSuffix(parsed.Path, "/")
		if excluded[strings.ToLower(slug)] {
			continue
		}
		segment := path.Base(slug)
		if segment == "" || segment == "." || segment == "services" {
			continue
		}
		if seen[slug] {
			continue
		}
		seen[slug] = true
		serviceURLs = append(serviceURLs, strings.TrimRight(raw, "/"))
		if len(serviceURLs) >= maxPrefillPages {
			break
		}
	}
	return serviceURLs
}

type servicePageContent struct {
	Heading         string
	Paragraphs      []string
	MetaDescription string
	MetaTitle       string
}

func scrapeServicesFromPages(ctx context.Context, client *http.Client, urls []string) []PrefillService {
	if len(urls) == 0 {
		return nil
	}
	services := []PrefillService{}
	seen := map[string]bool{}
	for _, serviceURL := range urls {
		details, err := scrapeServiceDetails(ctx, client, serviceURL)
		if err != nil || len(details) == 0 {
			name := serviceNameFromURL(serviceURL)
			if name == "" {
				continue
			}
			details = []PrefillService{{
				Name:            name,
				Description:     fmt.Sprintf("%s treatments are offered at this clinic.", name),
				DurationMinutes: 30,
				PriceRange:      "Varies",
				SourceURL:       serviceURL,
			}}
		}
		for _, service := range details {
			key := strings.ToLower(strings.TrimSpace(service.Name))
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			services = append(services, service)
			if len(services) >= maxPrefillPages {
				return services
			}
		}
	}
	return services
}

func scrapeServiceDetails(ctx context.Context, client *http.Client, serviceURL string) ([]PrefillService, error) {
	body, err := fetchURL(ctx, client, serviceURL)
	if err != nil {
		return nil, err
	}

	content := extractServicePageContent(body)
	name := content.Heading
	if name == "" {
		name = content.MetaTitle
	}
	if name == "" {
		name = extractTitle(body)
		if name != "" {
			name = strings.TrimSpace(strings.Split(name, "|")[0])
		}
	}
	if name == "" {
		name = serviceNameFromURL(serviceURL)
	}
	name = cleanServiceName(name)

	description := ""
	if len(content.Paragraphs) > 0 {
		description = strings.Join(content.Paragraphs, " ")
	}
	if description == "" && content.MetaDescription != "" {
		description = content.MetaDescription
	}
	if description == "" {
		description = truncateText(extractText(body), 360)
	}
	description = normalizeWhitespace(description)
	if description == "" && name != "" {
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

	names := splitServiceTitle(name)
	if len(names) == 0 {
		names = []string{name}
	}

	services := make([]PrefillService, 0, len(names))
	for _, entry := range names {
		entry = cleanServiceName(entry)
		if entry == "" {
			continue
		}
		services = append(services, PrefillService{
			Name:            entry,
			Description:     description,
			DurationMinutes: duration,
			PriceRange:      priceRange,
			SourceURL:       serviceURL,
		})
	}
	return services, nil
}

func extractServicePageContent(htmlBody string) servicePageContent {
	doc, err := nethtml.Parse(strings.NewReader(htmlBody))
	if err != nil {
		return servicePageContent{}
	}
	content := servicePageContent{}
	paragraphs := []string{}
	foundHeading := false

	var walk func(*nethtml.Node)
	walk = func(n *nethtml.Node) {
		if n.Type == nethtml.ElementNode {
			switch n.Data {
			case "script", "style", "noscript":
				return
			case "meta":
				if content.MetaDescription == "" {
					name := strings.ToLower(getHTMLAttr(n, "name"))
					prop := strings.ToLower(getHTMLAttr(n, "property"))
					if name == "description" || prop == "og:description" || name == "twitter:description" {
						content.MetaDescription = normalizeWhitespace(getHTMLAttr(n, "content"))
					}
				}
				if content.MetaTitle == "" {
					name := strings.ToLower(getHTMLAttr(n, "name"))
					prop := strings.ToLower(getHTMLAttr(n, "property"))
					if prop == "og:title" || name == "twitter:title" {
						content.MetaTitle = normalizeWhitespace(getHTMLAttr(n, "content"))
					}
				}
			case "h1", "h2":
				if content.Heading == "" {
					heading := normalizeWhitespace(nodeText(n))
					if heading != "" {
						content.Heading = heading
						foundHeading = true
					}
				}
			case "p":
				if foundHeading && len(paragraphs) < 2 {
					text := normalizeWhitespace(nodeText(n))
					if len(text) >= 40 && !isBoilerplateParagraph(text) {
						paragraphs = append(paragraphs, text)
					}
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	content.Paragraphs = paragraphs
	return content
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

func getHTMLAttr(n *nethtml.Node, key string) string {
	for _, attr := range n.Attr {
		if strings.EqualFold(attr.Key, key) {
			return attr.Val
		}
	}
	return ""
}

func nodeText(n *nethtml.Node) string {
	if n == nil {
		return ""
	}
	var builder strings.Builder
	var walk func(*nethtml.Node)
	walk = func(node *nethtml.Node) {
		if node.Type == nethtml.TextNode {
			builder.WriteString(node.Data)
		}
		if node.Type == nethtml.ElementNode {
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

func parseBusinessHours(text string) clinic.BusinessHours {
	var hours clinic.BusinessHours
	hoursText := extractBetween(text, "Hours:", []string{"Connect", "Contact", "Book"})
	if hoursText == "" {
		return hours
	}

	dayPattern := `(?i)(?:mon(?:day)?|tue(?:sday)?|tues|wed(?:nesday)?|thu(?:rsday)?|thur|thurs|fri(?:day)?|sat(?:urday)?|sun(?:day)?)(?:\s*(?:&|and|,|-)\s*(?:mon(?:day)?|tue(?:sday)?|tues|wed(?:nesday)?|thu(?:rsday)?|thur|thurs|fri(?:day)?|sat(?:urday)?|sun(?:day)?))*`
	re := regexp.MustCompile(dayPattern)
	matches := re.FindAllStringIndex(hoursText, -1)
	for i, match := range matches {
		if len(match) < 2 {
			continue
		}
		dayGroup := strings.TrimSpace(hoursText[match[0]:match[1]])
		start := match[1]
		end := len(hoursText)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		timePart := strings.TrimSpace(hoursText[start:end])
		timePart = strings.TrimSpace(strings.TrimLeft(timePart, ":-"))
		days := extractDays(dayGroup)
		if len(days) == 0 {
			continue
		}
		if strings.Contains(strings.ToLower(timePart), "closed") {
			for _, day := range days {
				setDayHours(&hours, day, nil)
			}
			continue
		}
		open, close := parseTimeRange(timePart)
		if open == "" || close == "" {
			continue
		}
		for _, day := range days {
			setDayHours(&hours, day, &clinic.DayHours{Open: open, Close: close})
		}
	}
	return hours
}

func extractDays(text string) []string {
	re := regexp.MustCompile(`(?i)\b(mon(?:day)?|tue(?:sday)?|tues|wed(?:nesday)?|thu(?:rsday)?|thur|thurs|fri(?:day)?|sat(?:urday)?|sun(?:day)?)\b`)
	matches := re.FindAllStringSubmatch(text, -1)
	result := []string{}
	seen := map[string]bool{}
	for _, match := range matches {
		normalized := normalizeDay(match[1])
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		result = append(result, normalized)
	}
	return result
}

func normalizeDay(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch raw {
	case "mon", "monday":
		return "monday"
	case "tue", "tues", "tuesday":
		return "tuesday"
	case "wed", "wednesday":
		return "wednesday"
	case "thu", "thur", "thurs", "thursday":
		return "thursday"
	case "fri", "friday":
		return "friday"
	case "sat", "saturday":
		return "saturday"
	case "sun", "sunday":
		return "sunday"
	default:
		return ""
	}
}

func parseTimeRange(raw string) (string, string) {
	re := regexp.MustCompile(`(?i)(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\s*-\s*(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
	match := re.FindStringSubmatch(raw)
	if len(match) < 7 {
		return "", ""
	}
	startMeridiem := strings.ToLower(match[3])
	endMeridiem := strings.ToLower(match[6])
	if endMeridiem == "" {
		endMeridiem = startMeridiem
	}
	open := formatTime(match[1], match[2], startMeridiem)
	close := formatTime(match[4], match[5], endMeridiem)
	return open, close
}

func formatTime(hourRaw, minRaw, meridiem string) string {
	hour := atoi(hourRaw)
	min := atoi(minRaw)
	if meridiem == "" {
		meridiem = "am"
	}
	if meridiem == "pm" && hour < 12 {
		hour += 12
	}
	if meridiem == "am" && hour == 12 {
		hour = 0
	}
	return fmt.Sprintf("%02d:%02d", hour, min)
}

func atoi(raw string) int {
	if raw == "" {
		return 0
	}
	n := 0
	for _, r := range raw {
		if r < '0' || r > '9' {
			continue
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func setDayHours(hours *clinic.BusinessHours, day string, value *clinic.DayHours) {
	switch day {
	case "monday":
		hours.Monday = value
	case "tuesday":
		hours.Tuesday = value
	case "wednesday":
		hours.Wednesday = value
	case "thursday":
		hours.Thursday = value
	case "friday":
		hours.Friday = value
	case "saturday":
		hours.Saturday = value
	case "sunday":
		hours.Sunday = value
	}
}

func pickFirstURLContaining(urls []string, keyword string) string {
	keyword = strings.ToLower(keyword)
	for _, candidate := range urls {
		if strings.Contains(strings.ToLower(candidate), keyword) {
			return strings.TrimRight(candidate, "/")
		}
	}
	return ""
}

func joinURL(base, suffix string) string {
	base = strings.TrimRight(base, "/")
	if suffix == "" {
		return base
	}
	return base + suffix
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

func timezoneForState(state string) string {
	state = strings.ToUpper(strings.TrimSpace(state))
	if state == "" {
		return ""
	}
	switch state {
	case "CT", "DE", "FL", "GA", "IN", "KY", "ME", "MD", "MA", "MI", "NH", "NJ", "NY", "NC", "OH", "PA", "RI", "SC", "TN", "VT", "VA", "WV", "DC":
		return "America/New_York"
	case "AL", "AR", "IL", "IA", "LA", "MN", "MS", "MO", "OK", "WI", "TX", "KS", "NE", "ND", "SD":
		return "America/Chicago"
	case "AZ", "CO", "ID", "MT", "NM", "UT", "WY":
		return "America/Denver"
	case "CA", "NV", "OR", "WA":
		return "America/Los_Angeles"
	case "AK":
		return "America/Anchorage"
	case "HI":
		return "Pacific/Honolulu"
	default:
		return ""
	}
}
