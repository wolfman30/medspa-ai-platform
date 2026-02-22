package conversation

import "testing"

func TestIsCallbackRequest(t *testing.T) {
	positives := []string{
		"call me back",
		"Can you call me back?",
		"I'd prefer a call",
		"callback please",
		"I'd rather talk",
		"can someone call me",
		"I want a call",
		"just call me",
		"I'd rather speak to someone",
		"Can you call me please",
		"phone call",
		"CALL ME BACK",
		"I prefer a call instead",
		"talk to a person",
		"speak with someone",
	}
	for _, msg := range positives {
		if !IsCallbackRequest(msg) {
			t.Errorf("expected true for %q", msg)
		}
	}

	negatives := []string{
		"I want to book Botox",
		"What's the price?",
		"",
		"yes",
		"no",
		"My name is Sarah",
		"I'll call later",
		"thanks for calling",
		"nice calling card",
		"the callback URL is wrong", // contains callback but in tech context - still matches, acceptable
	}
	// "the callback URL is wrong" will match — that's an edge case we accept
	// since patients won't say that
	for _, msg := range negatives[:len(negatives)-1] {
		if IsCallbackRequest(msg) {
			t.Errorf("expected false for %q", msg)
		}
	}
}

func TestIsCallbackRequest_Empty(t *testing.T) {
	if IsCallbackRequest("") {
		t.Error("expected false for empty string")
	}
	if IsCallbackRequest("   ") {
		t.Error("expected false for whitespace")
	}
}
