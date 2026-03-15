package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brdocument "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

// ──────────────────────────────────────────────────────────────────────────────
// ClaudeLLM — conversation with Claude via AWS Bedrock Converse API.
// Maintains conversation history and supports tool calling.
// ──────────────────────────────────────────────────────────────────────────────

// ClaudeLLMConfig configures the Claude LLM client.
type ClaudeLLMConfig struct {
	ModelID      string
	SystemPrompt string
	Tools        []ToolDefinition
	MaxTokens    int
}

// ClaudeContentBlock is a content block in a Claude response.
type ClaudeContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// ClaudeToolUse represents a tool call from Claude.
type ClaudeToolUse struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ClaudeToolResultBlock represents a tool result to send back.
type ClaudeToolResultBlock struct {
	ToolUseID string
	Content   string
	IsError   bool
}

// ClaudeResponse represents Claude's response.
type ClaudeResponse struct {
	Content    []ClaudeContentBlock
	StopReason string
}

// ClaudeLLM manages conversation with Claude via AWS Bedrock Converse API.
type ClaudeLLM struct {
	cfg      ClaudeLLMConfig
	client   *bedrockruntime.Client
	logger   *slog.Logger
	messages []brtypes.Message

	mu sync.Mutex
}

// NewClaudeLLM creates a new Claude LLM client connected to Bedrock.
func NewClaudeLLM(ctx context.Context, cfg ClaudeLLMConfig, logger *slog.Logger) (*ClaudeLLM, error) {
	if logger == nil {
		logger = slog.Default()
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		return nil, fmt.Errorf("claude: load aws config: %w", err)
	}

	client := bedrockruntime.NewFromConfig(awsCfg)

	return &ClaudeLLM{
		cfg:      cfg,
		client:   client,
		logger:   logger,
		messages: make([]brtypes.Message, 0),
	}, nil
}

// SendMessage sends a user message to Claude and returns the response.
func (c *ClaudeLLM) SendMessage(ctx context.Context, role, text string) (*ClaudeResponse, error) {
	userMsg := brtypes.Message{
		Role: brtypes.ConversationRoleUser,
		Content: []brtypes.ContentBlock{
			&brtypes.ContentBlockMemberText{Value: text},
		},
	}

	c.mu.Lock()
	c.messages = append(c.messages, userMsg)
	messages := make([]brtypes.Message, len(c.messages))
	copy(messages, c.messages)
	c.mu.Unlock()

	resp, err := c.converse(ctx, messages)
	if err != nil {
		return nil, err
	}

	// Add assistant response to history
	c.mu.Lock()
	c.messages = append(c.messages, resp.sdkMessage)
	c.mu.Unlock()

	return &resp.ClaudeResponse, nil
}

// SendToolResults sends tool results back to Claude for follow-up response.
func (c *ClaudeLLM) SendToolResults(ctx context.Context, _ []ClaudeContentBlock, results []ClaudeToolResultBlock) (*ClaudeResponse, error) {
	var content []brtypes.ContentBlock
	for _, r := range results {
		status := brtypes.ToolResultStatusSuccess
		if r.IsError {
			status = brtypes.ToolResultStatusError
		}
		content = append(content, &brtypes.ContentBlockMemberToolResult{
			Value: brtypes.ToolResultBlock{
				ToolUseId: aws.String(r.ToolUseID),
				Content: []brtypes.ToolResultContentBlock{
					&brtypes.ToolResultContentBlockMemberText{Value: r.Content},
				},
				Status: status,
			},
		})
	}

	toolResultMsg := brtypes.Message{
		Role:    brtypes.ConversationRoleUser,
		Content: content,
	}

	c.mu.Lock()
	c.messages = append(c.messages, toolResultMsg)
	messages := make([]brtypes.Message, len(c.messages))
	copy(messages, c.messages)
	c.mu.Unlock()

	resp, err := c.converse(ctx, messages)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.messages = append(c.messages, resp.sdkMessage)
	c.mu.Unlock()

	return &resp.ClaudeResponse, nil
}

type converseResult struct {
	ClaudeResponse
	sdkMessage brtypes.Message
}

func (c *ClaudeLLM) converse(ctx context.Context, messages []brtypes.Message) (*converseResult, error) {
	// Build tool config
	var toolConfig *brtypes.ToolConfiguration
	if len(c.cfg.Tools) > 0 {
		tools := make([]brtypes.Tool, len(c.cfg.Tools))
		for i, t := range c.cfg.Tools {
			var raw interface{}
			_ = json.Unmarshal(t.InputSchema, &raw)
			tools[i] = &brtypes.ToolMemberToolSpec{
				Value: brtypes.ToolSpecification{
					Name:        aws.String(t.Name),
					Description: aws.String(t.Description),
					InputSchema: &brtypes.ToolInputSchemaMemberJson{Value: brdocument.NewLazyDocument(raw)},
				},
			}
		}
		toolConfig = &brtypes.ToolConfiguration{Tools: tools}
	}

	input := &bedrockruntime.ConverseInput{
		ModelId:  aws.String(c.cfg.ModelID),
		Messages: messages,
		System: []brtypes.SystemContentBlock{
			&brtypes.SystemContentBlockMemberText{Value: c.cfg.SystemPrompt},
		},
		InferenceConfig: &brtypes.InferenceConfiguration{
			MaxTokens:   aws.Int32(int32(c.cfg.MaxTokens)),
			Temperature: aws.Float32(0.7),
			TopP:        aws.Float32(0.9),
		},
		ToolConfig: toolConfig,
	}

	c.logger.Info("claude-llm: converse", "model", c.cfg.ModelID, "messages", len(messages))

	output, err := c.client.Converse(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("claude: converse: %w", err)
	}

	// Parse response
	var blocks []ClaudeContentBlock
	var sdkContent []brtypes.ContentBlock

	if msg, ok := output.Output.(*brtypes.ConverseOutputMemberMessage); ok {
		sdkContent = msg.Value.Content
		for _, block := range msg.Value.Content {
			switch v := block.(type) {
			case *brtypes.ContentBlockMemberText:
				blocks = append(blocks, ClaudeContentBlock{Type: "text", Text: v.Value})
			case *brtypes.ContentBlockMemberToolUse:
				inputJSON, _ := json.Marshal(v.Value.Input)
				blocks = append(blocks, ClaudeContentBlock{
					Type:  "tool_use",
					ID:    aws.ToString(v.Value.ToolUseId),
					Name:  aws.ToString(v.Value.Name),
					Input: inputJSON,
				})
			}
		}
	}

	stopReason := ""
	if output.StopReason != "" {
		stopReason = string(output.StopReason)
	}

	return &converseResult{
		ClaudeResponse: ClaudeResponse{
			Content:    blocks,
			StopReason: stopReason,
		},
		sdkMessage: brtypes.Message{
			Role:    brtypes.ConversationRoleAssistant,
			Content: sdkContent,
		},
	}, nil
}

