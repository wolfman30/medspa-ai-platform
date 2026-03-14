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
					"wrinkle relaxers": "1",
					"lip filler":       "2",
				},
			},
		}
		result := buildAvailableServicesSection(cfg)
		if !strings.Contains(result, "AVAILABLE SERVICES") {
			t.Error("expected AVAILABLE SERVICES header")
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
	prompt := BuildVoiceSystemPrompt(slog.Default(), nil, "")
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
	prompt := BuildVoiceSystemPrompt(slog.Default(), store, orgID)

	// Deposit flow: must require slot selection before deposit talk
	if !strings.Contains(prompt, "AFTER the caller picks a specific date AND time") {
		t.Fatalf("expected prompt to require slot selection before deposit")
	}
	// Must mention configured deposit amount
	if !strings.Contains(prompt, "75 dollar deposit") {
		t.Fatalf("expected prompt to contain configured deposit amount")
	}
	// One question at a time
	if !strings.Contains(prompt, "ONE question at a time") {
		t.Fatalf("expected one-question-at-a-time instruction")
	}
}

func TestBuildVoiceSystemPrompt_DefaultStillEnforcesOneQuestionAtATime(t *testing.T) {
	prompt := BuildVoiceSystemPrompt(slog.Default(), nil, "")
	if !strings.Contains(prompt, "ONE question at a time") {
		t.Fatalf("expected one-question-at-a-time instruction in default prompt")
	}
}

func TestBuildVoiceSystemPrompt_UsesClinicConfigFromStore(t *testing.T) {
	orgID := "org-prompt-clinic"
	cfg := clinic.DefaultConfig(orgID)
	cfg.Name = "Textual Glow"
	cfg.DepositAmountCents = 6400

	store := setupClinicStore(t, cfg)

	loaded, err := store.Get(context.Background(), orgID)
	if err != nil {
		t.Fatalf("store.Get() failed: %v", err)
	}
	if loaded.Name != "Textual Glow" {
		t.Fatalf("loaded config name = %q, want %q", loaded.Name, "Textual Glow")
	}

	prompt := BuildVoiceSystemPrompt(slog.Default(), store, orgID)
	if !strings.Contains(prompt, "Textual Glow") {
		t.Fatalf("expected prompt to include clinic name from config")
	}
	if !strings.Contains(prompt, "64 dollar deposit") {
		t.Fatalf("expected prompt to include deposit from config")
	}
}

func TestBuildVoiceSystemPrompt_IncludesAfterXGuardrails(t *testing.T) {
	prompt := BuildVoiceSystemPrompt(slog.Default(), nil, "")
	// Must have "after X" time filtering rule
	if !strings.Contains(prompt, "After four PM") {
		t.Fatalf("expected after-X guidance in prompt")
	}
	if !strings.Contains(prompt, "NEVER four PM exactly") {
		t.Fatalf("expected strict after-X wording in prompt")
	}
}

func TestBuildVoiceSystemPrompt_AvailabilityGuardrails(t *testing.T) {
	prompt := BuildVoiceSystemPrompt(slog.Default(), nil, "")
	mustContain := []string{
		"MUST use the check_availability tool",
		"Never invent or guess times",
	}
	for _, fragment := range mustContain {
		if !strings.Contains(prompt, fragment) {
			t.Fatalf("expected prompt to contain %q", fragment)
		}
	}
}

func TestBuildVoiceSystemPrompt_PaymentTruthfulness(t *testing.T) {
	prompt := BuildVoiceSystemPrompt(slog.Default(), nil, "")
	mustContain := []string{
		"NEVER say payment went through unless the caller explicitly confirms",
		"Never offer to email",
		"Never invent capabilities",
		"404",
	}
	for _, fragment := range mustContain {
		if !strings.Contains(prompt, fragment) {
			t.Fatalf("expected prompt to contain %q", fragment)
		}
	}
}

func TestBuildVoiceSystemPrompt_GreetingSuppression(t *testing.T) {
	prompt := BuildVoiceSystemPrompt(slog.Default(), nil, "")
	if !strings.Contains(prompt, "Do NOT greet again") {
		t.Fatal("expected greeting suppression instruction")
	}
}

func TestBuildVoiceSystemPrompt_StructuredSections(t *testing.T) {
	prompt := BuildVoiceSystemPrompt(slog.Default(), nil, "")
	// Verify the prompt has clear section headers
	sections := []string{"GREETING:", "STYLE:", "BOOKING FLOW:", "AVAILABILITY:", "DEPOSIT:", "PAYMENT RULES:", "BEHAVIOR:"}
	for _, s := range sections {
		if !strings.Contains(prompt, s) {
			t.Errorf("expected section header %q in prompt", s)
		}
	}
}

func TestBuildVoiceSystemPrompt_NoPreLoadedAvailability(t *testing.T) {
	prompt := BuildVoiceSystemPrompt(slog.Default(), nil, "")
	if !strings.Contains(prompt, "MUST use the check_availability tool") {
		t.Fatal("expected tool-only availability instruction")
	}
	if strings.Contains(prompt, "AVAILABILITY DATA") {
		t.Fatal("should not have pre-loaded availability data section")
	}
}

func TestBuildVoiceSystemPrompt_MandatoryCheckpoint(t *testing.T) {
	prompt := BuildVoiceSystemPrompt(slog.Default(), nil, "")
	if !strings.Contains(prompt, "CHECKPOINT") {
		t.Fatal("expected CHECKPOINT section in booking flow")
	}
	if !strings.Contains(prompt, "MUST complete steps 1-5") {
		t.Fatal("expected mandatory steps instruction")
	}
}
