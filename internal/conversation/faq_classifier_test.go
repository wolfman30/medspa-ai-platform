package conversation

import (
	"context"
	"strings"
	"testing"
)

// mockLLMClient for testing the FAQ classifier
type mockFAQClassifierLLMClient struct {
	response string
}

func (m *mockFAQClassifierLLMClient) Complete(ctx context.Context, req LLMRequest) (LLMResponse, error) {
	return LLMResponse{Text: m.response}, nil
}

func TestFAQClassifier_ClassifyQuestion(t *testing.T) {
	tests := []struct {
		name         string
		question     string
		llmResponse  string
		wantCategory FAQCategory
	}{
		{
			name:         "hylenex vs fillers",
			question:     "What's the difference between dermal fillers and Hylenex?",
			llmResponse:  `{"category": "hylenex_vs_fillers"}`,
			wantCategory: FAQCategoryHylenexVsFillers,
		},
		{
			name:         "botox vs fillers",
			question:     "What's the difference between Botox and fillers?",
			llmResponse:  `{"category": "botox_vs_fillers"}`,
			wantCategory: FAQCategoryBotoxVsFillers,
		},
		{
			name:         "filler dissolve phrasing",
			question:     `What's the difference between dermal fillers and "fillers dissolve/Hylenex"`,
			llmResponse:  `{"category": "hylenex_vs_fillers"}`,
			wantCategory: FAQCategoryHylenexVsFillers,
		},
		{
			name:         "other question",
			question:     "How much does a consultation cost?",
			llmResponse:  `{"category": "other"}`,
			wantCategory: FAQCategoryOther,
		},
		{
			name:         "hydrafacial comparison",
			question:     "HydraFacial vs DiamondGlow - which is better?",
			llmResponse:  `{"category": "hydrafacial_vs_diamondglow"}`,
			wantCategory: FAQCategoryHydraFacialVsDiamondGlow,
		},
		{
			name:         "invalid JSON falls through",
			question:     "Some random question",
			llmResponse:  `I don't understand`,
			wantCategory: FAQCategoryOther,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &mockFAQClassifierLLMClient{response: tt.llmResponse}
			classifier := NewFAQClassifier(client)

			category, err := classifier.ClassifyQuestion(context.Background(), tt.question)
			if err != nil {
				t.Errorf("ClassifyQuestion() error = %v", err)
				return
			}
			if category != tt.wantCategory {
				t.Errorf("ClassifyQuestion() = %v, want %v", category, tt.wantCategory)
			}
		})
	}
}

func TestFAQClassifier_ClassifyAndRespond(t *testing.T) {
	tests := []struct {
		name        string
		question    string
		llmResponse string
		wantEmpty   bool
		wantContain string
	}{
		{
			name:        "hylenex gets cached response",
			question:    "Filler vs Hylenex?",
			llmResponse: `{"category": "hylenex_vs_fillers"}`,
			wantEmpty:   false,
			wantContain: "DISSOLVES",
		},
		{
			name:        "botox gets cached response",
			question:    "Botox vs fillers?",
			llmResponse: `{"category": "botox_vs_fillers"}`,
			wantEmpty:   false,
			wantContain: "Botox relaxes muscles",
		},
		{
			name:        "other falls through",
			question:    "Book me an appointment",
			llmResponse: `{"category": "other"}`,
			wantEmpty:   true,
			wantContain: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &mockFAQClassifierLLMClient{response: tt.llmResponse}
			classifier := NewFAQClassifier(client)

			response, err := classifier.ClassifyAndRespond(context.Background(), tt.question)
			if err != nil {
				t.Errorf("ClassifyAndRespond() error = %v", err)
				return
			}
			if tt.wantEmpty && response != "" {
				t.Errorf("ClassifyAndRespond() = %v, want empty", response)
			}
			if !tt.wantEmpty && response == "" {
				t.Error("ClassifyAndRespond() = empty, want non-empty")
			}
			if tt.wantContain != "" && !strings.Contains(response, tt.wantContain) {
				t.Errorf("ClassifyAndRespond() should contain %q", tt.wantContain)
			}
		})
	}
}
