package conversation

import (
	"testing"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
)

func makeVariantConfig(variants map[string][]string) *clinic.Config {
	return &clinic.Config{
		ServiceVariants: variants,
	}
}

func TestResolveServiceVariant_NoConfig(t *testing.T) {
	resolved, question := ResolveServiceVariant(nil, "weight loss", []string{"hello"})
	if resolved != "weight loss" {
		t.Errorf("expected original service, got %q", resolved)
	}
	if question != "" {
		t.Errorf("expected no question, got %q", question)
	}
}

func TestResolveServiceVariant_NoVariants(t *testing.T) {
	cfg := makeVariantConfig(map[string][]string{})
	resolved, question := ResolveServiceVariant(cfg, "botox", []string{"I want botox"})
	if resolved != "botox" {
		t.Errorf("expected original service, got %q", resolved)
	}
	if question != "" {
		t.Errorf("expected no question, got %q", question)
	}
}

func TestResolveServiceVariant_ServiceWithoutVariants(t *testing.T) {
	cfg := makeVariantConfig(map[string][]string{
		"weight loss": {"Weight Loss - In Person", "Weight Loss - Virtual"},
	})
	resolved, question := ResolveServiceVariant(cfg, "botox", []string{"I want botox"})
	if resolved != "botox" {
		t.Errorf("expected original service, got %q", resolved)
	}
	if question != "" {
		t.Errorf("expected no question, got %q", question)
	}
}

func TestResolveServiceVariant_AsksWhenAmbiguous(t *testing.T) {
	cfg := makeVariantConfig(map[string][]string{
		"weight loss": {"Weight Loss - In Person", "Weight Loss - Virtual"},
	})
	resolved, question := ResolveServiceVariant(cfg, "weight loss", []string{"I want to lose weight"})
	if resolved != "" {
		t.Errorf("expected empty resolved, got %q", resolved)
	}
	if question == "" {
		t.Fatal("expected clarification question")
	}
	if question != "Would you prefer an in person or virtual weight loss consultation?" {
		t.Errorf("unexpected question: %q", question)
	}
}

func TestResolveServiceVariant_ResolvesInPerson(t *testing.T) {
	cfg := makeVariantConfig(map[string][]string{
		"weight loss": {"Weight Loss - In Person", "Weight Loss - Virtual"},
	})

	tests := []struct {
		name string
		msgs []string
	}{
		{"explicit in person", []string{"I'd like to come in person"}},
		{"hyphenated", []string{"I prefer in-person"}},
		{"come in", []string{"Can I come in for this?"}},
		{"office", []string{"I'd rather visit the office"}},
		{"clinic", []string{"I'll come to the clinic"}},
		{"walk in", []string{"Can I walk in?"}},
		{"face to face", []string{"I prefer face to face"}},
		{"on site", []string{"I'd prefer to be seen on site"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, question := ResolveServiceVariant(cfg, "weight loss", tt.msgs)
			if resolved != "Weight Loss - In Person" {
				t.Errorf("expected 'Weight Loss - In Person', got %q (question: %q)", resolved, question)
			}
			if question != "" {
				t.Errorf("expected no question, got %q", question)
			}
		})
	}
}

func TestResolveServiceVariant_ResolvesVirtual(t *testing.T) {
	cfg := makeVariantConfig(map[string][]string{
		"weight loss": {"Weight Loss - In Person", "Weight Loss - Virtual"},
	})

	tests := []struct {
		name string
		msgs []string
	}{
		{"explicit virtual", []string{"I'd like a virtual consultation"}},
		{"telehealth", []string{"Can we do telehealth?"}},
		{"online", []string{"Online would be great"}},
		{"video", []string{"Let's do a video call"}},
		{"zoom", []string{"Can we zoom?"}},
		{"remote", []string{"I prefer remote"}},
		{"from home", []string{"I'd rather do it from home"}},
		{"telemedicine", []string{"Is telemedicine available?"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, question := ResolveServiceVariant(cfg, "weight loss", tt.msgs)
			if resolved != "Weight Loss - Virtual" {
				t.Errorf("expected 'Weight Loss - Virtual', got %q (question: %q)", resolved, question)
			}
			if question != "" {
				t.Errorf("expected no question, got %q", question)
			}
		})
	}
}

func TestResolveServiceVariant_ChecksMultipleMessages(t *testing.T) {
	cfg := makeVariantConfig(map[string][]string{
		"weight loss": {"Weight Loss - In Person", "Weight Loss - Virtual"},
	})
	// Variant keyword is in the second message, not the first
	msgs := []string{"I want to lose weight", "I'd like to do it in person"}
	resolved, question := ResolveServiceVariant(cfg, "weight loss", msgs)
	if resolved != "Weight Loss - In Person" {
		t.Errorf("expected 'Weight Loss - In Person', got %q (question: %q)", resolved, question)
	}
}

