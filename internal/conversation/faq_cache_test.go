package conversation

import (
	"strings"
	"testing"
)

func TestCheckFAQCache_HydraFacialVsDiamondGlow(t *testing.T) {
	tests := []struct {
		name      string
		message   string
		wantMatch bool
	}{
		{
			name:      "direct comparison question",
			message:   "What's the difference between a HydraFacial and a DiamondGlow?",
			wantMatch: true,
		},
		{
			name:      "sensitive skin variant",
			message:   "What's the difference between a HydraFacial and a DiamondGlow? I have sensitive skin and want to know which one would be better for me.",
			wantMatch: true,
		},
		{
			name:      "reversed order",
			message:   "Is DiamondGlow better than HydraFacial for my skin type?",
			wantMatch: true,
		},
		{
			name:      "hydrafacial only",
			message:   "What is a HydraFacial?",
			wantMatch: false,
		},
		{
			name:      "unrelated question",
			message:   "How much does Botox cost?",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, found := CheckFAQCache(tt.message)
			if found != tt.wantMatch {
				t.Errorf("CheckFAQCache() found = %v, want %v", found, tt.wantMatch)
			}
			if tt.wantMatch && response == "" {
				t.Error("CheckFAQCache() returned empty response for match")
			}
			if tt.wantMatch {
				// Verify response mentions both treatments
				if !strings.Contains(response, "HydraFacial") || !strings.Contains(response, "DiamondGlow") {
					t.Error("Response should mention both HydraFacial and DiamondGlow")
				}
			}
		})
	}
}

func TestCheckFAQCache_BotoxVsFillers(t *testing.T) {
	tests := []struct {
		name      string
		message   string
		wantMatch bool
	}{
		{
			name:      "botox vs filler",
			message:   "What's the difference between Botox and fillers?",
			wantMatch: true,
		},
		{
			name:      "filler vs botox reversed",
			message:   "Should I get filler or Botox?",
			wantMatch: true,
		},
		{
			name:      "botox only",
			message:   "How does Botox work?",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, found := CheckFAQCache(tt.message)
			if found != tt.wantMatch {
				t.Errorf("CheckFAQCache() found = %v, want %v", found, tt.wantMatch)
			}
			if tt.wantMatch && response == "" {
				t.Error("CheckFAQCache() returned empty response for match")
			}
		})
	}
}

func TestIsServiceComparisonQuestion(t *testing.T) {
	tests := []struct {
		message string
		want    bool
	}{
		{"What's the difference between HydraFacial and DiamondGlow?", true},
		{"Is Botox vs Dysport better?", true},
		{"Which one should I choose?", true},
		{"What is a HydraFacial?", false},
		{"Book me an appointment", false},
		{"HydraFacial or DiamondGlow for sensitive skin?", true},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			got := IsServiceComparisonQuestion(tt.message)
			if got != tt.want {
				t.Errorf("IsServiceComparisonQuestion(%q) = %v, want %v", tt.message, got, tt.want)
			}
		})
	}
}

func TestCheckFAQCache_HylenexVsFillers(t *testing.T) {
	tests := []struct {
		name      string
		message   string
		wantMatch bool
		wantTopic string // What topic the response should mention
	}{
		{
			name:      "hylenex vs filler direct",
			message:   "What's the difference between Hylenex and fillers?",
			wantMatch: true,
			wantTopic: "Hylenex",
		},
		{
			name:      "filler dissolve question",
			message:   "What's the difference between dermal fillers and filler dissolve?",
			wantMatch: true,
			wantTopic: "DISSOLVES",
		},
		{
			name:      "filler vs hylenex reversed",
			message:   "filler vs hylenex - what's the difference?",
			wantMatch: true,
			wantTopic: "Hylenex",
		},
		{
			name:      "user exact query",
			message:   `What's the difference between dermal fillers and "fillers dissolve/Hylenex" ?`,
			wantMatch: true,
			wantTopic: "Hylenex",
		},
		{
			name:      "should not match botox vs filler",
			message:   "What's the difference between Botox and fillers?",
			wantMatch: true,
			wantTopic: "Botox", // Should return Botox FAQ, not Hylenex
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, found := CheckFAQCache(tt.message)
			if found != tt.wantMatch {
				t.Errorf("CheckFAQCache() found = %v, want %v", found, tt.wantMatch)
			}
			if tt.wantMatch && response == "" {
				t.Error("CheckFAQCache() returned empty response for match")
			}
			if tt.wantMatch && !strings.Contains(response, tt.wantTopic) {
				t.Errorf("Response should mention %q but got: %s", tt.wantTopic, response)
			}
		})
	}
}

func TestSanitizeSMSResponse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "removes bold markers",
			input: "**Botox** relaxes muscles to smooth wrinkles.",
			want:  "Botox relaxes muscles to smooth wrinkles.",
		},
		{
			name:  "removes multiple bold markers",
			input: "**Fillers** add volume while **Botox** relaxes.",
			want:  "Fillers add volume while Botox relaxes.",
		},
		{
			name:  "removes italic markers",
			input: "Results *typically* last 3-4 months.",
			want:  "Results typically last 3-4 months.",
		},
		{
			name:  "removes bullet points but keeps newlines",
			input: "Options:\n- Botox\n- Fillers\n- Peels",
			want:  "Options:\nBotox\nFillers\nPeels",
		},
		{
			name:  "removes numbered lists but keeps newlines",
			input: "Steps:\n1. Consultation\n2. Treatment\n3. Follow-up",
			want:  "Steps:\nConsultation\nTreatment\nFollow-up",
		},
		{
			name:  "preserves normal text",
			input: "Would you like to schedule a consultation?",
			want:  "Would you like to schedule a consultation?",
		},
		{
			name:  "cleans double spaces",
			input: "Hello  world   test",
			want:  "Hello world test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeSMSResponse(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeSMSResponse() = %q, want %q", got, tt.want)
			}
		})
	}
}
