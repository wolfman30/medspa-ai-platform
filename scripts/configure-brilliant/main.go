// configure-brilliant builds and outputs the complete clinic config JSON for
// Brilliant Aesthetics (org_id: 124a2e4a-74bd-4a3a-8f60-84444079a35a).
//
// Usage: go run scripts/configure-brilliant/main.go > brilliant-config.json
// Then apply via: PUT /admin/clinics/{orgID}/config with the JSON body.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
)

func main() {
	cfg := &clinic.Config{
		OrgID:              "124a2e4a-74bd-4a3a-8f60-84444079a35a",
		Name:               "Brilliant Aesthetics LLC",
		Timezone:           "America/New_York",
		DepositAmountCents: 5000, // $50
		BookingPlatform:    "moxie",
		BookingURL:         "https://brilliant-aesthetics.mfrn.co",
		AIPersona: clinic.AIPersona{
			ProviderName:   "Kimberly Enochs",
			IsSoloOperator: true,
			Tone:           "warm",
		},
		MoxieConfig: &clinic.MoxieConfig{
			MedspaID:          "95",
			MedspaSlug:        "brilliant-aesthetics",
			DefaultProviderID: "1434",
			ProviderNames: map[string]string{
				"1434": "Kimberly Enochs",
			},
			ServiceMenuItems: map[string]string{
				// Wrinkle Relaxers
				"lip flip":                     "6848",
				"neurotoxin first time client": "659",
				"neurotoxin returning client":  "1947",
				// Fillers
				"dermal fillers":  "664",
				"filler dissolve": "667",
				// Skincare
				"advanced chemical peel": "15233",
				"no downtime peel":       "6922",
				"skin care consultation": "7520",
				// Weight Loss
				"weight loss consultation": "22216",
				"weight loss follow up":    "22217",
				// Hormone Therapy
				"female hormone therapy consultation":     "46285",
				"female hormone therapy pellet insertion": "46288",
				"male hormone therapy consultation":       "46286",
				"male hormone therapy pellet insertion":   "46287",
				// PDO Threads
				"pdo threads smooth": "48393",
			},
			ServiceProviderCount: map[string]int{
				"6848":  1,
				"659":   1,
				"1947":  1,
				"664":   1,
				"667":   1,
				"15233": 1,
				"6922":  1,
				"7520":  1,
				"22216": 1,
				"22217": 1,
				"46285": 1,
				"46288": 1,
				"46286": 1,
				"46287": 1,
				"48393": 1,
			},
		},
		ServiceAliases: map[string]string{
			// Wrinkle Relaxers → new patients get 40-min, returning get 20-min
			// Default to first-time (659) since AI asks new/returning and can switch
			"botox":            "neurotoxin first time client",
			"dysport":          "neurotoxin first time client",
			"xeomin":           "neurotoxin first time client",
			"daxxify":          "neurotoxin first time client",
			"wrinkle relaxer":  "neurotoxin first time client",
			"wrinkle relaxers": "neurotoxin first time client",
			"neurotoxin":       "neurotoxin first time client",
			"tox":              "neurotoxin first time client",
			"neuromodulator":   "neurotoxin first time client",
			"forehead lines":   "neurotoxin first time client",
			"11 lines":         "neurotoxin first time client",
			"crow's feet":      "neurotoxin first time client",
			"crows feet":       "neurotoxin first time client",
			"frown lines":      "neurotoxin first time client",
			"bunny lines":      "neurotoxin first time client",

			// Lip flip (specific neurotoxin service)
			"lip flip": "lip flip",

			// Fillers
			"filler":           "dermal fillers",
			"fillers":          "dermal fillers",
			"dermal filler":    "dermal fillers",
			"lip filler":       "dermal fillers",
			"lip fillers":      "dermal fillers",
			"cheek filler":     "dermal fillers",
			"cheek fillers":    "dermal fillers",
			"chin filler":      "dermal fillers",
			"jawline filler":   "dermal fillers",
			"under eye filler": "dermal fillers",
			"tear trough":      "dermal fillers",
			"nasolabial folds": "dermal fillers",
			"smile lines":      "dermal fillers",
			"marionette lines": "dermal fillers",
			"juvederm":         "dermal fillers",
			"restylane":        "dermal fillers",
			"sculptra":         "dermal fillers",
			"radiesse":         "dermal fillers",
			"versa":            "dermal fillers",
			"rha":              "dermal fillers",

			// Filler dissolve
			"filler dissolve": "filler dissolve",
			"dissolve filler": "filler dissolve",
			"hylenex":         "filler dissolve",
			"hyaluronidase":   "filler dissolve",

			// Skincare
			"chemical peel":         "advanced chemical peel",
			"peel":                  "advanced chemical peel",
			"advanced peel":         "advanced chemical peel",
			"no downtime peel":      "no downtime peel",
			"light peel":            "no downtime peel",
			"gentle peel":           "no downtime peel",
			"skin consultation":     "skin care consultation",
			"skincare consultation": "skin care consultation",
			"skin care":             "skin care consultation",
			"facial":                "skin care consultation",

			// Weight Loss
			"weight loss":           "weight loss consultation",
			"weight management":     "weight loss consultation",
			"ozempic":               "weight loss consultation",
			"wegovy":                "weight loss consultation",
			"mounjaro":              "weight loss consultation",
			"semaglutide":           "weight loss consultation",
			"tirzepatide":           "weight loss consultation",
			"glp-1":                 "weight loss consultation",
			"glp1":                  "weight loss consultation",
			"skinny shot":           "weight loss consultation",
			"weight loss follow up": "weight loss follow up",

			// Hormone Therapy
			"hormone therapy":       "female hormone therapy consultation",
			"hrt":                   "female hormone therapy consultation",
			"bhrt":                  "female hormone therapy consultation",
			"bioidentical hormones": "female hormone therapy consultation",
			"pellet therapy":        "female hormone therapy pellet insertion",
			"hormone pellet":        "female hormone therapy pellet insertion",
			"testosterone":          "male hormone therapy consultation",
			"trt":                   "male hormone therapy consultation",
			"low t":                 "male hormone therapy consultation",

			// PDO Threads
			"pdo threads": "pdo threads smooth",
			"threads":     "pdo threads smooth",
			"thread lift": "pdo threads smooth",
		},
		BookingPolicies: []string{
			"A $50 deposit is required to secure your appointment.",
			"The deposit goes toward your treatment cost.",
			"Cancel 24+ hours in advance for a full refund.",
			"No-shows forfeit the deposit.",
		},
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
}
