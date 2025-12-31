package conversation

import "testing"

func TestParseSupervisorDecisionAllowsValidJSON(t *testing.T) {
	decision, err := parseSupervisorDecision(`{"action":"allow","edited_text":"","reason":"ok"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Action != SupervisorActionAllow {
		t.Fatalf("expected allow action, got %q", decision.Action)
	}
}

func TestParseSupervisorDecisionHandlesCodeFence(t *testing.T) {
	raw := "```json\n{\"action\":\"edit\",\"edited_text\":\"fixed\",\"reason\":\"compliance\"}\n```"
	decision, err := parseSupervisorDecision(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Action != SupervisorActionEdit {
		t.Fatalf("expected edit action, got %q", decision.Action)
	}
	if decision.EditedText != "fixed" {
		t.Fatalf("expected edited text, got %q", decision.EditedText)
	}
}

func TestParseSupervisorDecisionExtractsEmbeddedJSON(t *testing.T) {
	raw := "note: {\"action\":\"block\",\"edited_text\":\"\",\"reason\":\"unsafe\"} thanks"
	decision, err := parseSupervisorDecision(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Action != SupervisorActionBlock {
		t.Fatalf("expected block action, got %q", decision.Action)
	}
}

func TestParseSupervisorDecisionRejectsInvalidAction(t *testing.T) {
	_, err := parseSupervisorDecision(`{"action":"maybe","edited_text":"","reason":""}`)
	if err == nil {
		t.Fatalf("expected error for invalid action")
	}
}

func TestParseSupervisorDecisionRejectsEmpty(t *testing.T) {
	_, err := parseSupervisorDecision("   ")
	if err == nil {
		t.Fatalf("expected error for empty response")
	}
}

func TestParseSupervisorModeNormalizesInput(t *testing.T) {
	cases := map[string]SupervisorMode{
		"warn":   SupervisorModeWarn,
		"WARN":   SupervisorModeWarn,
		" edit ": SupervisorModeEdit,
		"block":  SupervisorModeBlock,
		"":       SupervisorModeWarn,
		"nope":   SupervisorModeWarn,
	}
	for raw, want := range cases {
		if got := ParseSupervisorMode(raw); got != want {
			t.Fatalf("ParseSupervisorMode(%q) = %q, want %q", raw, got, want)
		}
	}
}
