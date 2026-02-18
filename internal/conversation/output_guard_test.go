package conversation

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScanOutputForLeaks(t *testing.T) {
	tests := []struct {
		name       string
		reply      string
		wantLeak   bool
		wantReason string
	}{
		// Safe replies
		{"normal booking reply", "Great! I found available times for Botox. Would Tuesday at 2pm work?", false, ""},
		{"price info", "Botox starts at $12 per unit. Would you like to schedule a consultation?", false, ""},
		{"deposit offer", "To secure your appointment, we collect a $50 refundable deposit. Would you like to proceed?", false, ""},
		{"empty reply", "", false, ""},

		// System prompt leaks
		{"discloses prompt", "My system prompt says I should help with appointments", true, "leak:system_prompt"},
		{"discloses instructions", "My instructions are to collect patient name and service", true, "leak:instructions_disclosure"},
		{"programmed to", "I'm programmed to only discuss appointment scheduling", true, "leak:programming_disclosure"},
		{"lists rules", "Here are my instructions: 1. Collect name 2. Collect service", true, "leak:rules_listing"},

		// AI identity
		{"says I am AI", "I'm an AI assistant, but I can help you book an appointment!", true, "leak:ai_identity"},
		{"mentions Claude", "I'm powered by Claude from Anthropic", true, "leak:tech_stack"},
		{"mentions GPT", "This is built on GPT technology", true, "leak:tech_stack"},

		// Credential leaks
		{"stripe key", "The key is sk-test-abc123def456ghi789jkl012mno", true, "leak:stripe_key"},
		{"AWS key", "The access key is AKIAWEQRR2HAQRVHRLTL", true, "leak:aws_key"},
		{"database URL", "Our database is at postgres://user:pass@host:5432/db", true, "leak:database_url"},
		{"API key in text", "The api_key: abc123def456", true, "leak:credential"},

		// Internal URLs
		{"dev API URL", "You can check at api-dev.aiwolfsolutions.com", true, "leak:internal_url"},
		{"admin path", "Go to /admin/clinics to see the config", true, "leak:internal_path"},
		{"webhooks path", "The endpoint is /webhooks/telnyx/messages", true, "leak:internal_path"},

		// Other patient data
		{"references other patient", "Another patient's appointment is at 3pm", true, "leak:other_patient_ref"},

		// Drug names (carrier spam filter)
		{"mentions semaglutide", "We offer semaglutide for weight loss", true, "spam:drug_name"},
		{"mentions GLP-1", "Our GLP-1 program helps patients lose weight", true, "spam:drug_name"},
		{"mentions glp1 no hyphen", "We use glp1 agonists", true, "spam:drug_name"},
		{"mentions ozempic", "Have you heard of Ozempic?", true, "spam:drug_name"},
		{"mentions wegovy", "Wegovy is another option", true, "spam:drug_name"},
		{"mentions tirzepatide", "Tirzepatide has shown great results", true, "spam:drug_name"},
		{"mentions mounjaro", "Mounjaro is available here", true, "spam:drug_name"},
		{"weight loss without drug names", "We offer medically supervised weight loss programs. Would you like to schedule a consultation?", false, ""},

		// Post-procedure symptom minimization
		{"says that's normal", "Bruising after Botox? That's completely normal and should resolve in a few days.", true, "safety:symptom_minimization"},
		{"says nothing to worry", "That is nothing to worry about, it happens sometimes.", true, "safety:symptom_minimization"},
		{"says perfectly normal", "Some swelling is perfectly normal after filler.", true, "safety:symptom_minimization"},
		{"proper post-procedure response", "I'd recommend reaching out to the clinic so your provider can take a look.", false, ""},

		// Edge cases — should NOT trigger
		{"mentions 'system' normally", "Our online booking system is easy to use", false, ""},
		{"mentions 'rules' normally", "Our cancellation rules require 24 hours notice", false, ""},
		{"mentions 'instructions' normally", "Post-treatment instructions will be provided at your visit", false, ""},
		{"ip-like but not really", "Call us at 937-896-2713 for more info", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ScanOutputForLeaks(tt.reply)
			if tt.wantLeak {
				assert.True(t, result.Leaked, "expected leak detection for: %s", tt.reply)
				if tt.wantReason != "" {
					found := false
					for _, r := range result.Reasons {
						if reasonContains(r, tt.wantReason) {
							found = true
							break
						}
					}
					assert.True(t, found, "expected reason containing %q in %v", tt.wantReason, result.Reasons)
				}
			} else {
				assert.False(t, result.Leaked, "expected NO leak for: %s (reasons: %v)", tt.reply, result.Reasons)
			}
		})
	}
}
