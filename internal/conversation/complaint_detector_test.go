package conversation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComplaintDetector_DetectComplaint(t *testing.T) {
	detector := NewComplaintDetector(nil)

	tests := []struct {
		name          string
		message       string
		wantDetected  bool
		wantType      ComplaintType
		minConfidence float64
	}{
		// Overcharge complaints
		{
			name:          "overcharged explicit",
			message:       "I was overcharged for my appointment",
			wantDetected:  true,
			wantType:      ComplaintOvercharge,
			minConfidence: 0.8,
		},
		{
			name:          "charged too much",
			message:       "You charged me too much money",
			wantDetected:  true,
			wantType:      ComplaintOvercharge,
			minConfidence: 0.8,
		},
		{
			name:          "wrong amount",
			message:       "The wrong amount was charged to my card",
			wantDetected:  true,
			wantType:      ComplaintOvercharge,
			minConfidence: 0.7,
		},

		// Unauthorized charge complaints
		{
			name:          "didn't authorize",
			message:       "I didn't authorize this charge",
			wantDetected:  true,
			wantType:      ComplaintUnauthorized,
			minConfidence: 0.9,
		},
		{
			name:          "fraud claim",
			message:       "This is fraud, I never approved this",
			wantDetected:  true,
			wantType:      ComplaintUnauthorized,
			minConfidence: 0.85,
		},
		{
			name:          "unauthorized transaction",
			message:       "There's an unauthorized charge on my account",
			wantDetected:  true,
			wantType:      ComplaintUnauthorized,
			minConfidence: 0.9,
		},

		// Refund requests
		{
			name:          "want refund",
			message:       "I want a refund please",
			wantDetected:  true,
			wantType:      ComplaintRefundReq,
			minConfidence: 0.8,
		},
		{
			name:          "money back",
			message:       "I need my money back",
			wantDetected:  true,
			wantType:      ComplaintRefundReq,
			minConfidence: 0.8,
		},
		{
			name:          "cancel charge",
			message:       "Please cancel the charge on my card",
			wantDetected:  true,
			wantType:      ComplaintRefundReq,
			minConfidence: 0.7,
		},

		// Double charge complaints
		{
			name:          "charged twice",
			message:       "I was charged twice for the same thing",
			wantDetected:  true,
			wantType:      ComplaintDoubleCharge,
			minConfidence: 0.9,
		},
		{
			name:          "duplicate charge",
			message:       "There's a duplicate charge on my statement",
			wantDetected:  true,
			wantType:      ComplaintDoubleCharge,
			minConfidence: 0.9,
		},

		// General billing
		{
			name:          "billing question",
			message:       "I have a billing question about my last visit",
			wantDetected:  true,
			wantType:      ComplaintGeneral,
			minConfidence: 0.5,
		},

		// Non-complaints
		{
			name:         "appointment inquiry",
			message:      "When is my next appointment?",
			wantDetected: false,
		},
		{
			name:         "service question",
			message:      "What services do you offer?",
			wantDetected: false,
		},
		{
			name:         "thank you message",
			message:      "Thank you for the great service!",
			wantDetected: false,
		},
		{
			name:         "empty message",
			message:      "",
			wantDetected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detector.DetectComplaint(context.Background(), tt.message)

			assert.Equal(t, tt.wantDetected, result.Detected, "detection mismatch")

			if tt.wantDetected {
				assert.Equal(t, tt.wantType, result.Type, "complaint type mismatch")
				assert.GreaterOrEqual(t, result.Confidence, tt.minConfidence,
					"confidence too low: got %f, want >= %f", result.Confidence, tt.minConfidence)
				assert.NotEmpty(t, result.MatchedKeyword)
				assert.NotEmpty(t, result.SuggestedReply)
			}
		})
	}
}

func TestComplaintDetector_IsHighPriority(t *testing.T) {
	detector := NewComplaintDetector(nil)

	tests := []struct {
		name         string
		result       *ComplaintResult
		wantPriority bool
	}{
		{
			name: "unauthorized is high priority",
			result: &ComplaintResult{
				Detected:   true,
				Type:       ComplaintUnauthorized,
				Confidence: 0.9,
			},
			wantPriority: true,
		},
		{
			name: "high confidence double charge is high priority",
			result: &ComplaintResult{
				Detected:   true,
				Type:       ComplaintDoubleCharge,
				Confidence: 0.85,
			},
			wantPriority: true,
		},
		{
			name: "low confidence refund is not high priority",
			result: &ComplaintResult{
				Detected:   true,
				Type:       ComplaintRefundReq,
				Confidence: 0.7,
			},
			wantPriority: false,
		},
		{
			name: "not detected is not high priority",
			result: &ComplaintResult{
				Detected: false,
			},
			wantPriority: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detector.IsHighPriority(tt.result)
			assert.Equal(t, tt.wantPriority, got)
		})
	}
}

func TestComplaintDetector_GetEscalationPriority(t *testing.T) {
	detector := NewComplaintDetector(nil)

	tests := []struct {
		name     string
		result   *ComplaintResult
		expected string
	}{
		{
			name:     "not detected returns NONE",
			result:   &ComplaintResult{Detected: false},
			expected: "NONE",
		},
		{
			name: "unauthorized returns HIGH",
			result: &ComplaintResult{
				Detected:   true,
				Type:       ComplaintUnauthorized,
				Confidence: 0.9,
			},
			expected: "HIGH",
		},
		{
			name: "refund request returns MEDIUM",
			result: &ComplaintResult{
				Detected:   true,
				Type:       ComplaintRefundReq,
				Confidence: 0.8,
			},
			expected: "MEDIUM",
		},
		{
			name: "general billing returns LOW",
			result: &ComplaintResult{
				Detected:   true,
				Type:       ComplaintGeneral,
				Confidence: 0.6,
			},
			expected: "LOW",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detector.GetEscalationPriority(tt.result)
			assert.Equal(t, tt.expected, got)
		})
	}
}
