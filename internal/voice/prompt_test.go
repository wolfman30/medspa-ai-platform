package voice

import (
	"context"
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
					"lip filler":       "20425",
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

func TestBuildVoiceSystemPrompt_DepositLanguageAndFlow(t *testing.T) {
	orgID := "org-prompt-test"
	cfg := clinic.DefaultConfig(orgID)
	cfg.Name = "Prompt Medspa"
	cfg.DepositAmountCents = 7500

	store := setupClinicStore(t, cfg)
	prompt := BuildVoiceSystemPrompt(slog.Default(), store, orgID, "")

	if !strings.Contains(prompt, "I'm texting you") {
		t.Fatalf("expected prompt to contain \"I'm texting you\", got: %q", prompt)
	}
	if strings.Contains(prompt, "after we hang up") {
		t.Fatalf("prompt should not contain outdated wording \"after we hang up\": %q", prompt)
	}
	if !strings.Contains(prompt, "stay on the line") {
		t.Fatalf("expected prompt to contain \"stay on the line\", got: %q", prompt)
	}
	if !strings.Contains(prompt, "75 dollar deposit") {
		t.Fatalf("expected prompt to contain configured deposit amount, got: %q", prompt)
	}
	if !strings.Contains(prompt, "ONE QUESTION AT A TIME") {
		t.Fatalf("expected one-question-at-a-time instruction, got: %q", prompt)
	}
}

func TestBuildVoiceSystemPrompt_DefaultStillEnforcesOneQuestionAtATime(t *testing.T) {
	prompt := BuildVoiceSystemPrompt(slog.Default(), nil, "", "")
	if !strings.Contains(prompt, "ONE QUESTION AT A TIME") {
		t.Fatalf("expected one-question-at-a-time instruction in default prompt, got: %q", prompt)
	}
}

func TestBuildVoiceSystemPrompt_UsesClinicConfigFromStore(t *testing.T) {
	orgID := "org-prompt-clinic"
	cfg := clinic.DefaultConfig(orgID)
	cfg.Name = "Textual Glow"
	cfg.DepositAmountCents = 6400

	store := setupClinicStore(t, cfg)

	// sanity check that config can be loaded in test setup path
	loaded, err := store.Get(context.Background(), orgID)
	if err != nil {
		t.Fatalf("store.Get() failed: %v", err)
	}
	if loaded.Name != "Textual Glow" {
		t.Fatalf("loaded config name = %q, want %q", loaded.Name, "Textual Glow")
	}

	prompt := BuildVoiceSystemPrompt(slog.Default(), store, orgID, "")
	if !strings.Contains(prompt, "Textual Glow") {
		t.Fatalf("expected prompt to include clinic name from config, got: %q", prompt)
	}
	if !strings.Contains(prompt, "64 dollar deposit") {
		t.Fatalf("expected prompt to include deposit from config, got: %q", prompt)
	}
}
