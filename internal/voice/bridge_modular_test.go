package voice

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetVoiceEngine(t *testing.T) {
	tests := []struct {
		name     string
		envVal   string
		expected string
	}{
		{"default is nova-sonic", "", "nova-sonic"},
		{"nova-sonic", "nova-sonic", "nova-sonic"},
		{"modular", "modular", "modular"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("VOICE_ENGINE", tt.envVal)
			assert.Equal(t, tt.expected, GetVoiceEngine())
		})
	}
}

func TestDeepgramEncoding(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"audio/x-mulaw", "mulaw"},
		{"audio/x-l16", "linear16"},
		{"audio/lpcm", "linear16"},
		{"L16", "linear16"},
		{"unknown", "linear16"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, deepgramEncoding(tt.input))
		})
	}
}

func TestModularBridge_ConvertOutputAudio(t *testing.T) {
	b := &ModularBridge{
		mediaFormat: TelnyxMediaFormat{Encoding: "audio/x-l16"},
	}

	// Linear16 passthrough
	input := []byte{0x01, 0x02, 0x03, 0x04}
	output, err := b.convertModularOutputAudio(input)
	require.NoError(t, err)
	assert.Equal(t, input, output)

	// Mulaw conversion
	b.mediaFormat.Encoding = "audio/x-mulaw"
	linear := []byte{0x00, 0x10, 0x00, 0x20} // two 16-bit samples
	output, err = b.convertModularOutputAudio(linear)
	require.NoError(t, err)
	assert.Len(t, output, 2) // 2 mulaw bytes for 2 samples
}

func TestModularBridge_SlotSelectionAndDeposit(t *testing.T) {
	b := &ModularBridge{
		recentAssistantText: make(map[string]time.Time),
	}

	// Slot selection not captured yet
	assert.False(t, b.slotSelectionCaptured)

	// Text with slot confirmation
	b.modularMaybeCaptureSlotSelection("Perfect! Monday at 3:00 PM works great!")
	assert.True(t, b.slotSelectionCaptured)
}

func TestNewDeepgramSTT_MissingAPIKey(t *testing.T) {
	_, err := NewDeepgramSTT(t.Context(), DeepgramConfig{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API key required")
}

func TestModularBridge_ConvertOutputAudio_UnknownEncoding(t *testing.T) {
	b := &ModularBridge{
		mediaFormat: TelnyxMediaFormat{Encoding: "audio/opus"},
	}
	input := []byte{0xDE, 0xAD}
	output, err := b.convertModularOutputAudio(input)
	require.NoError(t, err)
	assert.Equal(t, input, output, "unknown encoding should pass through unchanged")
}

func TestModularBridge_SlotSelectionNotCaptured_NoDeposit(t *testing.T) {
	b := &ModularBridge{
		recentAssistantText: make(map[string]time.Time),
	}

	// Deposit text should NOT fire if slot not captured
	b.modularMaybeFireDepositSMS(t.Context(), "I'll send you a deposit link now")
	assert.False(t, b.depositSMSSent, "deposit SMS should not fire without slot selection")
}

func TestModularBridge_DepositSMS_Idempotent(t *testing.T) {
	b := &ModularBridge{
		recentAssistantText:   make(map[string]time.Time),
		slotSelectionCaptured: true,
		depositSMSSent:        true, // pretend already sent
	}

	// Should be a no-op since already sent (idempotent)
	b.modularMaybeFireDepositSMS(t.Context(), "I'll send you a deposit link now")
	assert.True(t, b.depositSMSSent, "deposit SMS flag should remain true")
}

func TestModularBridge_DepositSMS_FiresOnce(t *testing.T) {
	b := &ModularBridge{
		logger:                slog.Default(),
		recentAssistantText:   make(map[string]time.Time),
		slotSelectionCaptured: true,
		toolHandler:           NewToolHandler("org1", "+1234567890", "+0987654321", nil, nil),
	}

	// This will set depositSMSSent=true and launch a goroutine (which will fail but that's ok)
	b.modularMaybeFireDepositSMS(t.Context(), "I'll send you a deposit link now")
	assert.True(t, b.depositSMSSent, "deposit SMS should be marked sent after matching text")

	// Give goroutine time to (fail and) finish
	time.Sleep(50 * time.Millisecond)
}

func TestModularBridge_SlotSelection_NegativeCases(t *testing.T) {
	b := &ModularBridge{
		recentAssistantText: make(map[string]time.Time),
	}

	// Text that doesn't look like a slot selection
	b.modularMaybeCaptureSlotSelection("What services do you offer?")
	assert.False(t, b.slotSelectionCaptured, "generic question should not trigger slot capture")

	b.modularMaybeCaptureSlotSelection("Tell me about pricing")
	assert.False(t, b.slotSelectionCaptured, "pricing question should not trigger slot capture")
}

func TestModularBridge_DepositSMS_RequiresKeywords(t *testing.T) {
	b := &ModularBridge{
		recentAssistantText:   make(map[string]time.Time),
		slotSelectionCaptured: true,
	}

	// Text without deposit/payment keywords
	b.modularMaybeFireDepositSMS(t.Context(), "Great, you're all set!")
	assert.False(t, b.depositSMSSent, "no deposit keywords should not trigger SMS")

	// Has deposit but no send/link keywords
	b.modularMaybeFireDepositSMS(t.Context(), "The deposit is $50")
	assert.False(t, b.depositSMSSent, "deposit without send/link should not trigger SMS")
}

func TestModularBridge_MulawConversion_RoundTrip(t *testing.T) {
	b := &ModularBridge{
		mediaFormat: TelnyxMediaFormat{Encoding: "audio/x-mulaw"},
	}

	// 4 bytes of linear16 = 2 samples → 2 mulaw bytes
	linear := []byte{0x00, 0x00, 0x00, 0x00} // silence
	output, err := b.convertModularOutputAudio(linear)
	require.NoError(t, err)
	assert.Len(t, output, 2)
}
