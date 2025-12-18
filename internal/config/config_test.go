package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("ENV", "")
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("BEDROCK_MODEL_ID", "")
	cfg := Load()
	if cfg.Port != "8080" {
		t.Fatalf("expected default port, got %s", cfg.Port)
	}
	if cfg.Env != "development" {
		t.Fatalf("expected default env, got %s", cfg.Env)
	}
	if cfg.BedrockModelID != "" {
		t.Fatalf("expected default bedrock model empty, got %s", cfg.BedrockModelID)
	}
	if cfg.AestheticRecordShadowSyncEnabled {
		t.Fatalf("expected aesthetic record shadow sync disabled by default")
	}
	if cfg.AestheticRecordSyncInterval != 30*time.Minute {
		t.Fatalf("expected default aesthetic record sync interval, got %s", cfg.AestheticRecordSyncInterval)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("ENV", "production")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("DATABASE_URL", "postgres://user@host/db")
	t.Setenv("DEPOSIT_AMOUNT_CENTS", "7500")
	t.Setenv("TWILIO_ORG_MAP_JSON", "{\"+1555\":\"org1\"}")
	t.Setenv("AESTHETIC_RECORD_CLINIC_ID", "clinic-123")
	t.Setenv("AESTHETIC_RECORD_SHADOW_SYNC_ENABLED", "true")
	t.Setenv("AESTHETIC_RECORD_SYNC_INTERVAL", "45m")
	t.Setenv("AESTHETIC_RECORD_SYNC_WINDOW_DAYS", "14")
	t.Setenv("AESTHETIC_RECORD_SYNC_DURATION_MINS", "20")
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
	if cfg.AestheticRecordClinicID != "clinic-123" {
		t.Fatalf("expected aesthetic record clinic override, got %s", cfg.AestheticRecordClinicID)
	}
	if !cfg.AestheticRecordShadowSyncEnabled {
		t.Fatalf("expected aesthetic record shadow sync enabled")
	}
	if cfg.AestheticRecordSyncInterval != 45*time.Minute {
		t.Fatalf("expected aesthetic record sync interval override, got %s", cfg.AestheticRecordSyncInterval)
	}
	if cfg.AestheticRecordSyncWindowDays != 14 {
		t.Fatalf("expected aesthetic record sync window override, got %d", cfg.AestheticRecordSyncWindowDays)
	}
	if cfg.AestheticRecordSyncDurationMins != 20 {
		t.Fatalf("expected aesthetic record duration override, got %d", cfg.AestheticRecordSyncDurationMins)
	}
}
