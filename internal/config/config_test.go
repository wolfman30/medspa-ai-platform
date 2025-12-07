package config

import (
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("ENV", "")
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("OPENAI_MODEL", "")
	cfg := Load()
	if cfg.Port != "8080" {
		t.Fatalf("expected default port, got %s", cfg.Port)
	}
	if cfg.Env != "development" {
		t.Fatalf("expected default env, got %s", cfg.Env)
	}
	if cfg.OpenAIModel != "gpt-4o-mini" {
		t.Fatalf("unexpected default model %s", cfg.OpenAIModel)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("ENV", "production")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("DATABASE_URL", "postgres://user@host/db")
	t.Setenv("DEPOSIT_AMOUNT_CENTS", "7500")
	t.Setenv("TWILIO_ORG_MAP_JSON", "{\"+1555\":\"org1\"}")
	cfg := Load()
	if cfg.Port != "9090" {
		t.Fatalf("expected override port, got %s", cfg.Port)
	}
	if cfg.Env != "production" {
		t.Fatalf("expected env override, got %s", cfg.Env)
	}
	if cfg.DatabaseURL != "postgres://user@host/db" {
		t.Fatalf("expected db override, got %s", cfg.DatabaseURL)
	}
	if cfg.DepositAmountCents != 7500 {
		t.Fatalf("expected deposit override, got %d", cfg.DepositAmountCents)
	}
	if cfg.TwilioOrgMapJSON != "{\"+1555\":\"org1\"}" {
		t.Fatalf("expected org map override, got %s", cfg.TwilioOrgMapJSON)
	}
}
