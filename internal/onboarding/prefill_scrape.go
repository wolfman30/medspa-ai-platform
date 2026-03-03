package onboarding

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
)

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
	doc, err := parseHTML(htmlBody)
	if err != nil {
		return servicePageContent{}
	}
	content := servicePageContent{}
	paragraphs := []string{}
	foundHeading := false

	var walk func(*htmlNode)
	walk = func(n *htmlNode) {
		if n.Type == elementNode {
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
