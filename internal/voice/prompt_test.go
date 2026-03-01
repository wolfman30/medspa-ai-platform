package voice

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
)

func TestBuildServiceAliasSection(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		result := buildServiceAliasSection(nil)
		if result != "" {
			t.Errorf("expected empty string for nil config, got %q", result)
		}
	})

	t.Run("empty aliases", func(t *testing.T) {
		cfg := &clinic.Config{}
		result := buildServiceAliasSection(cfg)
		if result != "" {
			t.Errorf("expected empty string for empty aliases, got %q", result)
		}
	})

	t.Run("with aliases", func(t *testing.T) {
		cfg := &clinic.Config{
			ServiceAliases: map[string]string{
				"botox": "Wrinkle Relaxers",
				"tox":   "Wrinkle Relaxers",
			},
		}
		result := buildServiceAliasSection(cfg)
		if !strings.Contains(result, "SERVICE NAME MAPPINGS") {
			t.Error("expected SERVICE NAME MAPPINGS header")
		}
		if !strings.Contains(result, "botox") {
			t.Error("expected 'botox' alias in output")
		}
		if !strings.Contains(result, "Wrinkle Relaxers") {
			t.Error("expected 'Wrinkle Relaxers' in output")
		}
	})
}

func TestBuildAvailableServicesSection(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		result := buildAvailableServicesSection(nil)
		if result != "" {
			t.Errorf("expected empty string, got %q", result)
		}
	})

	t.Run("no moxie config", func(t *testing.T) {
		cfg := &clinic.Config{}
		result := buildAvailableServicesSection(cfg)
		if result != "" {
			t.Errorf("expected empty string, got %q", result)
		}
	})

	t.Run("with services", func(t *testing.T) {
		cfg := &clinic.Config{
			MoxieConfig: &clinic.MoxieConfig{
				ServiceMenuItems: map[string]string{
					"wrinkle relaxers": "20424",
					"lip filler":      "20425",
				},
			},
		}
		result := buildAvailableServicesSection(cfg)
		if !strings.Contains(result, "AVAILABLE BOOKABLE SERVICES") {
			t.Error("expected AVAILABLE BOOKABLE SERVICES header")
		}
		if !strings.Contains(result, "wrinkle relaxers") {
			t.Error("expected 'wrinkle relaxers' in output")
		}
		if !strings.Contains(result, "lip filler") {
			t.Error("expected 'lip filler' in output")
		}
	})
}

func TestBuildVoiceSystemPrompt_IncludesAliases(t *testing.T) {
	// BuildVoiceSystemPrompt with nil store should still work (no aliases)
	prompt := BuildVoiceSystemPrompt(slog.Default(), nil, "", "")
	if strings.Contains(prompt, "SERVICE NAME MAPPINGS") {
		t.Error("should not contain alias section when no store provided")
	}
}
