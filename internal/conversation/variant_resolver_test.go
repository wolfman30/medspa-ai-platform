package conversation

import (
	"context"
	"fmt"
	"testing"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
)

func makeVariantConfig(variants map[string][]string) *clinic.Config {
	return &clinic.Config{
		ServiceVariants: variants,
	}
}

var weightLossVariants = map[string][]string{
	"weight loss": {"Weight Loss - In Person", "Weight Loss - Virtual"},
}

// --- ResolveServiceVariant (keyword-only, no LLM) ---

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
	cfg := makeVariantConfig(weightLossVariants)
	resolved, question := ResolveServiceVariant(cfg, "botox", []string{"I want botox"})
	if resolved != "botox" {
		t.Errorf("expected original service, got %q", resolved)
	}
	if question != "" {
		t.Errorf("expected no question, got %q", question)
	}
}

func TestResolveServiceVariant_AsksWhenAmbiguous(t *testing.T) {
	cfg := makeVariantConfig(weightLossVariants)
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
	cfg := makeVariantConfig(weightLossVariants)
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
		})
	}
}

func TestResolveServiceVariant_ResolvesVirtual(t *testing.T) {
	cfg := makeVariantConfig(weightLossVariants)
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
		})
	}
}

func TestResolveServiceVariant_ChecksMultipleMessages(t *testing.T) {
	cfg := makeVariantConfig(weightLossVariants)
	msgs := []string{"I want to lose weight", "I'd like to do it in person"}
	resolved, _ := ResolveServiceVariant(cfg, "weight loss", msgs)
	if resolved != "Weight Loss - In Person" {
		t.Errorf("expected 'Weight Loss - In Person', got %q", resolved)
	}
}

func TestResolveServiceVariant_FirstMatchWins(t *testing.T) {
	cfg := makeVariantConfig(weightLossVariants)
	msgs := []string{"Let's do virtual", "Actually maybe in person"}
	resolved, _ := ResolveServiceVariant(cfg, "weight loss", msgs)
	if resolved != "Weight Loss - Virtual" {
		t.Errorf("expected first match (Virtual) to win, got %q", resolved)
	}
}

func TestResolveServiceVariant_CaseInsensitive(t *testing.T) {
	cfg := makeVariantConfig(weightLossVariants)
	resolved, _ := ResolveServiceVariant(cfg, "weight loss", []string{"I WANT VIRTUAL PLEASE"})
	if resolved != "Weight Loss - Virtual" {
		t.Errorf("expected Virtual, got %q", resolved)
	}
}

func TestResolveServiceVariant_FuzzyServiceMatch(t *testing.T) {
	cfg := makeVariantConfig(weightLossVariants)
	resolved, question := ResolveServiceVariant(cfg, "weight loss consultation", []string{"virtual please"})
	if resolved != "Weight Loss - Virtual" {
		t.Errorf("expected Virtual via fuzzy match, got %q (question: %q)", resolved, question)
	}
}

func TestResolveServiceVariant_VariantsWithoutDash(t *testing.T) {
	cfg := makeVariantConfig(map[string][]string{
		"therapy": {"In Person Therapy", "Virtual Therapy"},
	})
	resolved, question := ResolveServiceVariant(cfg, "therapy", []string{"I want to come in"})
	if resolved != "In Person Therapy" {
		t.Errorf("expected 'In Person Therapy', got %q (question: %q)", resolved, question)
	}
}

