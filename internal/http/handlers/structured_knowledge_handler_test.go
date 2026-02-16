package handlers

import (
	"testing"
)

func TestParseMoxieBookingJSON(t *testing.T) {
	fixture := []byte(`{
		"pageProps": {
			"medspaInfo": {
				"id": "1264",
				"name": "Forever 22 Med Spa",
				"userMedspas": [
					{
						"id": "33950",
						"user": {"id": "u1", "firstName": "Brandi", "lastName": "Sesock"}
					},
					{
						"id": "38627",
						"user": {"id": "u2", "firstName": "Gale", "lastName": "Tesar"}
					}
				],
				"serviceCategories": [
					{
						"name": "Wrinkle Relaxers",
						"medspaServiceMenuItems": [
							{
								"id": "18430",
								"name": "Tox (Botox, Jeuveau)",
								"description": "Soften fine lines",
								"durationInMinutes": 30,
								"price": "12.00",
								"isVariablePricing": true,
								"isAddon": false,
								"serviceMenuAdditionalPublicInfo": {
									"eligibleProvidersDetails": [
										{"userMedspa": {"id": "33950", "user": {"id": "u1", "firstName": "Brandi", "lastName": "Sesock"}}, "customDuration": 0},
										{"userMedspa": {"id": "38627", "user": {"id": "u2", "firstName": "Gale", "lastName": "Tesar"}}, "customDuration": 0}
									]
								}
							}
						]
					},
					{
						"name": "Fillers",
						"medspaServiceMenuItems": [
							{
								"id": "20425",
								"name": "Lip Filler",
								"description": "Full lips",
								"durationInMinutes": 45,
								"price": "650.00",
								"isVariablePricing": false,
								"isAddon": false,
								"serviceMenuAdditionalPublicInfo": {
									"eligibleProvidersDetails": [
										{"userMedspa": {"id": "33950", "user": {"id": "u1", "firstName": "Brandi", "lastName": "Sesock"}}, "customDuration": 0}
									]
								}
							}
						]
					}
				]
			}
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
	if tox.PriceType != "variable" {
		t.Errorf("price type = %q", tox.PriceType)
	}

	lip := sk.Sections.Services.Items[1]
	if lip.Name != "Lip Filler" {
		t.Errorf("service name = %q", lip.Name)
	}
	if lip.Order != 2 {
		t.Errorf("order = %d", lip.Order)
	}
	if lip.Price != "$650.00" {
		t.Errorf("price = %q", lip.Price)
	}

	// Providers
	if len(sk.Sections.Providers.Items) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(sk.Sections.Providers.Items))
	}
	if sk.Sections.Providers.Items[0].Name != "Brandi Sesock" {
		t.Errorf("provider name = %q", sk.Sections.Providers.Items[0].Name)
	}
}

func TestParseMoxieBookingJSON_Empty(t *testing.T) {
	fixture := []byte(`{"pageProps": {"medspaInfo": {"id": "1", "name": "Test", "userMedspas": [], "serviceCategories": []}}}`)
	sk, err := parseMoxieBookingJSON(fixture, "org-empty")
	if err != nil {
		t.Fatal(err)
	}
	if len(sk.Sections.Services.Items) != 0 {
		t.Errorf("expected 0 services, got %d", len(sk.Sections.Services.Items))
	}
}
