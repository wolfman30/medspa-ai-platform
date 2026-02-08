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

func TestSMSProviderIssues_NoProvider(t *testing.T) {
	cfg := &Config{} // all empty
	issues := cfg.SMSProviderIssues()
	if len(issues) == 0 {
		t.Fatal("expected issues when no SMS provider is configured")
	}
}

func TestSMSProviderIssues_TelnyxOK(t *testing.T) {
	cfg := &Config{
		TelnyxAPIKey:             "KEY123",
		TelnyxMessagingProfileID: "profile-id",
		TelnyxFromNumber:         "+14407448197",
	}
	issues := cfg.SMSProviderIssues()
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got: %v", issues)
	}
}

func TestSMSProviderIssues_TelnyxMissingFrom(t *testing.T) {
	cfg := &Config{
		TelnyxAPIKey:             "KEY123",
		TelnyxMessagingProfileID: "profile-id",
		// TelnyxFromNumber missing
	}
	issues := cfg.SMSProviderIssues()
	if len(issues) == 0 {
		t.Fatal("expected issue about missing TELNYX_FROM_NUMBER")
	}
}

func TestSMSProviderIssues_TwilioOK(t *testing.T) {
	cfg := &Config{
		TwilioAccountSID: "AC123",
		TwilioAuthToken:  "token",
		TwilioFromNumber: "+18662894911",
	}
	issues := cfg.SMSProviderIssues()
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got: %v", issues)
	}
}

func TestSMSProviderIssues_BothProviders(t *testing.T) {
	cfg := &Config{
		TelnyxAPIKey:             "KEY123",
		TelnyxMessagingProfileID: "profile-id",
		TelnyxFromNumber:         "+14407448197",
		TwilioAccountSID:         "AC123",
		TwilioAuthToken:          "token",
		TwilioFromNumber:         "+18662894911",
	}
	issues := cfg.SMSProviderIssues()
	if len(issues) != 0 {
		t.Fatalf("expected no issues with both providers, got: %v", issues)
	}
}
