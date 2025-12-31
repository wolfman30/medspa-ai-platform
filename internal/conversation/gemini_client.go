package conversation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// GeminiLLMClient implements LLMClient using Google's Gemini API.
type GeminiLLMClient struct {
	client  *genai.Client
	modelID string
}

// NewGeminiLLMClient creates a new Gemini LLM client.
func NewGeminiLLMClient(ctx context.Context, apiKey, modelID string) (*GeminiLLMClient, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("conversation: gemini api key is required")
	}
	if strings.TrimSpace(modelID) == "" {
		modelID = "gemini-2.5-flash"
	}

	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("conversation: failed to create gemini client: %w", err)
	}

	return &GeminiLLMClient{
		client:  client,
		modelID: modelID,
	}, nil
}

// Complete sends a completion request to Gemini and returns the response.
func (c *GeminiLLMClient) Complete(ctx context.Context, req LLMRequest) (LLMResponse, error) {
	model := c.client.GenerativeModel(c.modelID)

	// Configure model parameters
	if req.Temperature >= 0 {
		model.SetTemperature(req.Temperature)
	}
	if req.TopP > 0 {
		model.SetTopP(float32(req.TopP))
	}
	if req.MaxTokens > 0 {
		model.SetMaxOutputTokens(req.MaxTokens)
	}

	// Set system instruction from system prompts
	if len(req.System) > 0 {
		systemText := strings.Join(req.System, "\n\n")
		if strings.TrimSpace(systemText) != "" {
			model.SystemInstruction = genai.NewUserContent(genai.Text(systemText))
		}
	}

	// Build conversation history
	cs := model.StartChat()

	// Add all messages except the last one to history
	if len(req.Messages) > 1 {
		for _, msg := range req.Messages[:len(req.Messages)-1] {
			content := strings.TrimSpace(msg.Content)
			if content == "" {
				continue
			}

			// Skip system messages (already handled above)
			if msg.Role == ChatRoleSystem {
				continue
			}

			role := "user"
			if msg.Role == ChatRoleAssistant {
				role = "model"
			}

			cs.History = append(cs.History, &genai.Content{
				Role:  role,
				Parts: []genai.Part{genai.Text(content)},
			})
		}
	}

	// Send the last message
	if len(req.Messages) == 0 {
		return LLMResponse{}, errors.New("conversation: gemini requires at least one message")
	}

	lastMsg := req.Messages[len(req.Messages)-1]
	resp, err := cs.SendMessage(ctx, genai.Text(lastMsg.Content))
	if err != nil {
		return LLMResponse{}, fmt.Errorf("conversation: gemini completion failed: %w", err)
	}

	// Extract response text
	if len(resp.Candidates) == 0 {
		return LLMResponse{}, errors.New("conversation: gemini returned no candidates")
	}

	candidate := resp.Candidates[0]
	if candidate.Content == nil || len(candidate.Content.Parts) == 0 {
		return LLMResponse{}, errors.New("conversation: gemini returned empty content")
	}

	var responseText strings.Builder
	for _, part := range candidate.Content.Parts {
		if text, ok := part.(genai.Text); ok {
			responseText.WriteString(string(text))
		}
	}

	result := LLMResponse{
		Text:       strings.TrimSpace(responseText.String()),
		StopReason: string(candidate.FinishReason),
	}

	// Extract token usage if available
	if resp.UsageMetadata != nil {
		result.Usage = TokenUsage{
			InputTokens:  resp.UsageMetadata.PromptTokenCount,
			OutputTokens: resp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:  resp.UsageMetadata.TotalTokenCount,
		}
	}

	return result, nil
}

// Close releases resources held by the Gemini client.
func (c *GeminiLLMClient) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}
