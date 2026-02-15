package handlers

import (
	"testing"
)

func TestParseMoxieBookingJSON(t *testing.T) {
	fixture := []byte(`{
		"pageProps": {
			"medspa": {"id": 1264, "name": "Forever 22 Med Spa"},
			"service_categories": [
				{
					"name": "Wrinkle Relaxers",
					"items": [
						{
							"id": 18430,
							"name": "Tox (Botox, Jeuveau)",
							"description": "Soften fine lines",
							"duration_minutes": 30,
							"price": "$12/unit",
							"price_type": "variable",
							"providers": [
								{"id": 33950, "name": "Brandi Sesock"},
								{"id": 38627, "name": "Gale Tesar"}
							]
						}
					]
				},
				{
					"name": "Fillers",
					"items": [
						{
							"id": 20425,
							"name": "Lip Filler",
							"description": "Full lips",
							"duration_minutes": 45,
							"price": "$650",
							"price_type": "fixed",
							"providers": [
								{"id": 33950, "name": "Brandi Sesock"}
							]
						}
					]
				}
			],
			"providers": [
				{"id": 33950, "name": "Brandi Sesock", "title": "Nurse Practitioner", "bio": "Expert injector"},
				{"id": 38627, "name": "Gale Tesar", "title": "RN", "bio": "Aesthetic nurse"}
			]
		}
	}`)

	sk, err := parseMoxieBookingJSON(fixture, "org-test")
	if err != nil {
		t.Fatal(err)
	}

	if sk.OrgID != "org-test" {
		t.Errorf("OrgID = %q", sk.OrgID)
	}

	// Services
	if len(sk.Sections.Services.Items) != 2 {
		t.Fatalf("expected 2 services, got %d", len(sk.Sections.Services.Items))
	}
	tox := sk.Sections.Services.Items[0]
	if tox.Name != "Tox (Botox, Jeuveau)" {
		t.Errorf("service name = %q", tox.Name)
	}
	if tox.Category != "Wrinkle Relaxers" {
		t.Errorf("category = %q", tox.Category)
	}
	if tox.BookingID != "18430" {
		t.Errorf("bookingID = %q", tox.BookingID)
	}
	if len(tox.ProviderIDs) != 2 {
		t.Errorf("provider count = %d", len(tox.ProviderIDs))
	}
	if tox.Order != 1 {
		t.Errorf("order = %d", tox.Order)
	}

	lip := sk.Sections.Services.Items[1]
	if lip.Name != "Lip Filler" {
		t.Errorf("service name = %q", lip.Name)
	}
	if lip.Order != 2 {
		t.Errorf("order = %d", lip.Order)
	}

	// Providers
	if len(sk.Sections.Providers.Items) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(sk.Sections.Providers.Items))
	}
	if sk.Sections.Providers.Items[0].Name != "Brandi Sesock" {
		t.Errorf("provider name = %q", sk.Sections.Providers.Items[0].Name)
	}
	if sk.Sections.Providers.Items[0].Title != "Nurse Practitioner" {
		t.Errorf("provider title = %q", sk.Sections.Providers.Items[0].Title)
	}
}

func TestParseMoxieBookingJSON_Empty(t *testing.T) {
	fixture := []byte(`{"pageProps": {"medspa": {"id": 1}, "service_categories": [], "providers": []}}`)
	sk, err := parseMoxieBookingJSON(fixture, "org-empty")
	if err != nil {
		t.Fatal(err)
	}
	if len(sk.Sections.Services.Items) != 0 {
		t.Errorf("expected 0 services, got %d", len(sk.Sections.Services.Items))
	}
}
