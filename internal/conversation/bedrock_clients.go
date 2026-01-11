package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

type bedrockConverseAPI interface {
	Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
	ConverseStream(ctx context.Context, params *bedrockruntime.ConverseStreamInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseStreamOutput, error)
}

type bedrockInvokeModelAPI interface {
	InvokeModel(ctx context.Context, params *bedrockruntime.InvokeModelInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error)
}

type BedrockLLMClient struct {
	api bedrockConverseAPI
}

func NewBedrockLLMClient(api bedrockConverseAPI) *BedrockLLMClient {
	if api == nil {
		panic("conversation: bedrock converse client cannot be nil")
	}
	return &BedrockLLMClient{api: api}
}

func (c *BedrockLLMClient) Complete(ctx context.Context, req LLMRequest) (LLMResponse, error) {
	if strings.TrimSpace(req.Model) == "" {
		return LLMResponse{}, errors.New("conversation: bedrock model id is required")
	}

	systemBlocks := make([]brtypes.SystemContentBlock, 0, len(req.System))
	for _, block := range req.System {
		if strings.TrimSpace(block) == "" {
			continue
		}
		systemBlocks = append(systemBlocks, &brtypes.SystemContentBlockMemberText{Value: block})
	}

	messages := make([]brtypes.Message, 0, len(req.Messages))
	for _, msg := range req.Messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}

		switch msg.Role {
		case ChatRoleSystem:
			systemBlocks = append(systemBlocks, &brtypes.SystemContentBlockMemberText{Value: content})
			continue
		case ChatRoleUser:
			messages = append(messages, brtypes.Message{
				Role: brtypes.ConversationRoleUser,
				Content: []brtypes.ContentBlock{
					&brtypes.ContentBlockMemberText{Value: content},
				},
			})
		case ChatRoleAssistant:
			messages = append(messages, brtypes.Message{
				Role: brtypes.ConversationRoleAssistant,
				Content: []brtypes.ContentBlock{
					&brtypes.ContentBlockMemberText{Value: content},
				},
			})
		default:
			return LLMResponse{}, fmt.Errorf("conversation: unsupported role %q", msg.Role)
		}
	}

	inference := &brtypes.InferenceConfiguration{}
	if req.MaxTokens > 0 {
		inference.MaxTokens = aws.Int32(req.MaxTokens)
	}
	// Allow callers to omit temperature by passing a negative value.
	if req.Temperature >= 0 {
		inference.Temperature = aws.Float32(req.Temperature)
	}
	if req.TopP != 0 {
		inference.TopP = aws.Float32(req.TopP)
	}
	if inference.MaxTokens == nil && inference.Temperature == nil && inference.TopP == nil {
		inference = nil
	}

	out, err := c.api.Converse(ctx, &bedrockruntime.ConverseInput{
		ModelId:         aws.String(req.Model),
		System:          systemBlocks,
		Messages:        messages,
		InferenceConfig: inference,
	})
	if err != nil {
		return LLMResponse{}, err
	}

	text, err := bedrockExtractOutputText(out)
	if err != nil {
		return LLMResponse{}, err
	}

	resp := LLMResponse{
		Text: strings.TrimSpace(text),
	}
	if out.StopReason != "" {
		resp.StopReason = string(out.StopReason)
	}
	if out.Usage != nil {
		resp.Usage = TokenUsage{
			InputTokens:  int32OrZero(out.Usage.InputTokens),
			OutputTokens: int32OrZero(out.Usage.OutputTokens),
			TotalTokens:  int32OrZero(out.Usage.TotalTokens),
		}
	}
	return resp, nil
}

