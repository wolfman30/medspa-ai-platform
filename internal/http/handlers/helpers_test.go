package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseConversationID(t *testing.T) {
	orgID, phone, ok := parseConversationID("sms:org-1:15551234567")
	if !ok {
		t.Fatalf("expected valid conversation id")
	}
	if orgID != "org-1" || phone != "15551234567" {
		t.Fatalf("unexpected parsed values org=%s phone=%s", orgID, phone)
	}
}

func TestParseConversationIDVoice(t *testing.T) {
	orgID, session, ok := parseConversationID("voice:org-1:session_abc123")
	if !ok || orgID != "org-1" || session != "session_abc123" {
		t.Fatalf("expected voice parse to work, got orgID=%q session=%q ok=%v", orgID, session, ok)
	}
	// Voice ID with colons in session
	orgID, session, ok = parseConversationID("voice:org-1:v3:abc:def")
	if !ok || orgID != "org-1" || session != "v3:abc:def" {
		t.Fatalf("expected colon session parse, got orgID=%q session=%q ok=%v", orgID, session, ok)
	}
}

func TestParseConversationIDInvalid(t *testing.T) {
	cases := []string{
		"email:org-1:15551234567",
		"sms:org-1",
		"",
	}
	for _, input := range cases {
		if _, _, ok := parseConversationID(input); ok {
			t.Fatalf("expected invalid conversation id for %q", input)
		}
	}
}

func TestJSONError(t *testing.T) {
	rec := httptest.NewRecorder()
	jsonError(rec, "oops", http.StatusTeapot)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("expected status %d, got %d", http.StatusTeapot, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected content type application/json, got %q", ct)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode json response: %v", err)
	}
	if body["error"] != "oops" {
		t.Fatalf("unexpected error message %q", body["error"])
	}
}

func TestNormalizePhoneDigits(t *testing.T) {
	if got := normalizePhoneDigits(" +1 (555) 123-4567 "); got != "15551234567" {
		t.Fatalf("unexpected digits %q", got)
	}
	if got := normalizePhoneDigits("abc"); got != "" {
		t.Fatalf("expected empty digits, got %q", got)
	}
}

func TestDefaultString(t *testing.T) {
	if got := defaultString("value", "fallback"); got != "value" {
		t.Fatalf("unexpected value %q", got)
	}
	if got := defaultString("   ", "fallback"); got != "fallback" {
		t.Fatalf("expected fallback, got %q", got)
	}
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	payload := map[string]string{"status": "ok"}

	writeJSON(rec, http.StatusCreated, payload)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected content type application/json, got %q", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode json response: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("unexpected body: %#v", body)
	}
}
