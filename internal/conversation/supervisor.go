package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// SupervisorMode controls how worker handles supervisor decisions.
type SupervisorMode string

const (
	SupervisorModeWarn  SupervisorMode = "warn"
	SupervisorModeBlock SupervisorMode = "block"
	SupervisorModeEdit  SupervisorMode = "edit"
)

// SupervisorAction is the recommendation returned by a supervisor.
type SupervisorAction string

const (
	SupervisorActionAllow SupervisorAction = "allow"
	SupervisorActionBlock SupervisorAction = "block"
	SupervisorActionEdit  SupervisorAction = "edit"
)

// SupervisorDecision represents the review result.
type SupervisorDecision struct {
	Action     SupervisorAction
	EditedText string
	Reason     string
}

// SupervisorRequest captures context for supervising a draft reply.
type SupervisorRequest struct {
	OrgID          string
	ConversationID string
	LeadID         string
	Channel        Channel
	UserMessage    string
	DraftMessage   string
}

// Supervisor evaluates draft replies for safety/compliance.
type Supervisor interface {
	Review(ctx context.Context, req SupervisorRequest) (SupervisorDecision, error)
}

// SupervisorConfig configures the LLM supervisor behavior.
type SupervisorConfig struct {
	Model        string
	Timeout      time.Duration
	MaxTokens    int32
	Temperature  float32
	SystemPrompt string
}

// LLMSupervisor runs a lightweight LLM pass to review responses.
type LLMSupervisor struct {
	client       LLMClient
	model        string
	timeout      time.Duration
	maxTokens    int32
	temperature  float32
	systemPrompt string
	logger       *logging.Logger
}

const defaultSupervisorPrompt = `You are a compliance reviewer for MedSpa AI SMS replies.

Review the assistant's draft reply and decide if it is safe and compliant.

Rules:
- Do NOT give medical advice or diagnosis. If the draft does, revise to a general guidance deflection.
- Do NOT claim real-time calendar access or confirm bookings. The clinic will call to confirm.
- Do NOT request payment card details over SMS.
- Keep replies brief and professional.

Return ONLY JSON in this exact format:
{"action":"allow|edit|block","edited_text":"","reason":""}

Use action:
- "allow" when the draft is acceptable.
- "edit" when you can safely fix it; provide the full revised reply in edited_text.
- "block" when it should not be sent at all; leave edited_text empty.
`

// NewLLMSupervisor constructs an LLM-backed supervisor.
func NewLLMSupervisor(client LLMClient, cfg SupervisorConfig, logger *logging.Logger) *LLMSupervisor {
	if client == nil {
		panic("conversation: supervisor llm client cannot be nil")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		panic("conversation: supervisor model id required")
	}
	if logger == nil {
		logger = logging.Default()
	}
	prompt := strings.TrimSpace(cfg.SystemPrompt)
	if prompt == "" {
		prompt = defaultSupervisorPrompt
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 256
	}
	return &LLMSupervisor{
		client:       client,
		model:        cfg.Model,
		timeout:      cfg.Timeout,
		maxTokens:    maxTokens,
		temperature:  cfg.Temperature,
		systemPrompt: prompt,
		logger:       logger,
	}
}

// Review evaluates a draft response and returns the supervisor decision.
func (s *LLMSupervisor) Review(ctx context.Context, req SupervisorRequest) (SupervisorDecision, error) {
	if s == nil {
		return SupervisorDecision{}, errors.New("conversation: supervisor is nil")
	}
	reviewCtx := ctx
	var cancel context.CancelFunc
	if s.timeout > 0 {
		reviewCtx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}
	user := strings.TrimSpace(req.UserMessage)
	draft := strings.TrimSpace(req.DraftMessage)
	if draft == "" {
		return SupervisorDecision{Action: SupervisorActionAllow}, nil
	}

	prompt := fmt.Sprintf("User message:\n%s\n\nAssistant draft:\n%s\n", user, draft)
	resp, err := s.client.Complete(reviewCtx, LLMRequest{
		Model:  s.model,
		System: []string{s.systemPrompt},
		Messages: []ChatMessage{
			{Role: ChatRoleUser, Content: prompt},
		},
		MaxTokens:   s.maxTokens,
		Temperature: s.temperature,
	})
	if err != nil {
		return SupervisorDecision{}, err
	}
	return parseSupervisorDecision(resp.Text)
}

func parseSupervisorDecision(raw string) (SupervisorDecision, error) {
	text := sanitizeSupervisorJSON(raw)
	if text == "" {
		return SupervisorDecision{}, errors.New("conversation: supervisor empty response")
	}

	payload, err := decodeSupervisorPayload(text)
	if err != nil {
		return SupervisorDecision{}, err
	}
	action, ok := normalizeSupervisorAction(payload.Action)
	if !ok {
		return SupervisorDecision{}, fmt.Errorf("conversation: supervisor action invalid: %q", payload.Action)
	}
	return SupervisorDecision{
		Action:     action,
		EditedText: strings.TrimSpace(payload.EditedText),
		Reason:     strings.TrimSpace(payload.Reason),
	}, nil
}

type supervisorPayload struct {
	Action     string `json:"action"`
	EditedText string `json:"edited_text"`
	Reason     string `json:"reason"`
}

func decodeSupervisorPayload(text string) (supervisorPayload, error) {
	var payload supervisorPayload
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return supervisorPayload{}, err
	}
	return payload, nil
}

func normalizeSupervisorAction(raw string) (SupervisorAction, bool) {
	action := SupervisorAction(strings.ToLower(strings.TrimSpace(raw)))
	switch action {
	case SupervisorActionAllow, SupervisorActionBlock, SupervisorActionEdit:
		return action, true
	default:
		return "", false
	}
}

func sanitizeSupervisorJSON(raw string) string {
	text := stripCodeFence(raw)
	text = extractJSONObject(text)
	return strings.TrimSpace(text)
}

func stripCodeFence(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}

func extractJSONObject(text string) string {
	if strings.HasPrefix(text, "{") {
		return text
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		return text[start : end+1]
	}
	return text
}

// ParseSupervisorMode normalizes a supervisor mode string.
func ParseSupervisorMode(raw string) SupervisorMode {
	mode := SupervisorMode(strings.ToLower(strings.TrimSpace(raw)))
	switch mode {
	case SupervisorModeWarn, SupervisorModeBlock, SupervisorModeEdit:
		return mode
	default:
		return SupervisorModeWarn
	}
}
