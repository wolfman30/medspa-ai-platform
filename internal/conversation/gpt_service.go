package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	openai "github.com/sashabaranov/go-openai"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

const (
	conversationTTL           = 24 * time.Hour
	defaultOpenAISystemPrompt = "You are MedSpa AI Concierge, a warm, trustworthy assistant for a medical spa. Keep responses short, actionable, and compliant with HIPAA. Never invent medical advice. Guide leads toward booking by clarifying needs, suggesting available services, and offering to reserve time with a provider."
)

type chatClient interface {
	CreateChatCompletion(ctx context.Context, request openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error)
}

// GPTService produces conversation responses using OpenAI and stores context in Redis.
type GPTService struct {
	client chatClient
	redis  *redis.Client
	rag    RAGRetriever
	model  string
	logger *logging.Logger
}

// NewGPTService returns a GPT-backed Service implementation.
func NewGPTService(client chatClient, redisClient *redis.Client, rag RAGRetriever, model string, logger *logging.Logger) *GPTService {
	if client == nil {
		panic("conversation: chat client cannot be nil")
	}
	if redisClient == nil {
		panic("conversation: redis client cannot be nil")
	}
	if model == "" {
		model = "gpt-4o-mini"
	}
	if logger == nil {
		logger = logging.Default()
	}

	return &GPTService{
		client: client,
		redis:  redisClient,
		rag:    rag,
		model:  model,
		logger: logger,
	}
}

// StartConversation opens a new thread, generates the first assistant response, and persists context.
func (s *GPTService) StartConversation(ctx context.Context, req StartRequest) (*Response, error) {
	conversationID := fmt.Sprintf("conv_%s_%d", req.LeadID, time.Now().UnixNano())
	history := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: defaultOpenAISystemPrompt},
	}
	history = s.appendContext(ctx, history, req.ClinicID, req.Intro)
	history = append(history, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: formatIntroMessage(req)})

	reply, err := s.generateResponse(ctx, history)
	if err != nil {
		return nil, err
	}
	history = append(history, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleAssistant,
		Content: reply,
	})

	if err := s.saveHistory(ctx, conversationID, history); err != nil {
		return nil, err
	}

	return &Response{
		ConversationID: conversationID,
		Message:        reply,
		Timestamp:      time.Now().UTC(),
	}, nil
}

// ProcessMessage continues an existing conversation with Redis-backed context.
func (s *GPTService) ProcessMessage(ctx context.Context, req MessageRequest) (*Response, error) {
	history, err := s.loadHistory(ctx, req.ConversationID)
	if err != nil {
		return nil, err
	}

	history = s.appendContext(ctx, history, req.ClinicID, req.Message)
	history = append(history, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: req.Message,
	})

	reply, err := s.generateResponse(ctx, history)
	if err != nil {
		return nil, err
	}
	history = append(history, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleAssistant,
		Content: reply,
	})

	if err := s.saveHistory(ctx, req.ConversationID, history); err != nil {
		return nil, err
	}

	return &Response{
		ConversationID: req.ConversationID,
		Message:        reply,
		Timestamp:      time.Now().UTC(),
	}, nil
}

func (s *GPTService) generateResponse(ctx context.Context, history []openai.ChatCompletionMessage) (string, error) {
	req := openai.ChatCompletionRequest{
		Model:    s.model,
		Messages: history,
	}
	resp, err := s.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("conversation: openai completion failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("conversation: openai returned no choices")
	}

	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}

func (s *GPTService) saveHistory(ctx context.Context, conversationID string, history []openai.ChatCompletionMessage) error {
	data, err := json.Marshal(history)
	if err != nil {
		return fmt.Errorf("conversation: failed to marshal history: %w", err)
	}
	if err := s.redis.Set(ctx, conversationKey(conversationID), data, conversationTTL).Err(); err != nil {
		return fmt.Errorf("conversation: failed to persist history: %w", err)
	}
	return nil
}

func (s *GPTService) loadHistory(ctx context.Context, conversationID string) ([]openai.ChatCompletionMessage, error) {
	data, err := s.redis.Get(ctx, conversationKey(conversationID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, fmt.Errorf("conversation: unknown conversation %s", conversationID)
		}
		return nil, fmt.Errorf("conversation: failed to load history: %w", err)
	}

	var history []openai.ChatCompletionMessage
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, fmt.Errorf("conversation: failed to decode history: %w", err)
	}
	return history, nil
}

func formatIntroMessage(req StartRequest) string {
	builder := strings.Builder{}
	builder.WriteString("Lead introduction:\n")
	if req.LeadID != "" {
		builder.WriteString(fmt.Sprintf("Lead ID: %s\n", req.LeadID))
	}
	builder.WriteString(fmt.Sprintf("Source: %s\n", req.Source))
	builder.WriteString(fmt.Sprintf("Message: %s", req.Intro))
	return builder.String()
}

func conversationKey(id string) string {
	return fmt.Sprintf("conversation:%s", id)
}

func (s *GPTService) appendContext(ctx context.Context, history []openai.ChatCompletionMessage, clinicID, query string) []openai.ChatCompletionMessage {
	if s.rag == nil || strings.TrimSpace(query) == "" {
		return history
	}
	snippets, err := s.rag.Query(ctx, clinicID, query, 3)
	if err != nil || len(snippets) == 0 {
		if err != nil {
			s.logger.Error("failed to retrieve RAG context", "error", err)
		}
		return history
	}
	builder := strings.Builder{}
	builder.WriteString("Relevant clinic context:\n")
	for i, snippet := range snippets {
		builder.WriteString(fmt.Sprintf("%d. %s\n", i+1, snippet))
	}
	history = append(history, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleSystem,
		Content: builder.String(),
	})
	return history
}
