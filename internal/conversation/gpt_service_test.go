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

func TestGPTService_ProcessMessage_UnknownConversationBootstraps(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	mockOpenAI := &stubChatClient{
		response: openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{
				{Message: openai.ChatCompletionMessage{Content: "Welcome back!"}},
			},
		},
	}
	service := NewGPTService(mockOpenAI, client, nil, "gpt-5-mini", logging.Default())

	resp, err := service.ProcessMessage(context.Background(), MessageRequest{
		ConversationID: "conv_missing",
		Message:        "hello",
		Channel:        ChannelSMS,
		OrgID:          "org-1",
	})
	if err != nil {
		t.Fatalf("expected new conversation to start, got error %v", err)
	}
	if resp == nil || resp.Message != "Welcome back!" {
		t.Fatalf("unexpected response: %#v", resp)
	}

	raw, err := mr.DB(0).Get(conversationKey("conv_missing"))
	if err != nil {
		t.Fatalf("expected conversation history to persist: %v", err)
	}
	var history []openai.ChatCompletionMessage
	if err := json.Unmarshal([]byte(raw), &history); err != nil {
		t.Fatalf("failed to decode history: %v", err)
	}
	if got := history[len(history)-1].Content; got != "Welcome back!" {
		t.Fatalf("expected assistant reply to be stored, got %s", got)
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

func TestGPTService_ProcessMessage_ExtractsDepositIntent(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	scripted := &scriptedChatClient{
		responses: []openai.ChatCompletionResponse{
			{Choices: []openai.ChatCompletionChoice{{Message: openai.ChatCompletionMessage{Content: "Hello!"}}}},
			{Choices: []openai.ChatCompletionChoice{{Message: openai.ChatCompletionMessage{Content: "Let's lock this in. I can send a quick deposit link."}}}},
			{Choices: []openai.ChatCompletionChoice{{Message: openai.ChatCompletionMessage{Content: `{"collect":true,"amount_cents":7500,"success_url":"http://ok","cancel_url":"http://cancel","description":"Hold your spot"}`}}}},
		},
	}

	service := NewGPTService(scripted, client, nil, "gpt-5-mini", logging.Default(), WithDepositConfig(DepositConfig{
		DefaultAmountCents: 5000,
		SuccessURL:         "http://default-success",
		CancelURL:          "http://default-cancel",
	}))

	start, err := service.StartConversation(context.Background(), StartRequest{
		ConversationID: "conv-deposit",
		LeadID:         "lead-1",
		Intro:          "Hi",
		Channel:        ChannelSMS,
		OrgID:          "org-1",
	})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}

	resp, err := service.ProcessMessage(context.Background(), MessageRequest{
		ConversationID: start.ConversationID,
		Message:        "Happy to pay a deposit for Friday",
		Channel:        ChannelSMS,
		OrgID:          "org-1",
	})
	if err != nil {
		t.Fatalf("process message failed: %v", err)
	}

	if resp.DepositIntent == nil {
		t.Fatalf("expected deposit intent to be set")
	}
	if resp.DepositIntent.AmountCents != 7500 {
		t.Fatalf("unexpected deposit amount: %d", resp.DepositIntent.AmountCents)
	}
	if resp.DepositIntent.SuccessURL != "http://ok" || resp.DepositIntent.CancelURL != "http://cancel" {
		t.Fatalf("unexpected deposit URLs: %#v", resp.DepositIntent)
	}
	if resp.DepositIntent.Description != "Hold your spot" {
		t.Fatalf("unexpected deposit description: %s", resp.DepositIntent.Description)
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

type scriptedChatClient struct {
	responses []openai.ChatCompletionResponse
	errs      []error
	calls     int
	lastReq   openai.ChatCompletionRequest
}

func (s *scriptedChatClient) CreateChatCompletion(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
	s.lastReq = req
	if s.calls < len(s.errs) && s.errs[s.calls] != nil {
		err := s.errs[s.calls]
		s.calls++
		return openai.ChatCompletionResponse{}, err
	}
	if s.calls >= len(s.responses) {
		s.calls++
		return openai.ChatCompletionResponse{}, errors.New("no scripted response")
	}
	resp := s.responses[s.calls]
	s.calls++
	return resp, nil
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
