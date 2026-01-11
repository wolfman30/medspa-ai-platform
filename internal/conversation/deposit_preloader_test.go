package conversation

import "testing"

func TestShouldPreloadDeposit(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		expected bool
	}{
		// Positive cases - should trigger preload
		{
			name:     "explicit deposit agreement",
			message:  "Yes, I'm happy to pay the deposit to secure my appointment",
			expected: true,
		},
		{
			name:     "affirmative with payment keyword",
			message:  "Sure, I'll pay for it",
			expected: true,
		},
		{
			name:     "secure spot language",
			message:  "Yes, let's secure my spot",
			expected: true,
		},
		{
			name:     "proceed with payment",
			message:  "Absolutely, proceed with the payment",
			expected: true,
		},
		{
			name:     "ready to pay",
			message:  "Ok, I'm ready to pay the deposit",
			expected: true,
		},
		{
			name:     "confirm appointment",
			message:  "Yes! Let's confirm the appointment",
			expected: true,
		},

		// Negative cases - should NOT trigger preload
		{
			name:     "asking about deposit",
			message:  "What's the deposit amount?",
			expected: false,
		},
		{
			name:     "declining deposit",
			message:  "No thanks, I don't want to pay a deposit",
			expected: false,
		},
		{
			name:     "maybe later",
			message:  "Maybe later, I need to think about it",
			expected: false,
		},
		{
			name:     "general question",
			message:  "What services do you offer?",
			expected: false,
		},
		{
			name:     "just affirmative without deposit context",
			message:  "Yes, that sounds great",
			expected: false,
		},
		{
			name:     "scheduling without payment",
			message:  "Friday at 2pm works for me",
			expected: false,
		},
		{
			name:     "empty message",
			message:  "",
			expected: false,
		},
		{
			name:     "skip payment",
			message:  "Yes but skip the deposit for now",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldPreloadDeposit(tt.message)
			if result != tt.expected {
				t.Errorf("ShouldPreloadDeposit(%q) = %v, want %v", tt.message, result, tt.expected)
			}
		})
	}
}
