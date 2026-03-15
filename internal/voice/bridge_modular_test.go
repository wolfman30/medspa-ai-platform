package voice

import (
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
