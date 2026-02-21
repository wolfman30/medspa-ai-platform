package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testGitHubNotifier struct {
	messages []string
}

func (n *testGitHubNotifier) Notify(_ context.Context, message string) error {
	n.messages = append(n.messages, message)
	return nil
}

func TestVerifyGitHubSignature(t *testing.T) {
	secret := "top-secret"
	payload := []byte(`{"hello":"world"}`)

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !verifyGitHubSignature(secret, payload, sig) {
		t.Fatal("expected signature to verify")
	}
	if verifyGitHubSignature(secret, payload, "sha256=badsignature") {
		t.Fatal("expected invalid signature to fail")
	}
}

func TestGitHubWebhookHandler_WorkflowFailure(t *testing.T) {
	secret := "github-secret"
	notifier := &testGitHubNotifier{}
	h := NewGitHubWebhookHandler(secret, notifier, nil)

	payload := []byte(`{
		"action": "completed",
		"workflow_run": {
			"name": "CI",
			"html_url": "https://github.com/acme/repo/actions/runs/123",
			"head_branch": "main",
			"status": "completed",
			"conclusion": "failure",
			"head_commit": {"message": "fix test"}
		}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "workflow_run")
	req.Header.Set("X-Hub-Signature-256", signed(secret, payload))
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(notifier.messages) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifier.messages))
	}
	if !strings.Contains(notifier.messages[0], "https://github.com/acme/repo/actions/runs/123") {
		t.Fatalf("expected run url in notification: %q", notifier.messages[0])
	}
}

func TestGitHubWebhookHandler_WorkflowSuccess(t *testing.T) {
	secret := "github-secret"
	notifier := &testGitHubNotifier{}
	h := NewGitHubWebhookHandler(secret, notifier, nil)

	payload := []byte(`{
		"action": "completed",
		"workflow_run": {
			"name": "Deploy Production",
			"html_url": "https://github.com/acme/repo/actions/runs/456",
			"head_branch": "main",
			"status": "completed",
			"conclusion": "success",
			"head_commit": {"message": "ship it\n\nmore details"}
		}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "workflow_run")
	req.Header.Set("X-Hub-Signature-256", signed(secret, payload))
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(notifier.messages) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifier.messages))
	}
	if !strings.Contains(notifier.messages[0], "✅ Deploy succeeded: ship it") {
		t.Fatalf("unexpected success notification: %q", notifier.messages[0])
	}
}

func TestGitHubWebhookHandler_InvalidSignature(t *testing.T) {
	h := NewGitHubWebhookHandler("secret", &testGitHubNotifier{}, nil)
	payload := []byte(`{"action":"completed"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "workflow_run")
	req.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func signed(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