// CompleteStream implements streaming completions using Bedrock's ConverseStream API.
// Returns a channel that emits partial text chunks as they arrive.
func (c *BedrockLLMClient) CompleteStream(ctx context.Context, req LLMRequest) (<-chan StreamChunk, error) {
	if strings.TrimSpace(req.Model) == "" {
		return nil, errors.New("conversation: bedrock model id is required")
	}

	systemBlocks := make([]brtypes.SystemContentBlock, 0, len(req.System))
	for _, block := range req.System {
		if strings.TrimSpace(block) == "" {
			continue
		}
		systemBlocks = append(systemBlocks, &brtypes.SystemContentBlockMemberText{Value: block})
	}

	messages := make([]brtypes.Message, 0, len(req.Messages))
	for _, msg := range req.Messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}

		switch msg.Role {
		case ChatRoleSystem:
			systemBlocks = append(systemBlocks, &brtypes.SystemContentBlockMemberText{Value: content})
			continue
		case ChatRoleUser:
			messages = append(messages, brtypes.Message{
				Role: brtypes.ConversationRoleUser,
				Content: []brtypes.ContentBlock{
					&brtypes.ContentBlockMemberText{Value: content},
				},
			})
		case ChatRoleAssistant:
			messages = append(messages, brtypes.Message{
				Role: brtypes.ConversationRoleAssistant,
				Content: []brtypes.ContentBlock{
					&brtypes.ContentBlockMemberText{Value: content},
				},
			})
		default:
			return nil, fmt.Errorf("conversation: unsupported role %q", msg.Role)
		}
	}

	inference := &brtypes.InferenceConfiguration{}
	if req.MaxTokens > 0 {
		inference.MaxTokens = aws.Int32(req.MaxTokens)
	}
	if req.Temperature >= 0 {
		inference.Temperature = aws.Float32(req.Temperature)
	}
	if req.TopP != 0 {
		inference.TopP = aws.Float32(req.TopP)
	}
	if inference.MaxTokens == nil && inference.Temperature == nil && inference.TopP == nil {
		inference = nil
	}

	out, err := c.api.ConverseStream(ctx, &bedrockruntime.ConverseStreamInput{
		ModelId:         aws.String(req.Model),
		System:          systemBlocks,
		Messages:        messages,
		InferenceConfig: inference,
	})
	if err != nil {
		return nil, err
	}

	chunks := make(chan StreamChunk, 32)

	go func() {
		defer close(chunks)

		stream := out.GetStream()
		if stream == nil {
			chunks <- StreamChunk{Error: errors.New("conversation: bedrock stream is nil"), Done: true}
			return
		}
		defer stream.Close()

		var usage TokenUsage
		for event := range stream.Events() {
			switch v := event.(type) {
			case *brtypes.ConverseStreamOutputMemberContentBlockDelta:
				if textDelta, ok := v.Value.Delta.(*brtypes.ContentBlockDeltaMemberText); ok {
					chunks <- StreamChunk{Text: textDelta.Value}
				}
			case *brtypes.ConverseStreamOutputMemberMetadata:
				if v.Value.Usage != nil {
					usage = TokenUsage{
						InputTokens:  int32OrZero(v.Value.Usage.InputTokens),
						OutputTokens: int32OrZero(v.Value.Usage.OutputTokens),
						TotalTokens:  int32OrZero(v.Value.Usage.TotalTokens),
					}
				}
			case *brtypes.ConverseStreamOutputMemberMessageStop:
				// Stream is complete
			}
		}

		if err := stream.Err(); err != nil {
			chunks <- StreamChunk{Error: err, Done: true}
			return
		}

		chunks <- StreamChunk{Done: true, Usage: usage}
	}()

	return chunks, nil
}

func bedrockExtractOutputText(out *bedrockruntime.ConverseOutput) (string, error) {
	if out == nil {
		return "", errors.New("conversation: bedrock response is nil")
	}
	msgOut, ok := out.Output.(*brtypes.ConverseOutputMemberMessage)
	if !ok {
		return "", errors.New("conversation: bedrock response did not include a message output")
	}
	if len(msgOut.Value.Content) == 0 {
		return "", errors.New("conversation: bedrock response message was empty")
	}

	var builder strings.Builder
	for _, block := range msgOut.Value.Content {
		if textBlock, ok := block.(*brtypes.ContentBlockMemberText); ok {
			builder.WriteString(textBlock.Value)
		}
	}
	outText := builder.String()
	if strings.TrimSpace(outText) == "" {
		return "", errors.New("conversation: bedrock response contained no text content blocks")
	}
	return outText, nil
}

func int32OrZero(v *int32) int32 {
	if v == nil {
		return 0
	}
	return *v
}

type BedrockEmbeddingClient struct {
	api bedrockInvokeModelAPI
}

func NewBedrockEmbeddingClient(api bedrockInvokeModelAPI) *BedrockEmbeddingClient {
	if api == nil {
		panic("conversation: bedrock runtime client cannot be nil")
	}
	return &BedrockEmbeddingClient{api: api}
}

func (c *BedrockEmbeddingClient) Embed(ctx context.Context, modelID string, texts []string) ([][]float32, error) {
	if strings.TrimSpace(modelID) == "" {
		return nil, errors.New("conversation: bedrock embedding model id is required")
	}
	if len(texts) == 0 {
		return nil, nil
	}

	embeddings := make([][]float32, 0, len(texts))
	for _, text := range texts {
		payload, err := json.Marshal(map[string]any{
			"inputText": text,
		})
		if err != nil {
			return nil, fmt.Errorf("conversation: embedding request marshal: %w", err)
		}

		out, err := c.api.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
			ModelId:     aws.String(modelID),
			ContentType: aws.String("application/json"),
			Accept:      aws.String("application/json"),
			Body:        payload,
		})
		if err != nil {
			return nil, err
		}

		var decoded struct {
			Embedding []float64 `json:"embedding"`
		}
		if err := json.Unmarshal(out.Body, &decoded); err != nil {
			return nil, fmt.Errorf("conversation: embedding response parse: %w", err)
		}
		if len(decoded.Embedding) == 0 {
			return nil, errors.New("conversation: embedding response was empty")
		}

		vec := make([]float32, len(decoded.Embedding))
		for i, f := range decoded.Embedding {
			vec[i] = float32(f)
		}
		embeddings = append(embeddings, vec)
	}

	return embeddings, nil
}
