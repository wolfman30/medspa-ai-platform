package conversation

import "context"

const (
	ChatRoleSystem    = "system"
	ChatRoleUser      = "user"
	ChatRoleAssistant = "assistant"
)

// ChatMessage is an internal message representation that can include system prompts.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type TokenUsage struct {
	InputTokens  int32
	OutputTokens int32
	TotalTokens  int32
}

type LLMRequest struct {
	Model       string
	System      []string
	Messages    []ChatMessage
	MaxTokens   int32
	Temperature float32
	TopP        float32
}

type LLMResponse struct {
	Text       string
	Usage      TokenUsage
	StopReason string
}

type LLMClient interface {
	Complete(ctx context.Context, req LLMRequest) (LLMResponse, error)
}
