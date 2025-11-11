package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	openai "github.com/sashabaranov/go-openai"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestGPTService_StartConversation_PersistsHistory(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	mockOpenAI := &stubChatClient{
		response: openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{
				{Message: openai.ChatCompletionMessage{Content: "Hi there!"}},
			},
		},
	}

	service := NewGPTService(mockOpenAI, client, nil, "gpt-5-mini", logging.Default())
	resp, err := service.StartConversation(context.Background(), StartRequest{
		LeadID:  "lead-123",
		Intro:   "Need dermaplaning",
		Source:  "web",
		Channel: ChannelSMS,
		OrgID:   "org-1",
	})
	if err != nil {
		t.Fatalf("StartConversation returned error: %v", err)
	}
	if resp.Message != "Hi there!" {
		t.Fatalf("expected assistant reply, got %s", resp.Message)
	}

	key := conversationKey(resp.ConversationID)
	raw, err := mr.DB(0).Get(key)
	if err != nil {
		t.Fatalf("failed to read history from redis: %v", err)
	}
	var history []openai.ChatCompletionMessage
	if err := json.Unmarshal([]byte(raw), &history); err != nil {
		t.Fatalf("failed to decode stored history: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("expected 3 messages in history, got %d", len(history))
	}
	if history[2].Role != openai.ChatMessageRoleAssistant || history[2].Content != "Hi there!" {
		t.Fatalf("expected assistant reply stored, got %#v", history[2])
	}
}

func TestGPTService_ProcessMessage_LoadsExistingHistory(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	mockOpenAI := &stubChatClient{
		response: openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{
				{Message: openai.ChatCompletionMessage{Content: "Sure, Friday works."}},
			},
		},
	}

	service := NewGPTService(mockOpenAI, client, nil, "gpt-5-mini", logging.Default())
	startResp, err := service.StartConversation(context.Background(), StartRequest{
		LeadID:  "lead-1",
		Intro:   "Book facial",
		Source:  "sms",
		Channel: ChannelSMS,
		OrgID:   "org-1",
	})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}

	resp, err := service.ProcessMessage(context.Background(), MessageRequest{
		ConversationID: startResp.ConversationID,
		Message:        "Do you have Friday afternoon?",
		Channel:        ChannelSMS,
		OrgID:          "org-1",
	})
	if err != nil {
		t.Fatalf("ProcessMessage returned error: %v", err)
	}
	if resp.Message != "Sure, Friday works." {
		t.Fatalf("unexpected assistant reply: %s", resp.Message)
	}

	var history []openai.ChatCompletionMessage
	raw, err := mr.DB(0).Get(conversationKey(startResp.ConversationID))
	if err != nil {
		t.Fatalf("failed to fetch stored history: %v", err)
	}
	if err := json.Unmarshal([]byte(raw), &history); err != nil {
		t.Fatalf("failed to decode history: %v", err)
	}
	if history[len(history)-2].Content != "Do you have Friday afternoon?" {
		t.Fatalf("expected user message appended, got %#v", history[len(history)-2])
	}
}

func TestGPTService_ProcessMessage_UnknownConversation(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	service := NewGPTService(&stubChatClient{}, client, nil, "gpt-5-mini", logging.Default())

	_, err := service.ProcessMessage(context.Background(), MessageRequest{
		ConversationID: "conv_missing",
		Message:        "hello",
		Channel:        ChannelSMS,
		OrgID:          "org-1",
	})
	if err == nil || !strings.Contains(err.Error(), "unknown conversation") {
		t.Fatalf("expected unknown conversation error, got %v", err)
	}
}

func TestGPTService_StartConversation_OpenAIError(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	mock := &stubChatClient{err: errors.New("quota exceeded")}
	service := NewGPTService(mock, client, nil, "gpt-5-mini", logging.Default())

	_, err := service.StartConversation(context.Background(), StartRequest{LeadID: "lead"})
	if err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("expected propagated OpenAI error, got %v", err)
	}
}

type stubChatClient struct {
	response openai.ChatCompletionResponse
	err      error
	lastReq  openai.ChatCompletionRequest
}

func (s *stubChatClient) CreateChatCompletion(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
	s.lastReq = req
	if s.err != nil {
		return openai.ChatCompletionResponse{}, s.err
	}
	return s.response, nil
}

type stubRAG struct {
	contexts   []string
	err        error
	lastClinic string
	lastQuery  string
}

func (s *stubRAG) Query(ctx context.Context, clinicID string, query string, topK int) ([]string, error) {
	s.lastClinic = clinicID
	s.lastQuery = query
	if s.err != nil {
		return nil, s.err
	}
	return s.contexts, nil
}

func TestGPTService_UsesRAGContext(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	mockOpenAI := &stubChatClient{
		response: openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{
				{Message: openai.ChatCompletionMessage{Content: "Response"}},
			},
		},
	}
	rag := &stubRAG{contexts: []string{"Dermaplaning removes peach fuzz"}}

	service := NewGPTService(mockOpenAI, client, rag, "gpt-5-mini", logging.Default())
	_, err := service.StartConversation(context.Background(), StartRequest{
		LeadID:   "lead-99",
		Intro:    "dermaplaning",
		Source:   "web",
		Channel:  ChannelSMS,
		OrgID:    "org-123",
		ClinicID: "clinic-77",
	})
	if err != nil {
		t.Fatalf("StartConversation returned error: %v", err)
	}

	if rag.lastClinic != "clinic-77" || rag.lastQuery != "dermaplaning" {
		t.Fatalf("rag queried with wrong parameters: %#v", rag)
	}

	foundContext := false
	for _, msg := range mockOpenAI.lastReq.Messages {
		if strings.Contains(msg.Content, "Dermaplaning removes peach fuzz") {
			foundContext = true
			break
		}
	}
	if !foundContext {
		t.Fatal("expected RAG context to be injected into chat history")
	}
}