func TestResolveServiceVariant_SingleVariantNoQuestion(t *testing.T) {
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

// --- VariantResolver with LLM ---

// mockVariantLLM returns a canned response for testing the LLM classifier path.
type mockVariantLLM struct {
	response string
	err      error
	calls    int
}

func (m *mockVariantLLM) Complete(_ context.Context, req LLMRequest) (LLMResponse, error) {
	m.calls++
	if m.err != nil {
		return LLMResponse{}, m.err
	}
	return LLMResponse{Text: m.response}, nil
}

func TestVariantResolver_LLM_ResolvesInPerson(t *testing.T) {
	mock := &mockVariantLLM{response: "Weight Loss - In Person"}
	vr := NewVariantResolver(mock, "test-model", nil)
	cfg := makeVariantConfig(weightLossVariants)

	resolved, question := vr.Resolve(context.Background(), cfg, "weight loss", []string{"I'd like to come see you guys"})
	if resolved != "Weight Loss - In Person" {
		t.Errorf("expected 'Weight Loss - In Person', got %q", resolved)
	}
	if question != "" {
		t.Errorf("expected no question, got %q", question)
	}
	if mock.calls != 1 {
		t.Errorf("expected 1 LLM call, got %d", mock.calls)
	}
}

func TestVariantResolver_LLM_ResolvesVirtual(t *testing.T) {
	mock := &mockVariantLLM{response: "Weight Loss - Virtual"}
	vr := NewVariantResolver(mock, "test-model", nil)
	cfg := makeVariantConfig(weightLossVariants)

	resolved, question := vr.Resolve(context.Background(), cfg, "weight loss", []string{"I'd rather do it from my couch"})
	if resolved != "Weight Loss - Virtual" {
		t.Errorf("expected 'Weight Loss - Virtual', got %q", resolved)
	}
	if question != "" {
		t.Errorf("expected no question, got %q", question)
	}
}

func TestVariantResolver_LLM_Unclear(t *testing.T) {
	mock := &mockVariantLLM{response: "unclear"}
	vr := NewVariantResolver(mock, "test-model", nil)
	cfg := makeVariantConfig(weightLossVariants)

	resolved, question := vr.Resolve(context.Background(), cfg, "weight loss", []string{"I want to lose weight"})
	if resolved != "" {
		t.Errorf("expected empty resolved, got %q", resolved)
	}
	if question == "" {
		t.Fatal("expected clarification question")
	}
}

func TestVariantResolver_LLM_FallbackOnError(t *testing.T) {
	// LLM fails, but message contains a keyword — should fall back to keyword matching
	mock := &mockVariantLLM{err: fmt.Errorf("API timeout")}
	vr := NewVariantResolver(mock, "test-model", nil)
	cfg := makeVariantConfig(weightLossVariants)

	resolved, question := vr.Resolve(context.Background(), cfg, "weight loss", []string{"I want to come in person"})
	if resolved != "Weight Loss - In Person" {
		t.Errorf("expected keyword fallback to resolve 'Weight Loss - In Person', got %q (question: %q)", resolved, question)
	}
}

func TestVariantResolver_LLM_FallbackOnErrorNoKeyword(t *testing.T) {
	// LLM fails AND no keyword match — should ask clarification
	mock := &mockVariantLLM{err: fmt.Errorf("API timeout")}
	vr := NewVariantResolver(mock, "test-model", nil)
	cfg := makeVariantConfig(weightLossVariants)

	resolved, question := vr.Resolve(context.Background(), cfg, "weight loss", []string{"I want to lose weight"})
	if resolved != "" {
		t.Errorf("expected empty resolved, got %q", resolved)
	}
	if question == "" {
		t.Fatal("expected clarification question on fallback")
	}
}

func TestVariantResolver_LLM_FuzzyMatchResponse(t *testing.T) {
	// LLM returns just "Virtual" instead of the full variant name
	mock := &mockVariantLLM{response: "Virtual"}
	vr := NewVariantResolver(mock, "test-model", nil)
	cfg := makeVariantConfig(weightLossVariants)

	resolved, question := vr.Resolve(context.Background(), cfg, "weight loss", []string{"from home please"})
	if resolved != "Weight Loss - Virtual" {
		t.Errorf("expected fuzzy match to 'Weight Loss - Virtual', got %q (question: %q)", resolved, question)
	}
}

func TestVariantResolver_LLM_CaseInsensitiveResponse(t *testing.T) {
	mock := &mockVariantLLM{response: "weight loss - in person"}
	vr := NewVariantResolver(mock, "test-model", nil)
	cfg := makeVariantConfig(weightLossVariants)

	resolved, _ := vr.Resolve(context.Background(), cfg, "weight loss", []string{"come in"})
	if resolved != "Weight Loss - In Person" {
		t.Errorf("expected case-insensitive match, got %q", resolved)
	}
}

func TestVariantResolver_LLM_UnexpectedResponse(t *testing.T) {
	// LLM returns something completely irrelevant
	mock := &mockVariantLLM{response: "I think the patient wants botox"}
	vr := NewVariantResolver(mock, "test-model", nil)
	cfg := makeVariantConfig(weightLossVariants)

	resolved, question := vr.Resolve(context.Background(), cfg, "weight loss", []string{"lose weight"})
	if resolved != "" {
		t.Errorf("expected empty resolved for unexpected LLM response, got %q", resolved)
	}
	if question == "" {
		t.Fatal("expected clarification question for unexpected response")
	}
}

func TestVariantResolver_NoLLM_FallsBackToKeywords(t *testing.T) {
	// nil LLM — should use keyword matching only
	vr := NewVariantResolver(nil, "", nil)
	cfg := makeVariantConfig(weightLossVariants)

	resolved, _ := vr.Resolve(context.Background(), cfg, "weight loss", []string{"I want virtual"})
	if resolved != "Weight Loss - Virtual" {
		t.Errorf("expected keyword fallback, got %q", resolved)
	}
}

func TestVariantResolver_NoConfig(t *testing.T) {
	mock := &mockVariantLLM{response: "Weight Loss - Virtual"}
	vr := NewVariantResolver(mock, "test-model", nil)

	resolved, question := vr.Resolve(context.Background(), nil, "weight loss", []string{"virtual"})
	if resolved != "weight loss" {
		t.Errorf("expected original service, got %q", resolved)
	}
	if question != "" {
		t.Errorf("expected no question, got %q", question)
	}
	if mock.calls != 0 {
		t.Errorf("LLM should not be called when no config, got %d calls", mock.calls)
	}
}

func TestVariantResolver_NoVariantsConfigured(t *testing.T) {
	mock := &mockVariantLLM{response: "something"}
	vr := NewVariantResolver(mock, "test-model", nil)
	cfg := makeVariantConfig(map[string][]string{})

	resolved, _ := vr.Resolve(context.Background(), cfg, "botox", []string{"hello"})
	if resolved != "botox" {
		t.Errorf("expected original service, got %q", resolved)
	}
	if mock.calls != 0 {
		t.Errorf("LLM should not be called when no variants, got %d calls", mock.calls)
	}
}

func TestVariantResolver_LLM_NaturalLanguage(t *testing.T) {
	// Test cases that keyword matching would miss but LLM should handle
	tests := []struct {
		name     string
		llmReply string
		msgs     []string
		expected string
	}{
		{"do it from my couch", "Weight Loss - Virtual", []string{"can I do this from my couch?"}, "Weight Loss - Virtual"},
		{"come see you guys", "Weight Loss - In Person", []string{"I'd love to come see you guys"}, "Weight Loss - In Person"},
		{"not leave the house", "Weight Loss - Virtual", []string{"I'd rather not leave the house"}, "Weight Loss - Virtual"},
		{"be there physically", "Weight Loss - In Person", []string{"I want to be there physically"}, "Weight Loss - In Person"},
		{"do it over the phone", "Weight Loss - Virtual", []string{"can we just do it over the phone?"}, "Weight Loss - Virtual"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockVariantLLM{response: tt.llmReply}
			vr := NewVariantResolver(mock, "test-model", nil)
			cfg := makeVariantConfig(weightLossVariants)

			resolved, question := vr.Resolve(context.Background(), cfg, "weight loss", tt.msgs)
			if resolved != tt.expected {
				t.Errorf("expected %q, got %q (question: %q)", tt.expected, resolved, question)
			}
		})
	}
}

// --- recentUserMessages helper ---

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
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d: %v", len(msgs), msgs)
	}
	if msgs[0] != "current msg" {
		t.Errorf("first should be lowercased current message, got %q", msgs[0])
	}
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
	msgs := recentUserMessages(history, "Current", 1)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d: %v", len(msgs), msgs)
	}
	if msgs[1] != "recent message" {
		t.Errorf("expected 'recent message', got %q", msgs[1])
	}
}
