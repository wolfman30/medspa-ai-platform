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

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
)

const (
	prefillTimeout   = 15 * time.Second
	prefillUserAgent = "Mozilla/5.0"
	maxPrefillPages  = 8
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

	services := buildServicesFromSitemap(sitemapURLs)
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
	reScripts := regexp.MustCompile(`(?is)<(script|style|noscript)[^>]*>.*?</\1>`)
	clean := reScripts.ReplaceAllString(htmlBody, " ")
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

func buildServicesFromSitemap(urls []string) []PrefillService {
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
	services := []PrefillService{}
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
		name := titleize(strings.ReplaceAll(segment, "-", " "))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		services = append(services, PrefillService{
			Name:            name,
			Description:     fmt.Sprintf("See website for details about %s.", name),
			DurationMinutes: 30,
			PriceRange:      "Varies",
			SourceURL:       raw,
		})
		if len(services) >= maxPrefillPages {
			break
		}
	}
	return services
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
	for _, name := range common {
		if strings.Contains(strings.ToLower(text), strings.ToLower(name)) {
			services = append(services, PrefillService{
				Name:            name,
				Description:     fmt.Sprintf("See website for details about %s.", name),
				DurationMinutes: 30,
				PriceRange:      "Varies",
			})
		}
	}
	return services
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

	dayPattern := `(?:mon(?:day)?|tue(?:sday)?|tues|wed(?:nesday)?|thu(?:rsday)?|thur|thurs|fri(?:day)?|sat(?:urday)?|sun(?:day)?)`
	re := regexp.MustCompile(`(?is)(${dayPattern}(?:\s*(?:&|and|,)\s*${dayPattern})*)\s*:\s*([^:]+?)(?=${dayPattern}|$)`)
	matches := re.FindAllStringSubmatch(hoursText, -1)
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		dayGroup := match[1]
		timePart := strings.TrimSpace(match[2])
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
