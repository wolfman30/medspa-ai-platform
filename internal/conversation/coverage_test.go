package conversation

import (
	"strings"
	"testing"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
)

// ─── trimHistory ───────────────────────────────────────────────

func TestTrimHistory_EmptyHistory(t *testing.T) {
	if len(trimHistory(nil, 10)) != 0 {
		t.Error("expected empty")
	}
}

func TestTrimHistory_UnderLimit(t *testing.T) {
	h := []ChatMessage{
		{Role: ChatRoleSystem, Content: "system"},
		{Role: ChatRoleUser, Content: "hi"},
		{Role: ChatRoleAssistant, Content: "hello"},
	}
	if len(trimHistory(h, 10)) != 3 {
		t.Error("expected 3")
	}
}

func TestTrimHistory_AtLimit(t *testing.T) {
	h := []ChatMessage{
		{Role: ChatRoleSystem, Content: "system"},
		{Role: ChatRoleUser, Content: "1"},
		{Role: ChatRoleAssistant, Content: "2"},
		{Role: ChatRoleUser, Content: "3"},
	}
	if len(trimHistory(h, 4)) != 4 {
		t.Error("expected 4")
	}
}

func TestTrimHistory_OverLimit_PreservesSystemAndRecent(t *testing.T) {
	h := []ChatMessage{
		{Role: ChatRoleSystem, Content: "system"},
		{Role: ChatRoleUser, Content: "old1"},
		{Role: ChatRoleAssistant, Content: "old2"},
		{Role: ChatRoleUser, Content: "old3"},
		{Role: ChatRoleAssistant, Content: "old4"},
		{Role: ChatRoleUser, Content: "recent1"},
		{Role: ChatRoleAssistant, Content: "recent2"},
	}
	result := trimHistory(h, 5)
	if len(result) != 5 {
		t.Fatalf("expected 5, got %d", len(result))
	}
	if result[0].Role != ChatRoleSystem {
		t.Error("first message should be system")
	}
	if result[len(result)-1].Content != "recent2" {
		t.Errorf("last message should be recent2, got %q", result[len(result)-1].Content)
	}
}

func TestTrimHistory_NoSystem(t *testing.T) {
	h := []ChatMessage{
		{Role: ChatRoleUser, Content: "1"},
		{Role: ChatRoleAssistant, Content: "2"},
		{Role: ChatRoleUser, Content: "3"},
		{Role: ChatRoleAssistant, Content: "4"},
		{Role: ChatRoleUser, Content: "5"},
	}
	result := trimHistory(h, 3)
	if len(result) != 3 {
		t.Fatalf("expected 3, got %d", len(result))
	}
	if result[len(result)-1].Content != "5" {
		t.Errorf("expected '5', got %q", result[len(result)-1].Content)
	}
}

// ─── clinicHasService ──────────────────────────────────────────

func TestClinicHasService_NilConfig(t *testing.T) {
	if clinicHasService(nil, "botox") {
		t.Error("nil config should return false")
	}
}

func TestClinicHasService_EmptyServices(t *testing.T) {
	if clinicHasService(&clinic.Config{}, "botox") {
		t.Error("empty services should return false")
	}
}

func TestClinicHasService_Found(t *testing.T) {
	cfg := &clinic.Config{Services: []string{"Tox (Wrinkle Relaxer)", "Dermal Filler"}}
	if !clinicHasService(cfg, "filler") {
		t.Error("should find 'filler' in 'Dermal Filler'")
	}
	if !clinicHasService(cfg, "tox") {
		t.Error("should find 'tox'")
	}
}

func TestClinicHasService_NotFound(t *testing.T) {
	if clinicHasService(&clinic.Config{Services: []string{"Microneedling"}}, "botox") {
		t.Error("should not find botox")
	}
}

func TestClinicHasService_CaseInsensitive(t *testing.T) {
	cfg := &clinic.Config{Services: []string{"Tox (Wrinkle Relaxer)"}}
	if !clinicHasService(cfg, "TOX") {
		t.Error("should be case-insensitive")
	}
}

// ─── isPriceInquiry ────────────────────────────────────────────

func TestIsPriceInquiry_Coverage(t *testing.T) {
	positives := []string{
		"how much does botox cost",
		"what's the price of filler",
		"how much is lip filler",
		"pricing for laser",
		"cost?",
		"what are your rates",
	}
	for _, msg := range positives {
		if !isPriceInquiry(msg) {
			t.Errorf("isPriceInquiry(%q) should be true", msg)
		}
	}
	negatives := []string{"I want botox", "", "do you accept insurance"}
	for _, msg := range negatives {
		if isPriceInquiry(msg) {
			t.Errorf("isPriceInquiry(%q) should be false", msg)
		}
	}
}

