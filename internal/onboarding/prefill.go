package onboarding

import (
	"context"
	"net/http"
	"time"

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

type servicePageContent struct {
	Heading         string
	Paragraphs      []string
	MetaDescription string
	MetaTitle       string
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