func TestResolveServiceVariant_FirstMatchWins(t *testing.T) {
	cfg := makeVariantConfig(map[string][]string{
		"weight loss": {"Weight Loss - In Person", "Weight Loss - Virtual"},
	})
	// First message says virtual, second says in person — first wins
	msgs := []string{"Let's do virtual", "Actually maybe in person"}
	resolved, _ := ResolveServiceVariant(cfg, "weight loss", msgs)
	if resolved != "Weight Loss - Virtual" {
		t.Errorf("expected first match (Virtual) to win, got %q", resolved)
	}
}

func TestResolveServiceVariant_CaseInsensitive(t *testing.T) {
	cfg := makeVariantConfig(map[string][]string{
		"weight loss": {"Weight Loss - In Person", "Weight Loss - Virtual"},
	})
	resolved, _ := ResolveServiceVariant(cfg, "weight loss", []string{"I WANT VIRTUAL PLEASE"})
	if resolved != "Weight Loss - Virtual" {
		t.Errorf("expected Virtual, got %q", resolved)
	}
}

func TestResolveServiceVariant_FuzzyServiceMatch(t *testing.T) {
	// GetServiceVariants does fuzzy matching — "weight loss consultation" contains "weight loss"
	cfg := makeVariantConfig(map[string][]string{
		"weight loss": {"Weight Loss - In Person", "Weight Loss - Virtual"},
	})
	resolved, question := ResolveServiceVariant(cfg, "weight loss consultation", []string{"virtual please"})
	if resolved != "Weight Loss - Virtual" {
		t.Errorf("expected Virtual via fuzzy match, got %q (question: %q)", resolved, question)
	}
}

func TestResolveServiceVariant_VariantsWithoutDash(t *testing.T) {
	// Variant names without " - " separator
	cfg := makeVariantConfig(map[string][]string{
		"therapy": {"In Person Therapy", "Virtual Therapy"},
	})
	resolved, question := ResolveServiceVariant(cfg, "therapy", []string{"I want to come in"})
	if resolved != "In Person Therapy" {
		t.Errorf("expected 'In Person Therapy', got %q (question: %q)", resolved, question)
	}
}

func TestResolveServiceVariant_SingleVariantNoQuestion(t *testing.T) {
	// Only one variant — GetServiceVariants returns nil (requires >1)
	cfg := makeVariantConfig(map[string][]string{
		"weight loss": {"Weight Loss - In Person"},
	})
	resolved, question := ResolveServiceVariant(cfg, "weight loss", []string{"hello"})
	if resolved != "weight loss" {
		t.Errorf("expected original service (single variant = no variants), got %q", resolved)
	}
	if question != "" {
		t.Errorf("expected no question, got %q", question)
	}
}

func TestRecentUserMessages(t *testing.T) {
	history := []ChatMessage{
		{Role: ChatRoleSystem, Content: "system"},
		{Role: ChatRoleUser, Content: "First message"},
		{Role: ChatRoleAssistant, Content: "Reply 1"},
		{Role: ChatRoleUser, Content: "Second message"},
		{Role: ChatRoleAssistant, Content: "Reply 2"},
		{Role: ChatRoleUser, Content: "Third message"},
	}

	msgs := recentUserMessages(history, "Current MSG", 6)

	if len(msgs) != 4 { // current + 3 user messages
		t.Fatalf("expected 4 messages, got %d: %v", len(msgs), msgs)
	}
	if msgs[0] != "current msg" {
		t.Errorf("first should be lowercased current message, got %q", msgs[0])
	}
	// Should be reverse order from history
	if msgs[1] != "third message" {
		t.Errorf("expected 'third message', got %q", msgs[1])
	}
}

func TestRecentUserMessages_EmptyHistory(t *testing.T) {
	msgs := recentUserMessages(nil, "Hello", 6)
	if len(msgs) != 1 || msgs[0] != "hello" {
		t.Errorf("expected ['hello'], got %v", msgs)
	}
}

func TestRecentUserMessages_LookbackLimit(t *testing.T) {
	history := []ChatMessage{
		{Role: ChatRoleUser, Content: "Old message"},
		{Role: ChatRoleUser, Content: "Recent message"},
	}
	// lookback=1 should only get the last message
	msgs := recentUserMessages(history, "Current", 1)
	if len(msgs) != 2 { // current + 1 from lookback
		t.Fatalf("expected 2 messages, got %d: %v", len(msgs), msgs)
	}
	if msgs[1] != "recent message" {
		t.Errorf("expected 'recent message', got %q", msgs[1])
	}
}