// ─── isAmbiguousHelp ───────────────────────────────────────────

func TestIsAmbiguousHelp_Coverage(t *testing.T) {
	// Must contain "help", "question", or "info" and NOT contain service/booking keywords
	positives := []string{"help", "info", "question", "I have a question", "need help", "more info please"}
	for _, msg := range positives {
		if !isAmbiguousHelp(msg) {
			t.Errorf("isAmbiguousHelp(%q) should be true", msg)
		}
	}
	negatives := []string{"hi", "hello", "", "help me book an appointment", "info about botox", "yes", "no"}
	for _, msg := range negatives {
		if isAmbiguousHelp(msg) {
			t.Errorf("isAmbiguousHelp(%q) should be false", msg)
		}
	}
}

// ─── isQuestionSelection ───────────────────────────────────────

func TestIsQuestionSelection_Coverage(t *testing.T) {
	positives := []string{"question", "quick question", "I have a question", "i had a question", "got a question"}
	for _, msg := range positives {
		if !isQuestionSelection(msg) {
			t.Errorf("isQuestionSelection(%q) should be true", msg)
		}
	}
	negatives := []string{"I want botox", "hello", "", "1", "question about filler"}
	for _, msg := range negatives {
		if isQuestionSelection(msg) {
			t.Errorf("isQuestionSelection(%q) should be false", msg)
		}
	}
}

// ─── sanitizeSMSResponse ───────────────────────────────────────

func TestSanitizeSMSResponse_Coverage(t *testing.T) {
	r := sanitizeSMSResponse("**Hello** world")
	if strings.Contains(r, "**") {
		t.Errorf("should strip bold: %q", r)
	}
	if sanitizeSMSResponse("Hello world") != "Hello world" {
		t.Error("should preserve normal text")
	}
	// Double spaces cleaned
	r = sanitizeSMSResponse("Hello  world")
	if strings.Contains(r, "  ") {
		t.Errorf("should clean double spaces: %q", r)
	}
	// Trimmed
	r = sanitizeSMSResponse("  Hello  ")
	if r != "Hello" {
		t.Errorf("should trim: %q", r)
	}
}

// ─── latestTurnAgreedToDeposit ─────────────────────────────────

