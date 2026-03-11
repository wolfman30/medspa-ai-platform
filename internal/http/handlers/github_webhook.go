package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// GitHubNotifier sends GitHub webhook notifications.
type GitHubNotifier interface {
	Notify(ctx context.Context, message string) error
}

// GitHubWebhookHandler handles GitHub workflow_run webhooks.
type GitHubWebhookHandler struct {
	secret   string
	notifier GitHubNotifier
	logger   *logging.Logger
}

func NewGitHubWebhookHandler(secret string, notifier GitHubNotifier, logger *logging.Logger) *GitHubWebhookHandler {
	if logger == nil {
		logger = logging.Default()
	}
	return &GitHubWebhookHandler{secret: strings.TrimSpace(secret), notifier: notifier, logger: logger}
}

func (h *GitHubWebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.secret == "" {
		h.logger.Error("github webhook secret not configured")
		http.Error(w, "webhook secret not configured", http.StatusInternalServerError)
		return
	}

	payload, err := io.ReadAll(io.LimitReader(r.Body, 10<<20)) // 10 MB max
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	sigHeader := r.Header.Get("X-Hub-Signature-256")
	if !verifyGitHubSignature(h.secret, payload, sigHeader) {
		h.logger.Warn("invalid github webhook signature")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	switch r.Header.Get("X-GitHub-Event") {
	case "workflow_run":
		var evt githubWorkflowRunEvent
		if err := json.Unmarshal(payload, &evt); err != nil {
			http.Error(w, "invalid JSON payload", http.StatusBadRequest)
			return
		}
		if evt.Action == "completed" {
			h.handleCompleted(r.Context(), evt)
		}
	case "pull_request":
		var evt githubPullRequestEvent
		if err := json.Unmarshal(payload, &evt); err != nil {
			http.Error(w, "invalid JSON payload", http.StatusBadRequest)
			return
		}
		h.handlePullRequest(r.Context(), evt)
	default:
		// Unhandled event type — ignore
	}

	w.WriteHeader(http.StatusOK)
}

func (h *GitHubWebhookHandler) handleCompleted(ctx context.Context, evt githubWorkflowRunEvent) {
	workflowName := firstNonEmpty(evt.WorkflowRun.Name, evt.Workflow.Name)
	branch := evt.WorkflowRun.HeadBranch
	runURL := evt.WorkflowRun.HTMLURL
	commitMessage := strings.TrimSpace(firstLine(evt.WorkflowRun.HeadCommit.Message))

	switch strings.ToLower(evt.WorkflowRun.Conclusion) {
	case "failure":
		h.logger.Error("github workflow failed",
			"workflow", workflowName,
			"run_url", runURL,
			"branch", branch,
		)
		msg := fmt.Sprintf("❌ GitHub workflow failed\nWorkflow: %s\nBranch: %s\nRun: %s", workflowName, branch, runURL)
		if h.notifier != nil {
			if err := h.notifier.Notify(ctx, msg); err != nil {
				h.logger.Error("failed to send GitHub failure notification", "error", err)
			}
		}
	case "success":
		prefix := "✅ CI passed"
		if strings.Contains(strings.ToLower(workflowName), "deploy") {
			prefix = "✅ Deploy succeeded"
		}
		msg := prefix
		if commitMessage != "" {
			msg = fmt.Sprintf("%s: %s", prefix, commitMessage)
		}
		if h.notifier != nil {
			if err := h.notifier.Notify(ctx, msg); err != nil {
				h.logger.Error("failed to send GitHub success notification", "error", err)
			}
		}
	}
}

func verifyGitHubSignature(secret string, payload []byte, header string) bool {
	if strings.TrimSpace(secret) == "" || strings.TrimSpace(header) == "" {
		return false
	}
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	providedHex := strings.TrimPrefix(header, prefix)
	providedSig, err := hex.DecodeString(providedHex)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	expectedSig := mac.Sum(nil)
	return hmac.Equal(expectedSig, providedSig)
}

// TelegramNotifier sends notifications to Telegram.
type TelegramNotifier struct {
	botToken string
	chatID   string
	client   *http.Client
}

func NewTelegramNotifier(botToken, chatID string, _ *logging.Logger) *TelegramNotifier {
	return &TelegramNotifier{
		botToken: strings.TrimSpace(botToken),
		chatID:   strings.TrimSpace(chatID),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (n *TelegramNotifier) Notify(ctx context.Context, message string) error {
	if n.botToken == "" || n.chatID == "" {
		return fmt.Errorf("telegram notifier not configured")
	}

	payload := map[string]interface{}{
		"chat_id":                  n.chatID,
		"text":                     message,
		"disable_web_page_preview": false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("http: Notify: %w", err)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("http: Notify: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("http: Notify: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("telegram API error: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

type githubPullRequestEvent struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		Title   string `json:"title"`
		HTMLURL string `json:"html_url"`
		User    struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"pull_request"`
}

func (h *GitHubWebhookHandler) handlePullRequest(ctx context.Context, evt githubPullRequestEvent) {
	switch evt.Action {
	case "opened", "synchronize", "reopened":
	default:
		return
	}

	pr := evt.PullRequest
	h.logger.Info("pull request event",
		"action", evt.Action,
		"number", evt.Number,
		"title", pr.Title,
		"author", pr.User.Login,
	)

	msg := fmt.Sprintf("🔍 PR #%d %s by %s: %s\nReview needed.\n%s",
		evt.Number, evt.Action, pr.User.Login, pr.Title, pr.HTMLURL)

	if h.notifier != nil {
		if err := h.notifier.Notify(ctx, msg); err != nil {
			h.logger.Error("failed to send PR notification", "error", err)
		}
	}
}

type githubWorkflowRunEvent struct {
	Action      string `json:"action"`
	WorkflowRun struct {
		Name       string `json:"name"`
		HTMLURL    string `json:"html_url"`
		HeadBranch string `json:"head_branch"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		HeadCommit struct {
			Message string `json:"message"`
		} `json:"head_commit"`
	} `json:"workflow_run"`
	Workflow struct {
		Name string `json:"name"`
	} `json:"workflow"`
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return "workflow_run"
}
