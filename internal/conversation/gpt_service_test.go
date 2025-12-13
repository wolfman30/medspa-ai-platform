package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
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

func TestGPTService_ProcessMessage_FallbacksToHeuristicDepositIntent(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	scripted := &scriptedChatClient{
		responses: []openai.ChatCompletionResponse{
			{Choices: []openai.ChatCompletionChoice{{Message: openai.ChatCompletionMessage{Content: "Hello!"}}}},
			{Choices: []openai.ChatCompletionChoice{{Message: openai.ChatCompletionMessage{Content: "Great, we do require a small deposit to hold your spot. Would you like to proceed?"}}}},
			{Choices: []openai.ChatCompletionChoice{{Message: openai.ChatCompletionMessage{Content: `{"collect":false,"amount_cents":0,"success_url":"","cancel_url":"","description":""}`}}}},
		},
	}

	service := NewGPTService(scripted, client, nil, "gpt-5-mini", logging.Default(), WithDepositConfig(DepositConfig{
		DefaultAmountCents: 5000,
		SuccessURL:         "http://default-success",
		CancelURL:          "http://default-cancel",
		Description:        "Appointment deposit",
	}))

	start, err := service.StartConversation(context.Background(), StartRequest{
		ConversationID: "conv-deposit-fallback",
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
		Message:        "Yes, I'd like to secure my spot with a deposit",
		Channel:        ChannelSMS,
		OrgID:          "org-1",
	})
	if err != nil {
		t.Fatalf("process message failed: %v", err)
	}

	if resp.DepositIntent == nil {
		t.Fatalf("expected fallback deposit intent to be set")
	}
	if resp.DepositIntent.AmountCents != 5000 {
		t.Fatalf("unexpected fallback amount: %d", resp.DepositIntent.AmountCents)
	}
}

