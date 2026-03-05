package voice

import (
	"encoding/binary"
	"testing"
)

func TestNormalizeTextForTTS(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"dollar amount", "That costs $50 today", "That costs 50 dollars today"},
		{"one dollar", "Just $1", "Just 1 dollar"},
		{"dollar cents", "Total is $19.99", "Total is 19.99 dollars"},
		{"dollar comma", "It's $1,500", "It's 1500 dollars"},
		{"time on hour", "Come at 3:00 PM", "Come at 3 PM"},
		{"time with minutes", "Meet at 3:30 pm", "Meet at 3 30 PM"},
		{"percent", "Save 20% today", "Save 20 percent today"},
		{"no match", "Hello world", "Hello world"},
		{"combined", "Save 15% on $50 at 2:00 PM", "Save 15 percent on 50 dollars at 2 PM"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeTextForTTS(tt.in)
			if got != tt.want {
				t.Errorf("NormalizeTextForTTS(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestElevenLabsConfigDefaults(t *testing.T) {
	cfg := ElevenLabsConfig{APIKey: "test-key"}
	cfg.applyDefaults()

	if cfg.VoiceID != DefaultVoiceID {
		t.Errorf("VoiceID = %q, want %q", cfg.VoiceID, DefaultVoiceID)
	}
	if cfg.ModelID != DefaultModelID {
		t.Errorf("ModelID = %q, want %q", cfg.ModelID, DefaultModelID)
	}
	if cfg.Stability != DefaultStability {
		t.Errorf("Stability = %f, want %f", cfg.Stability, DefaultStability)
	}
	if cfg.SimilarityBoost != DefaultSimilarity {
		t.Errorf("SimilarityBoost = %f, want %f", cfg.SimilarityBoost, DefaultSimilarity)
	}
	if cfg.OutputSampleRate != 8000 {
		t.Errorf("OutputSampleRate = %d, want 8000", cfg.OutputSampleRate)
	}
}

func TestElevenLabsConfigPreserved(t *testing.T) {
	cfg := ElevenLabsConfig{
		APIKey:           "key",
		VoiceID:          "custom-voice",
		ModelID:          "eleven_v3",
		Stability:        0.8,
		SimilarityBoost:  0.9,
		Speed:            1.5,
		OutputSampleRate: 16000,
	}
	cfg.applyDefaults()

	if cfg.VoiceID != "custom-voice" {
		t.Errorf("VoiceID overwritten: %q", cfg.VoiceID)
	}
	if cfg.ModelID != "eleven_v3" {
		t.Errorf("ModelID overwritten: %q", cfg.ModelID)
	}
	if cfg.OutputSampleRate != 16000 {
		t.Errorf("OutputSampleRate overwritten: %d", cfg.OutputSampleRate)
	}
}

func TestNewElevenLabsClientRequiresKey(t *testing.T) {
	_, err := NewElevenLabsClient(ElevenLabsConfig{}, nil)
	if err == nil {
		t.Error("expected error for empty API key")
	}
}

func TestNewElevenLabsClientOK(t *testing.T) {
	c, err := NewElevenLabsClient(ElevenLabsConfig{APIKey: "test"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("client is nil")
	}
}

func TestResampleLinear16SameRate(t *testing.T) {
	input := make([]byte, 100)
	out := ResampleLinear16(input, 8000, 8000)
	if len(out) != len(input) {
		t.Errorf("same-rate resample changed length: %d → %d", len(input), len(out))
	}
}

func TestResampleLinear16Downsample(t *testing.T) {
	// Create 16 samples at "16kHz" → should become ~8 samples at "8kHz"
	numSamples := 16
	input := make([]byte, numSamples*2)
	for i := 0; i < numSamples; i++ {
		binary.LittleEndian.PutUint16(input[i*2:], uint16(int16(i*1000)))
	}

	out := ResampleLinear16(input, 16000, 8000)
	outSamples := len(out) / 2
	if outSamples != 8 {
		t.Errorf("expected 8 output samples, got %d", outSamples)
	}
}

func TestResampleLinear16Empty(t *testing.T) {
	out := ResampleLinear16(nil, 16000, 8000)
	if len(out) != 0 {
		t.Errorf("expected empty output, got %d bytes", len(out))
	}
}