func TestLatestTurnAgreedToDeposit_Coverage(t *testing.T) {
	tests := []struct {
		name    string
		history []ChatMessage
		want    bool
	}{
		{"yes after deposit", []ChatMessage{
			{Role: ChatRoleAssistant, Content: "A $50 deposit is required. Ready?"},
			{Role: ChatRoleUser, Content: "yes"},
		}, true},
		{"sure after deposit", []ChatMessage{
			{Role: ChatRoleAssistant, Content: "deposit of $50"},
			{Role: ChatRoleUser, Content: "sure"},
		}, true},
		{"yeah after deposit", []ChatMessage{
			{Role: ChatRoleAssistant, Content: "deposit of $50"},
			{Role: ChatRoleUser, Content: "yeah"},
		}, true},
		{"let's do it", []ChatMessage{
			{Role: ChatRoleAssistant, Content: "deposit of $50"},
			{Role: ChatRoleUser, Content: "let's do it"},
		}, true},
		{"no after deposit", []ChatMessage{
			{Role: ChatRoleAssistant, Content: "deposit of $50"},
			{Role: ChatRoleUser, Content: "no thanks"},
		}, false},
		{"yes no deposit context", []ChatMessage{
			{Role: ChatRoleAssistant, Content: "What service?"},
			{Role: ChatRoleUser, Content: "yes"},
		}, false},
		{"empty", []ChatMessage{}, false},
		{"only user", []ChatMessage{{Role: ChatRoleUser, Content: "yes"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := latestTurnAgreedToDeposit(tt.history); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// ─── conversationHasDepositAgreement ───────────────────────────

func TestConversationHasDepositAgreement_Coverage(t *testing.T) {
	tests := []struct {
		name    string
		history []ChatMessage
		want    bool
	}{
		{"agreed", []ChatMessage{
			{Role: ChatRoleAssistant, Content: "deposit of $50 required"},
			{Role: ChatRoleUser, Content: "Sounds good, let's proceed"},
		}, true},
		{"declined", []ChatMessage{
			{Role: ChatRoleAssistant, Content: "deposit of $50 required"},
			{Role: ChatRoleUser, Content: "no I don't want to pay"},
		}, false},
		{"no deposit", []ChatMessage{
			{Role: ChatRoleAssistant, Content: "What service?"},
			{Role: ChatRoleUser, Content: "botox"},
		}, false},
		{"empty", []ChatMessage{}, false},
		{"proceed after deposit", []ChatMessage{
			{Role: ChatRoleAssistant, Content: "A deposit is needed to confirm"},
			{Role: ChatRoleUser, Content: "proceed"},
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := conversationHasDepositAgreement(tt.history); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// ─── shouldAttemptDepositClassification ────────────────────────

func TestShouldAttemptDepositClassification_Coverage(t *testing.T) {
	tests := []struct {
		name    string
		history []ChatMessage
		want    bool
	}{
		{"deposit context", []ChatMessage{
			{Role: ChatRoleAssistant, Content: "A $50 deposit is required"},
			{Role: ChatRoleUser, Content: "okay"},
		}, true},
		{"no deposit", []ChatMessage{
			{Role: ChatRoleAssistant, Content: "What service?"},
			{Role: ChatRoleUser, Content: "botox"},
		}, false},
		{"empty", []ChatMessage{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldAttemptDepositClassification(tt.history); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// ─── detectServiceKey ──────────────────────────────────────────

func TestDetectServiceKey_Coverage(t *testing.T) {
	cfg := &clinic.Config{
		ServiceAliases: map[string]string{
			"botox":  "Tox (Wrinkle Relaxer)",
			"filler": "Dermal Filler",
		},
		Services: []string{"Tox (Wrinkle Relaxer)", "Dermal Filler", "Microneedling"},
	}
	tests := []struct {
		msg  string
		want string
	}{
		{"I want botox", "tox (wrinkle relaxer)"}, // resolved via alias
		{"interested in filler", "dermal filler"}, // resolved via alias
		{"hello", ""},
		{"I want microneedling", "microneedling"},
	}
	for _, tt := range tests {
		if got := detectServiceKey(tt.msg, cfg); got != tt.want {
			t.Errorf("detectServiceKey(%q) = %q, want %q", tt.msg, got, tt.want)
		}
	}
	// nil config still has universal fallbacks
	if got := detectServiceKey("botox", nil); got != "botox" {
		t.Errorf("nil config + botox should return 'botox', got %q", got)
	}
	if detectServiceKey("xyzzy nonsense", nil) != "" {
		t.Error("unrecognized service should return empty")
	}
}

// ─── detectPHI ─────────────────────────────────────────────────

func TestDetectPHI_Coverage(t *testing.T) {
	// Requires phiPrefaceRE match ("diagnosed", "I have", etc) AND phiKeywordsRE match
	positives := []string{
		"I have diabetes",
		"I was diagnosed with cancer",
		"I've had hepatitis before",
		"I am pregnant",
		"I have depression and anxiety",
	}
	for _, msg := range positives {
		if !detectPHI(msg) {
			t.Errorf("detectPHI(%q) should be true", msg)
		}
	}
	negatives := []string{"I want botox", "hello", "", "my social security number is 123"}
	for _, msg := range negatives {
		if detectPHI(msg) {
			t.Errorf("detectPHI(%q) should be false", msg)
		}
	}
}

// ─── detectMedicalAdvice ───────────────────────────────────────

func TestDetectMedicalAdvice_Coverage(t *testing.T) {
	positives := []string{
		"is it safe to get filler while pregnant",
		"what are the side effects of botox",
		"should I stop taking aspirin before botox",
		"can I get botox if breastfeeding",
	}
	for _, msg := range positives {
		if len(detectMedicalAdvice(msg)) == 0 {
			t.Errorf("detectMedicalAdvice(%q) should detect", msg)
		}
	}
	negatives := []string{"I want to schedule botox", "hello", ""}
	for _, msg := range negatives {
		if len(detectMedicalAdvice(msg)) > 0 {
			t.Errorf("detectMedicalAdvice(%q) should not detect", msg)
		}
	}
}

// ─── splitSystemAndMessages ────────────────────────────────────

func TestSplitSystemAndMessages_Coverage(t *testing.T) {
	history := []ChatMessage{
		{Role: ChatRoleSystem, Content: "sys1"},
		{Role: ChatRoleSystem, Content: "sys2"},
		{Role: ChatRoleUser, Content: "hi"},
		{Role: ChatRoleAssistant, Content: "hello"},
	}
	sys, msgs := splitSystemAndMessages(history)
	if len(sys) != 2 || len(msgs) != 2 {
		t.Errorf("expected 2 sys + 2 msgs, got %d + %d", len(sys), len(msgs))
	}
	// empty
	sys, msgs = splitSystemAndMessages(nil)
	if len(sys) != 0 || len(msgs) != 0 {
		t.Error("nil input should return empty")
	}
	// no system
	sys, msgs = splitSystemAndMessages([]ChatMessage{{Role: ChatRoleUser, Content: "hi"}})
	if len(sys) != 0 || len(msgs) != 1 {
		t.Error("no-system case failed")
	}
}

// ─── summarizeHistory ──────────────────────────────────────────

func TestSummarizeHistory_Coverage(t *testing.T) {
	history := []ChatMessage{
		{Role: ChatRoleSystem, Content: "system prompt"},
		{Role: ChatRoleUser, Content: "I want botox"},
		{Role: ChatRoleAssistant, Content: "What's your name?"},
		{Role: ChatRoleUser, Content: "Jane"},
	}
	result := summarizeHistory(history, 3)
	if result == "" {
		t.Fatal("expected non-empty")
	}
	if strings.Contains(result, "system prompt") {
		t.Error("should exclude system messages")
	}
	if summarizeHistory(nil, 5) != "" {
		t.Error("nil should return empty")
	}
	// Limit
	result = summarizeHistory([]ChatMessage{
		{Role: ChatRoleUser, Content: "msg1"},
		{Role: ChatRoleUser, Content: "msg2"},
		{Role: ChatRoleUser, Content: "msg3"},
	}, 1)
	if strings.Contains(result, "msg1") {
		t.Error("limit=1 should exclude msg1")
	}
}

// ─── buildSystemPrompt ─────────────────────────────────────────

func TestBuildSystemPrompt_DepositAmounts(t *testing.T) {
	if !strings.Contains(buildSystemPrompt(5000, false), "$50") {
		t.Error("expected $50")
	}
	if !strings.Contains(buildSystemPrompt(7500, false), "$75") {
		t.Error("expected $75")
	}
	if buildSystemPrompt(0, false) == "" {
		t.Error("zero deposit should still produce prompt")
	}
}

func TestBuildSystemPrompt_MoxieAppendix(t *testing.T) {
	without := buildSystemPrompt(5000, false)
	with := buildSystemPrompt(5000, true)
	if len(with) <= len(without) {
		t.Error("Moxie prompt should be longer")
	}
}

func TestBuildSystemPrompt_ClinicConfig(t *testing.T) {
	cfg := &clinic.Config{
		Name:            "Test Spa",
		Timezone:        "America/New_York",
		BookingPolicies: []string{"$50 deposit", "24h cancellation"},
	}
	prompt := buildSystemPrompt(5000, false, cfg)
	// Should inject time context
	if !strings.Contains(prompt, "CURRENT TIME") {
		t.Error("expected time context with clinic config")
	}
	// Should be longer than no-config version
	noConfigPrompt := buildSystemPrompt(5000, false)
	if len(prompt) <= len(noConfigPrompt) {
		t.Error("config prompt should be longer than no-config prompt")
	}
}

// ─── buildServiceHighlightsContext ─────────────────────────────

func TestBuildServiceHighlightsContext_NilConfig(t *testing.T) {
	if buildServiceHighlightsContext(nil, "botox") != "" {
		t.Error("nil config should return empty")
	}
}

func TestBuildServiceHighlightsContext_WithServices(t *testing.T) {
	cfg := &clinic.Config{Services: []string{"Tox (Wrinkle Relaxer)", "Chemical Peel"}}
	_ = buildServiceHighlightsContext(cfg, "I want botox") // no panic
}

// ─── isCapitalized ─────────────────────────────────────────────

func TestIsCapitalized_Coverage(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"Hello", true}, {"hello", false}, {"H", true}, {"h", false},
		{"", false}, {"123", false}, {"Andrea", true},
	}
	for _, tt := range tests {
		if got := isCapitalized(tt.s); got != tt.want {
			t.Errorf("isCapitalized(%q) = %v, want %v", tt.s, got, tt.want)
		}
	}
}

// ─── GetServiceVariants already-resolved guard ─────────────────

func TestGetServiceVariants_AlreadyResolved(t *testing.T) {
	cfg := &clinic.Config{
		ServiceVariants: map[string][]string{
			"filler": {"Dermal Filler", "Lip Filler"},
		},
	}
	if v := cfg.GetServiceVariants("Lip Filler"); len(v) != 0 {
		t.Errorf("resolved variant should return nil, got %v", v)
	}
	if v := cfg.GetServiceVariants("Dermal Filler"); len(v) != 0 {
		t.Errorf("resolved variant should return nil, got %v", v)
	}
	if v := cfg.GetServiceVariants("filler"); len(v) != 2 {
		t.Errorf("base should return 2 variants, got %v", v)
	}
}

// ─── formatIntroMessage ────────────────────────────────────────

func TestFormatIntroMessage_Coverage(t *testing.T) {
	r := formatIntroMessage(StartRequest{Intro: "I want botox", From: "+15551234567"}, "conv-1")
	if r == "" {
		t.Error("should return intro")
	}
	r = formatIntroMessage(StartRequest{From: "+15551234567"}, "conv-1")
	_ = r // no panic on empty intro
}
