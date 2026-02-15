package conversation

import (
	"strings"
	"testing"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
)

func TestDeriveConfigFromKnowledge(t *testing.T) {
	sk := &StructuredKnowledge{
		OrgID:   "org-1",
		Version: 1,
		Sections: KnowledgeSections{
			Services: ServiceSection{
				Items: []ServiceItem{
					{
						ID:                 "svc-1",
						Name:               "Tox",
						Category:           "Wrinkle Relaxers",
						Price:              "$12/unit",
						PriceType:          "variable",
						DurationMinutes:    30,
						Description:        "Soften fine lines",
						ProviderIDs:        []string{"33950", "38627"},
						BookingID:          "18430",
						Aliases:            []string{"botox", "jeuveau", "xeomin"},
						DepositAmountCents: 5000,
						Order:              1,
					},
					{
						ID:              "svc-2",
						Name:            "Lip Filler",
						Category:        "Fillers",
						Price:           "$650",
						PriceType:       "fixed",
						DurationMinutes: 45,
						ProviderIDs:     []string{"33950"},
						BookingID:       "20425",
						Aliases:         []string{"lip fillers", "lip injections"},
						Order:           2,
					},
				},
			},
			Providers: ProviderSection{
				Items: []ProviderItem{
					{ID: "33950", Name: "Brandi Sesock", Title: "NP", Order: 1},
					{ID: "38627", Name: "Gale Tesar", Title: "RN", Order: 2},
				},
			},
			Policies: PolicySection{
				Cancellation:    "24 hours notice required",
				Deposit:         "$50 deposit required",
				AgeRequirement:  "18+",
				BookingPolicies: []string{"Policy 1", "Policy 2"},
			},
		},
	}

	cfg := clinic.DefaultConfig("org-1")
	DeriveConfigFromKnowledge(sk, cfg)

	// Services
	if len(cfg.Services) != 2 || cfg.Services[0] != "Tox" || cfg.Services[1] != "Lip Filler" {
		t.Errorf("Services = %v", cfg.Services)
	}

	// Aliases
	if cfg.ServiceAliases["botox"] != "Tox" {
		t.Errorf("alias botox = %q", cfg.ServiceAliases["botox"])
	}
	if cfg.ServiceAliases["lip fillers"] != "Lip Filler" {
		t.Errorf("alias lip fillers = %q", cfg.ServiceAliases["lip fillers"])
	}

	// Price text
	if cfg.ServicePriceText["tox"] != "$12/unit" {
		t.Errorf("price tox = %q", cfg.ServicePriceText["tox"])
	}

	// Deposit amounts
	if cfg.ServiceDepositAmountCents["tox"] != 5000 {
		t.Errorf("deposit tox = %d", cfg.ServiceDepositAmountCents["tox"])
	}

	// Moxie service menu items
	if cfg.MoxieConfig.ServiceMenuItems["tox"] != "18430" {
		t.Errorf("menu item tox = %q", cfg.MoxieConfig.ServiceMenuItems["tox"])
	}
	if cfg.MoxieConfig.ServiceMenuItems["lip filler"] != "20425" {
		t.Errorf("menu item lip filler = %q", cfg.MoxieConfig.ServiceMenuItems["lip filler"])
	}

	// Provider counts
	if cfg.MoxieConfig.ServiceProviderCount["18430"] != 2 {
		t.Errorf("provider count 18430 = %d", cfg.MoxieConfig.ServiceProviderCount["18430"])
	}
	if cfg.MoxieConfig.ServiceProviderCount["20425"] != 1 {
		t.Errorf("provider count 20425 = %d", cfg.MoxieConfig.ServiceProviderCount["20425"])
	}

	// Provider names
	if cfg.MoxieConfig.ProviderNames["33950"] != "Brandi Sesock" {
		t.Errorf("provider name 33950 = %q", cfg.MoxieConfig.ProviderNames["33950"])
	}

	// Booking policies
	if len(cfg.BookingPolicies) != 2 || cfg.BookingPolicies[0] != "Policy 1" {
		t.Errorf("BookingPolicies = %v", cfg.BookingPolicies)
	}
}

func TestDeriveConfigFromKnowledge_NilInputs(t *testing.T) {
	// Should not panic
	DeriveConfigFromKnowledge(nil, &clinic.Config{})
	DeriveConfigFromKnowledge(&StructuredKnowledge{}, nil)
}

func TestFlattenKnowledgeForRAG(t *testing.T) {
	sk := &StructuredKnowledge{
		Sections: KnowledgeSections{
			Services: ServiceSection{
				Items: []ServiceItem{
					{
						Name:            "Tox",
						Category:        "Wrinkle Relaxers",
						Price:           "$12/unit",
						DurationMinutes: 30,
						Description:     "Soften fine lines",
						ProviderIDs:     []string{"33950"},
						Aliases:         []string{"botox"},
					},
				},
			},
			Providers: ProviderSection{
				Items: []ProviderItem{
					{ID: "33950", Name: "Brandi Sesock", Title: "NP"},
				},
			},
			Policies: PolicySection{
				Cancellation: "24 hours notice",
				Deposit:      "$50 deposit",
			},
			Custom: []CustomDoc{
				{Title: "Parking", Content: "Free parking in rear"},
			},
		},
	}

	docs := FlattenKnowledgeForRAG(sk)

	if len(docs) == 0 {
		t.Fatal("expected docs")
	}

	// Check service doc contains key info
	found := false
	for _, d := range docs {
		if strings.Contains(d, "Tox") && strings.Contains(d, "$12/unit") && strings.Contains(d, "Brandi Sesock") {
			found = true
		}
	}
	if !found {
		t.Errorf("service doc not found in %v", docs)
	}

	// Check policies
	hasCancel := false
	hasCustom := false
	for _, d := range docs {
		if strings.Contains(d, "Cancellation Policy") {
			hasCancel = true
		}
		if strings.Contains(d, "Parking") {
			hasCustom = true
		}
	}
	if !hasCancel {
		t.Error("missing cancellation policy doc")
	}
	if !hasCustom {
		t.Error("missing custom doc")
	}
}

func TestFlattenKnowledgeForRAG_Nil(t *testing.T) {
	docs := FlattenKnowledgeForRAG(nil)
	if docs != nil {
		t.Errorf("expected nil, got %v", docs)
	}
}
