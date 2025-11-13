package conversation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	openai "github.com/sashabaranov/go-openai"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const defaultOpenAISystemPrompt = "You are MedSpa AI Concierge, a warm, trustworthy assistant for a medical spa. Keep responses short, actionable, and compliant with HIPAA. Never invent medical advice. Guide leads toward booking by clarifying needs, suggesting available services, and offering to reserve time with a provider."

var gptTracer = otel.Tracer("medspa.internal.conversation.gpt")

type chatClient interface {
	CreateChatCompletion(ctx context.Context, request openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error)
}

// GPTService produces conversation responses using OpenAI and stores context in Redis.
type GPTService struct {
	client  chatClient
	rag     RAGRetriever
	model   string
	logger  *logging.Logger
	history *historyStore
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
		client:  client,
		rag:     rag,
		model:   model,
		logger:  logger,
		history: newHistoryStore(redisClient, gptTracer),
	}
}

// StartConversation opens a new thread, generates the first assistant response, and persists context.
func (s *GPTService) StartConversation(ctx context.Context, req StartRequest) (*Response, error) {
	ctx, span := gptTracer.Start(ctx, "conversation.start")
	defer span.End()

	conversationID := req.ConversationID
	if conversationID == "" {
		base := req.LeadID
		if base == "" {
			base = uuid.NewString()
		}
		conversationID = fmt.Sprintf("conv_%s_%d", base, time.Now().UnixNano())
	}
	span.SetAttributes(
		attribute.String("medspa.org_id", req.OrgID),
		attribute.String("medspa.conversation_id", conversationID),
		attribute.String("medspa.channel", string(req.Channel)),
	)

	history := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: defaultOpenAISystemPrompt},
	}
	history = s.appendContext(ctx, history, req.ClinicID, req.Intro)
	history = append(history, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: formatIntroMessage(req, conversationID),
	})

	reply, err := s.generateResponse(ctx, history)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	history = append(history, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleAssistant,
		Content: reply,
	})

	if err := s.history.Save(ctx, conversationID, history); err != nil {
		span.RecordError(err)
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
	if strings.TrimSpace(req.ConversationID) == "" {
		return nil, errors.New("conversation: conversationID required")
	}

	ctx, span := gptTracer.Start(ctx, "conversation.message")
	defer span.End()
	span.SetAttributes(
		attribute.String("medspa.org_id", req.OrgID),
		attribute.String("medspa.conversation_id", req.ConversationID),
		attribute.String("medspa.channel", string(req.Channel)),
	)

	history, err := s.history.Load(ctx, req.ConversationID)
	if err != nil {
		span.RecordError(err)
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

	if err := s.history.Save(ctx, req.ConversationID, history); err != nil {
		span.RecordError(err)
		return nil, err
	}

	return &Response{
		ConversationID: req.ConversationID,
		Message:        reply,
		Timestamp:      time.Now().UTC(),
	}, nil
}

func (s *GPTService) generateResponse(ctx context.Context, history []openai.ChatCompletionMessage) (string, error) {
	ctx, span := gptTracer.Start(ctx, "conversation.openai")
	defer span.End()

	req := openai.ChatCompletionRequest{
		Model:    s.model,
		Messages: history,
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := s.client.CreateChatCompletion(callCtx, req)
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("conversation: openai completion failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		err := errors.New("conversation: openai returned no choices")
		span.RecordError(err)
		return "", err
	}
	if span.IsRecording() {
		span.SetAttributes(
			attribute.Int("medspa.openai.choices", len(resp.Choices)),
		)
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}

func formatIntroMessage(req StartRequest, conversationID string) string {
	builder := strings.Builder{}
	builder.WriteString("Lead introduction:\n")
	builder.WriteString(fmt.Sprintf("Conversation ID: %s\n", conversationID))
	if req.OrgID != "" {
		builder.WriteString(fmt.Sprintf("Org ID: %s\n", req.OrgID))
	}
	if req.LeadID != "" {
		builder.WriteString(fmt.Sprintf("Lead ID: %s\n", req.LeadID))
	}
	if req.Channel != ChannelUnknown {
		builder.WriteString(fmt.Sprintf("Channel: %s\n", req.Channel))
	}
	if req.Source != "" {
		builder.WriteString(fmt.Sprintf("Source: %s\n", req.Source))
	}
	if req.From != "" {
		builder.WriteString(fmt.Sprintf("From: %s\n", req.From))
	}
	if req.To != "" {
		builder.WriteString(fmt.Sprintf("To: %s\n", req.To))
	}
	if len(req.Metadata) > 0 {
		builder.WriteString("Metadata:\n")
		for k, v := range req.Metadata {
			builder.WriteString(fmt.Sprintf("- %s: %s\n", k, v))
		}
	}
	builder.WriteString(fmt.Sprintf("Message: %s", req.Intro))
	return builder.String()
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
