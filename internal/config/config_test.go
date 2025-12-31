package config

import "testing"

func TestLoadPersistConversationHistoryEnabled(t *testing.T) {
	t.Setenv("PERSIST_CONVERSATION_HISTORY", "true")

	cfg := Load()
	if !cfg.PersistConversationHistory {
		t.Fatalf("expected PersistConversationHistory to be true")
	}
}

func TestLoadPersistConversationHistoryDefaultFalse(t *testing.T) {
	t.Setenv("PERSIST_CONVERSATION_HISTORY", "false")

	cfg := Load()
	if cfg.PersistConversationHistory {
		t.Fatalf("expected PersistConversationHistory to be false by default")
	}
}
