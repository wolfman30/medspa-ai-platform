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
