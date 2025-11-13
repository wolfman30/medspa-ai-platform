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
	"github.com/wolfman30/medspa-ai-platform/internal/langchain"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var (
	langchainTracer   = otel.Tracer("medspa.internal.conversation.langchain")
	defaultClinicSlug = "global"
)

// LangChainService delegates generation to the Python LangChain orchestrator.
type langchainGenerator interface {
	Generate(ctx context.Context, req langchain.GenerateRequest) (*langchain.GenerateResponse, error)
}

type LangChainService struct {
	client  langchainGenerator
	history *historyStore
	logger  *logging.Logger
}

// NewLangChainService constructs a conversation.Service backed by LangChain.
func NewLangChainService(client langchainGenerator, redisClient *redis.Client, logger *logging.Logger) *LangChainService {
	if client == nil {
		panic("conversation: langchain client cannot be nil")
	}
	if redisClient == nil {
		panic("conversation: redis client cannot be nil")
	}
	if logger == nil {
		logger = logging.Default()
	}
	return &LangChainService{
		client:  client,
		history: newHistoryStore(redisClient, langchainTracer),
		logger:  logger,
	}
}

// StartConversation mirrors GPTService but proxies to LangChain for responses.
func (s *LangChainService) StartConversation(ctx context.Context, req StartRequest) (*Response, error) {
	ctx, span := langchainTracer.Start(ctx, "conversation.start")
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

	clinicID := clinicOrDefault(req.ClinicID)

	history := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: defaultOpenAISystemPrompt},
		{
			Role:    openai.ChatMessageRoleUser,
			Content: formatIntroMessage(req, conversationID),
		},
	}

	reply, err := s.generate(ctx, langchain.GenerateMetadata{
		ConversationID: conversationID,
		ClinicID:       clinicID,
		OrgID:          req.OrgID,
		LeadID:         req.LeadID,
		Channel:        string(req.Channel),
		Metadata:       req.Metadata,
	}, req.Intro, history)
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

// ProcessMessage continues the thread using the orchestrator.
func (s *LangChainService) ProcessMessage(ctx context.Context, req MessageRequest) (*Response, error) {
	if strings.TrimSpace(req.ConversationID) == "" {
		return nil, errors.New("conversation: conversationID required")
	}

	ctx, span := langchainTracer.Start(ctx, "conversation.message")
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

	history = append(history, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: req.Message,
	})

	reply, err := s.generate(ctx, langchain.GenerateMetadata{
		ConversationID: req.ConversationID,
		ClinicID:       clinicOrDefault(req.ClinicID),
		OrgID:          req.OrgID,
		LeadID:         req.LeadID,
		Channel:        string(req.Channel),
		Metadata:       req.Metadata,
	}, req.Message, history)
	if err != nil {
		span.RecordError(err)
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

func (s *LangChainService) generate(ctx context.Context, meta langchain.GenerateMetadata, latest string, history []openai.ChatCompletionMessage) (string, error) {
	resp, err := s.client.Generate(ctx, langchain.GenerateRequest{
		Metadata:    meta,
		History:     history,
		LatestInput: latest,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Message), nil
}

func clinicOrDefault(id string) string {
	if strings.TrimSpace(id) == "" {
		return defaultClinicSlug
	}
	return id
}
