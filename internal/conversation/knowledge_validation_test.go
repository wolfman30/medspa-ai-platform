package conversation

import (
	"encoding/json"
	"testing"
)

func TestParseKnowledgePayloadStrings(t *testing.T) {
	raw := json.RawMessage(`["  Hello ", "World"]`)
	docs, err := ParseKnowledgePayload(raw)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(docs) != 2 || docs[0] != "Hello" || docs[1] != "World" {
		t.Fatalf("unexpected docs: %#v", docs)
	}
}

func TestParseKnowledgePayloadTitled(t *testing.T) {
	raw := json.RawMessage(`[
		{"title":"FAQ","content":"We do peels."},
		{"title":"Hours","content":""}
	]`)
	docs, err := ParseKnowledgePayload(raw)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(docs))
	}
	if docs[0] != "FAQ\n\nWe do peels." {
		t.Fatalf("unexpected doc[0]: %q", docs[0])
	}
	if docs[1] != "Hours" {
		t.Fatalf("unexpected doc[1]: %q", docs[1])
	}
}

func TestValidateKnowledgeDocumentsRejectsPHI(t *testing.T) {
	err := ValidateKnowledgeDocuments([]string{
		"Patient: John Doe",
	})
	if err == nil {
		t.Fatalf("expected PHI error")
	}
}

func TestValidateKnowledgeDocumentsAllowsGeneralInfo(t *testing.T) {
	err := ValidateKnowledgeDocuments([]string{
		"Forever 22 Med Spa offers chemical peels and microneedling.",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}
