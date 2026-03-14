// system_prompt.go contains the builder logic that assembles the final system prompt
// from templates defined in system_prompt_templates.go. It handles deposit amounts,
// time-of-day context, Moxie/Boulevard provider info, and service highlights.
package conversation

import (
	"fmt"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
)

var serviceHighlightTemplates = map[string]string{
	"perfect derma": "SIGNATURE SERVICE: Perfect Derma Peel — a popular medium-depth chemical peel that helps brighten and smooth skin tone and texture for a fresh glow. When someone asks about chemical peels, mention Perfect Derma Peel with enthusiasm and invite them to book a consultation.",
}

// buildSystemPrompt returns the system prompt with the actual deposit amount (Square path).
// If depositCents is 0 or negative, it defaults to $50.
// If usesMoxie is true, it appends Moxie-specific booking instructions that override the
// Square deposit flow. Moxie clinics do NOT use Square — the patient completes payment
// directly on Moxie's Step 5 payment page.
func buildSystemPrompt(depositCents int, usesMoxie bool, cfg ...*clinic.Config) string {
	if depositCents <= 0 {
		depositCents = 5000 // default $50
	}
	depositDollars := fmt.Sprintf("$%d", depositCents/100)
	// Replace all instances of $50 with the actual deposit amount
	prompt := strings.ReplaceAll(defaultSystemPrompt, "$50", depositDollars)

	// Inject current clinic-local time for time-aware greetings
	if len(cfg) > 0 && cfg[0] != nil {
		tz := ClinicLocation(cfg[0].Timezone)
		now := time.Now().In(tz)
		hour := now.Hour()
		timeStr := now.Format("3:04 PM MST")
		dayStr := now.Format("Monday")

		var timeContext string
		if hour >= 7 && hour < 21 { // 7 AM - 8:59 PM
			timeContext = fmt.Sprintf(
				"\n\n⏰ CURRENT TIME: %s (%s). The clinic is within normal business hours. "+
					"In your greeting, say providers are currently with patients or busy with appointments.",
				timeStr, dayStr)
		} else {
			timeContext = fmt.Sprintf(
				"\n\n⏰ CURRENT TIME: %s (%s). The clinic is CLOSED right now (after hours). "+
					"In your greeting, do NOT say providers are with patients. Instead say something like: "+
					"\"Hi! This is [Clinic]'s AI assistant. We're currently closed, but I can help you get started "+
					"with booking an appointment. What treatment are you interested in?\"",
				timeStr, dayStr)
		}
		prompt += timeContext
	}

	// Append Moxie-specific instructions if clinic uses Moxie booking
	if usesMoxie {
		prompt += moxieSystemPromptAddendum
	}

	// Append per-service provider info if available
	if len(cfg) > 0 && cfg[0] != nil && cfg[0].MoxieConfig != nil {
		mc := cfg[0].MoxieConfig
		if mc.ServiceProviderCount != nil && mc.ProviderNames != nil && mc.ServiceMenuItems != nil {
			// Build a reverse map: serviceMenuItemId → service name
			idToName := make(map[string]string)
			for name, id := range mc.ServiceMenuItems {
				idToName[id] = name
			}
			var providerInfo strings.Builder
			providerInfo.WriteString("\n\n📋 SERVICE PROVIDER INFO — IMPORTANT:\n")
			hasMulti := false
			for itemID, count := range mc.ServiceProviderCount {
				svcName := idToName[itemID]
				if svcName == "" {
					continue
				}
				if count > 1 {
					hasMulti = true
					providerInfo.WriteString(fmt.Sprintf("- %s: %d providers\n", strings.Title(svcName), count))
				}
			}
			if hasMulti && len(mc.ProviderNames) > 0 {
				providerInfo.WriteString("Available providers: ")
				names := make([]string, 0, len(mc.ProviderNames))
				for _, name := range mc.ProviderNames {
					names = append(names, name)
				}
				providerInfo.WriteString(strings.Join(names, ", "))
				providerInfo.WriteString("\n")
				providerInfo.WriteString("\n🚨 PROVIDER PREFERENCE RULE:\n")
				providerInfo.WriteString("For services with MULTIPLE providers listed above, you MUST ask the patient:\n")
				providerInfo.WriteString("\"Do you have a preferred provider? We have [names]. Or no preference is totally fine!\"\n")
				providerInfo.WriteString("Ask this BEFORE asking for email. Do NOT skip this step.\n")
				providerInfo.WriteString("For services with only 1 provider (not listed above), do NOT ask.\n")
			}
			prompt += providerInfo.String()
		}
	}

	// Boulevard clinics: add provider preference from ProviderNames in clinic config
	if len(cfg) > 0 && cfg[0] != nil && cfg[0].UsesBoulevardBooking() && len(cfg[0].ProviderNames) > 0 {
		var blvdInfo strings.Builder
		blvdInfo.WriteString("\n\n📋 PROVIDER INFO — IMPORTANT:\n")
		blvdInfo.WriteString("Available providers at this clinic:\n")
		for _, name := range cfg[0].ProviderNames {
			blvdInfo.WriteString(fmt.Sprintf("- %s\n", name))
		}
		blvdInfo.WriteString("\n🚨 PROVIDER PREFERENCE RULE:\n")
		blvdInfo.WriteString("For ALL services, you MUST ask the patient:\n")
		blvdInfo.WriteString("\"Do you have a preferred provider? We have [names]. Or no preference is totally fine!\"\n")
		blvdInfo.WriteString("Ask this BEFORE checking availability. Do NOT skip this step.\n")
		prompt += blvdInfo.String()
	}

	return prompt
}

func buildServiceHighlightsContext(cfg *clinic.Config, query string) string {
	if cfg == nil {
		return ""
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" || !strings.Contains(query, "peel") {
		return ""
	}
	if clinicHasService(cfg, "perfect derma") {
		return serviceHighlightTemplates["perfect derma"]
	}
	return ""
}

func clinicHasService(cfg *clinic.Config, needle string) bool {
	if cfg == nil {
		return false
	}
	needle = strings.ToLower(strings.TrimSpace(needle))
	if needle == "" {
		return false
	}
	for _, svc := range cfg.Services {
		if strings.Contains(strings.ToLower(svc), needle) {
			return true
		}
	}
	for key := range cfg.ServicePriceText {
		if strings.Contains(strings.ToLower(key), needle) {
			return true
		}
	}
	for key := range cfg.ServiceDepositAmountCents {
		if strings.Contains(strings.ToLower(key), needle) {
			return true
		}
	}
	for _, svc := range cfg.AIPersona.SpecialServices {
		if strings.Contains(strings.ToLower(svc), needle) {
			return true
		}
	}
	return false
}