func TestGPTService_ProcessMessage_FallbacksOnGenericYesAfterDepositAsk(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	scripted := &scriptedChatClient{
		responses: []openai.ChatCompletionResponse{
			// StartConversation assistant reply
			{Choices: []openai.ChatCompletionChoice{{Message: openai.ChatCompletionMessage{Content: "Hello!"}}}},
			// First user turn assistant reply (asks for deposit)
			{Choices: []openai.ChatCompletionChoice{{Message: openai.ChatCompletionMessage{Content: "We do require a small refundable deposit to hold your spot. Would you like to proceed?"}}}},
			// Classifier for first user turn (skip)
			{Choices: []openai.ChatCompletionChoice{{Message: openai.ChatCompletionMessage{Content: `{"collect":false,"amount_cents":0,"success_url":"","cancel_url":"","description":""}`}}}},
			// Second user turn assistant reply (any content)
			{Choices: []openai.ChatCompletionChoice{{Message: openai.ChatCompletionMessage{Content: "Great!"}}}},
			// Classifier for second user turn (skip, forcing heuristic)
			{Choices: []openai.ChatCompletionChoice{{Message: openai.ChatCompletionMessage{Content: `{"collect":false,"amount_cents":0,"success_url":"","cancel_url":"","description":""}`}}}},
		},
	}

	service := NewGPTService(scripted, client, nil, "gpt-5-mini", logging.Default(), WithDepositConfig(DepositConfig{
		DefaultAmountCents: 5000,
		SuccessURL:         "http://default-success",
		CancelURL:          "http://default-cancel",
		Description:        "Appointment deposit",
	}))

	start, err := service.StartConversation(context.Background(), StartRequest{
		ConversationID: "conv-deposit-generic-yes",
		LeadID:         "lead-1",
		Intro:          "Hi",
		Channel:        ChannelSMS,
		OrgID:          "org-1",
	})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// First user message that triggers a deposit ask, but should not infer intent yet.
	if _, err := service.ProcessMessage(context.Background(), MessageRequest{
		ConversationID: start.ConversationID,
		Message:        "I want to book Botox",
		Channel:        ChannelSMS,
		OrgID:          "org-1",
	}); err != nil {
		t.Fatalf("process first message failed: %v", err)
	}

	// Generic affirmative after deposit ask should infer intent.
	resp, err := service.ProcessMessage(context.Background(), MessageRequest{
		ConversationID: start.ConversationID,
		Message:        "Yes",
		Channel:        ChannelSMS,
		OrgID:          "org-1",
	})
	if err != nil {
		t.Fatalf("process second message failed: %v", err)
	}
	if resp.DepositIntent == nil {
		t.Fatalf("expected deposit intent to be inferred on generic yes")
	}
	if resp.DepositIntent.AmountCents != 5000 {
		t.Fatalf("unexpected inferred amount: %d", resp.DepositIntent.AmountCents)
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

type stubOpenDepositStatusChecker struct {
	status string
}

func (s *stubOpenDepositStatusChecker) HasOpenDeposit(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID) (bool, error) {
	return strings.TrimSpace(s.status) != "", nil
}

func (s *stubOpenDepositStatusChecker) OpenDepositStatus(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID) (string, error) {
	return s.status, nil
}

func TestGPTService_AppendsPaidDepositContext_DoesNotPromptForConfirmationRepeat(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	mockOpenAI := &stubChatClient{
		response: openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{
				{Message: openai.ChatCompletionMessage{Content: "Ok!"}},
			},
		},
	}
	checker := &stubOpenDepositStatusChecker{status: "succeeded"}

	service := NewGPTService(mockOpenAI, client, nil, "gpt-5-mini", logging.Default(), WithPaymentChecker(checker))
	orgID := uuid.New()
	leadID := uuid.New()
	if _, err := service.StartConversation(context.Background(), StartRequest{
		ConversationID: "conv-paid",
		LeadID:         leadID.String(),
		OrgID:          orgID.String(),
		Intro:          "hi",
		Channel:        ChannelSMS,
	}); err != nil {
		t.Fatalf("StartConversation returned error: %v", err)
	}

	found := false
	for _, msg := range mockOpenAI.lastReq.Messages {
		if msg.Role != openai.ChatMessageRoleSystem {
			continue
		}
		if strings.Contains(msg.Content, "ALREADY PAID their deposit") {
			found = true
			if strings.Contains(strings.ToLower(msg.Content), "acknowledge") {
				t.Fatalf("expected paid-deposit context to avoid prompting for an acknowledgment, got %q", msg.Content)
			}
			if !strings.Contains(msg.Content, "already sent a payment confirmation SMS") {
				t.Fatalf("expected paid-deposit context to mention confirmation already sent, got %q", msg.Content)
			}
		}
	}
	if !found {
		t.Fatalf("expected paid-deposit context to be injected")
	}
}

func TestGPTService_AppendsPendingDepositContext_DoesNotClaimPaid(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	mockOpenAI := &stubChatClient{
		response: openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{
				{Message: openai.ChatCompletionMessage{Content: "Ok!"}},
			},
		},
	}
	checker := &stubOpenDepositStatusChecker{status: "deposit_pending"}

	service := NewGPTService(mockOpenAI, client, nil, "gpt-5-mini", logging.Default(), WithPaymentChecker(checker))
	orgID := uuid.New()
	leadID := uuid.New()
	if _, err := service.StartConversation(context.Background(), StartRequest{
		ConversationID: "conv-pending",
		LeadID:         leadID.String(),
		OrgID:          orgID.String(),
		Intro:          "hi",
		Channel:        ChannelSMS,
	}); err != nil {
		t.Fatalf("StartConversation returned error: %v", err)
	}

	found := false
	for _, msg := range mockOpenAI.lastReq.Messages {
		if msg.Role != openai.ChatMessageRoleSystem {
			continue
		}
		if strings.Contains(msg.Content, "deposit payment link") && strings.Contains(msg.Content, "still pending") {
			found = true
			if strings.Contains(msg.Content, "ALREADY PAID") {
				t.Fatalf("expected pending-deposit context to avoid claiming the deposit is paid, got %q", msg.Content)
			}
		}
	}
	if !found {
		t.Fatalf("expected pending-deposit context to be injected")
	}
}
