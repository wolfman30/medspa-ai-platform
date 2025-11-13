package conversation

import (
	"context"
	"errors"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	openai "github.com/sashabaranov/go-openai"
	"github.com/wolfman30/medspa-ai-platform/internal/langchain"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestLangChainServiceStartConversation(t *testing.T) {
	mr := miniredis.RunT(t)
	t.Cleanup(mr.Close)

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	client := &fakeLangChainClient{responses: []string{"Hi there!"}}

	service := NewLangChainService(client, redisClient, logging.Default())
	resp, err := service.StartConversation(context.Background(), StartRequest{
		OrgID:    "org-1",
		LeadID:   "lead-1",
		Intro:    "I need Botox info",
		ClinicID: "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message != "Hi there!" {
		t.Fatalf("expected orchestrator reply, got %s", resp.Message)
	}
	if resp.ConversationID == "" {
		t.Fatal("expected conversation id to be set")
	}

	history, err := service.history.Load(context.Background(), resp.ConversationID)
	if err != nil {
		t.Fatalf("history load failed: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("expected 3 history messages, got %d", len(history))
	}
	if client.callCount != 1 {
		t.Fatalf("expected single orchestrator call, got %d", client.callCount)
	}
	if got := client.lastRequest.Metadata.ClinicID; got != defaultClinicSlug {
		t.Fatalf("expected default clinic id, got %s", got)
	}
}

func TestLangChainServiceProcessMessage(t *testing.T) {
	mr := miniredis.RunT(t)
	t.Cleanup(mr.Close)
	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	client := &fakeLangChainClient{responses: []string{"Sure, here's the info."}}
	svc := NewLangChainService(client, redisClient, logging.Default())

	convID := "conv_lead_123"
	initial := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: defaultOpenAISystemPrompt},
		{Role: openai.ChatMessageRoleUser, Content: "Hi"},
		{Role: openai.ChatMessageRoleAssistant, Content: "Hello!"},
	}
	if err := svc.history.Save(context.Background(), convID, initial); err != nil {
		t.Fatalf("failed to seed history: %v", err)
	}

	resp, err := svc.ProcessMessage(context.Background(), MessageRequest{
		ConversationID: convID,
		Message:        "Do you have openings tomorrow?",
		ClinicID:       "spa-west",
	})
	if err != nil {
		t.Fatalf("ProcessMessage error: %v", err)
	}
	if resp.Message == "" {
		t.Fatal("expected response message")
	}
	if client.lastRequest.Metadata.ClinicID != "spa-west" {
		t.Fatalf("expected clinic metadata to propagate")
	}
	if client.lastRequest.LatestInput != "Do you have openings tomorrow?" {
		t.Fatalf("expected latest input to match user message")
	}

	history, err := svc.history.Load(context.Background(), convID)
	if err != nil {
		t.Fatalf("history load failed: %v", err)
	}
	if len(history) != 5 {
		t.Fatalf("expected history to grow, got %d", len(history))
	}
}

func TestLangChainServiceErrorsPropagate(t *testing.T) {
	mr := miniredis.RunT(t)
	t.Cleanup(mr.Close)
	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	client := &fakeLangChainClient{err: errors.New("boom")}
	svc := NewLangChainService(client, redisClient, logging.Default())

	if _, err := svc.StartConversation(context.Background(), StartRequest{
		LeadID:   "lead",
		Intro:    "hello",
		ClinicID: "c1",
	}); err == nil {
		t.Fatal("expected error from orchestrator")
	}
}

type fakeLangChainClient struct {
	responses   []string
	callCount   int
	err         error
	lastRequest langchain.GenerateRequest
}

func (f *fakeLangChainClient) Generate(ctx context.Context, req langchain.GenerateRequest) (*langchain.GenerateResponse, error) {
	f.callCount++
	f.lastRequest = req
	if f.err != nil {
		return nil, f.err
	}
	index := f.callCount - 1
	if index >= len(f.responses) {
		index = len(f.responses) - 1
	}
	// mimic small processing delay
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(1 * time.Millisecond):
	}
	return &langchain.GenerateResponse{Message: f.responses[index]}, nil
}
