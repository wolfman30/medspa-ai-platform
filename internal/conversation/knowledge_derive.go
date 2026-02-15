package conversation

import (
	"fmt"
	"strings"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
)

// DeriveConfigFromKnowledge updates cfg in place from structured knowledge.
func DeriveConfigFromKnowledge(sk *StructuredKnowledge, cfg *clinic.Config) {
	if sk == nil || cfg == nil {
		return
	}

	// Services list
	services := make([]string, 0, len(sk.Sections.Services.Items))
	for _, svc := range sk.Sections.Services.Items {
		services = append(services, svc.Name)
	}
	cfg.Services = services

	// Service aliases
	aliases := make(map[string]string)
	for _, svc := range sk.Sections.Services.Items {
		for _, alias := range svc.Aliases {
			aliases[strings.ToLower(alias)] = svc.Name
		}
	}
	cfg.ServiceAliases = aliases

	// Service price text
	priceText := make(map[string]string)
	for _, svc := range sk.Sections.Services.Items {
		if svc.Price != "" {
			priceText[strings.ToLower(svc.Name)] = svc.Price
		}
	}
	cfg.ServicePriceText = priceText

	// Service deposit amounts
	depositAmounts := make(map[string]int)
	for _, svc := range sk.Sections.Services.Items {
		if svc.DepositAmountCents > 0 {
			depositAmounts[strings.ToLower(svc.Name)] = svc.DepositAmountCents
		}
	}
	cfg.ServiceDepositAmountCents = depositAmounts

	// Moxie config
	if cfg.MoxieConfig == nil {
		cfg.MoxieConfig = &clinic.MoxieConfig{}
	}

	menuItems := make(map[string]string)
	providerCount := make(map[string]int)
	for _, svc := range sk.Sections.Services.Items {
		if svc.BookingID != "" {
			menuItems[strings.ToLower(svc.Name)] = svc.BookingID
			providerCount[svc.BookingID] = len(svc.ProviderIDs)
		}
	}
	cfg.MoxieConfig.ServiceMenuItems = menuItems
	cfg.MoxieConfig.ServiceProviderCount = providerCount

	// Provider names
	providerNames := make(map[string]string)
	for _, p := range sk.Sections.Providers.Items {
		providerNames[p.ID] = p.Name
	}
	cfg.MoxieConfig.ProviderNames = providerNames

	// Booking policies
	cfg.BookingPolicies = sk.Sections.Policies.BookingPolicies
}

// FlattenKnowledgeForRAG produces human-readable documents for RAG ingestion.
func FlattenKnowledgeForRAG(sk *StructuredKnowledge) []string {
	if sk == nil {
		return nil
	}

	var docs []string

	// Build provider name lookup
	providerNames := make(map[string]string)
	for _, p := range sk.Sections.Providers.Items {
		providerNames[p.ID] = p.Name
	}

	// Services
	for _, svc := range sk.Sections.Services.Items {
		var parts []string
		parts = append(parts, fmt.Sprintf("Service: %s", svc.Name))
		if svc.Category != "" {
			parts = append(parts, fmt.Sprintf("Category: %s", svc.Category))
		}
		if svc.Price != "" {
			parts = append(parts, fmt.Sprintf("Price: %s", svc.Price))
		}
		if svc.DurationMinutes > 0 {
			parts = append(parts, fmt.Sprintf("Duration: %d min", svc.DurationMinutes))
		}
		if svc.Description != "" {
			parts = append(parts, svc.Description)
		}
		if len(svc.ProviderIDs) > 0 {
			var names []string
			for _, id := range svc.ProviderIDs {
				if name, ok := providerNames[id]; ok {
					names = append(names, name)
				}
			}
			if len(names) > 0 {
				parts = append(parts, fmt.Sprintf("Providers: %s", strings.Join(names, ", ")))
			}
		}
		if len(svc.Aliases) > 0 {
			parts = append(parts, fmt.Sprintf("Also known as: %s", strings.Join(svc.Aliases, ", ")))
		}
		docs = append(docs, strings.Join(parts, " - "))
	}

	// Providers
	for _, p := range sk.Sections.Providers.Items {
		parts := []string{fmt.Sprintf("Provider: %s", p.Name)}
		if p.Title != "" {
			parts = append(parts, p.Title)
		}
		if p.Bio != "" {
			parts = append(parts, p.Bio)
		}
		if len(p.Specialties) > 0 {
			parts = append(parts, fmt.Sprintf("Specialties: %s", strings.Join(p.Specialties, ", ")))
		}
		docs = append(docs, strings.Join(parts, " - "))
	}

	// Policies
	if sk.Sections.Policies.Cancellation != "" {
		docs = append(docs, fmt.Sprintf("Cancellation Policy: %s", sk.Sections.Policies.Cancellation))
	}
	if sk.Sections.Policies.Deposit != "" {
		docs = append(docs, fmt.Sprintf("Deposit Policy: %s", sk.Sections.Policies.Deposit))
	}
	if sk.Sections.Policies.AgeRequirement != "" {
		docs = append(docs, fmt.Sprintf("Age Requirement: %s", sk.Sections.Policies.AgeRequirement))
	}
	for _, p := range sk.Sections.Policies.Custom {
		docs = append(docs, p)
	}

	// Custom docs
	for _, c := range sk.Sections.Custom {
		docs = append(docs, fmt.Sprintf("%s: %s", c.Title, c.Content))
	}

	return docs
}
