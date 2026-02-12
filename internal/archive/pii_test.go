package archive

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHashPhone(t *testing.T) {
	h1 := HashPhone("+15005550002")
	h2 := HashPhone("+15005550002")
	h3 := HashPhone("+15551234567")

	assert.Equal(t, h1, h2, "same input should produce same hash")
	assert.NotEqual(t, h1, h3, "different input should produce different hash")
	assert.Len(t, h1, 64, "SHA-256 hex should be 64 chars")
}

func TestScrubPII(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"email", "contact me at john@example.com please", "contact me at [EMAIL] please"},
		{"phone", "call me at (330) 333-2654", "call me at[PHONE]"},
		{"phone with plus", "my number is +15005550002", "my number is [PHONE]"},
		{"both", "email: a@b.com phone: 330-333-2654", "email: [EMAIL] phone:[PHONE]"},
		{"no pii", "I want to book Botox", "I want to book Botox"},
		{"name kept", "My name is Sarah Lee", "My name is Sarah Lee"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, ScrubPII(tt.input))
		})
	}
}

func TestScrubMessages(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "my email is test@test.com", Timestamp: time.Now()},
		{Role: "assistant", Content: "Got it!", Timestamp: time.Now()},
	}
	ScrubMessages(msgs)
	assert.Equal(t, "my email is [EMAIL]", msgs[0].Content)
	assert.Equal(t, "Got it!", msgs[1].Content)
}
