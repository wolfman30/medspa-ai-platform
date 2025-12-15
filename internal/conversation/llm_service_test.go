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
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestLLMService_StartConversation_PersistsHistory(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	mockLLM := &stubLLMClient{
		response: LLMResponse{Text: "Hi there!"},
	}

	service := NewLLMService(mockLLM, client, nil, "anthropic.claude-3-haiku-20240307-v1:0", logging.Default())
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
	var history []ChatMessage
	if err := json.Unmarshal([]byte(raw), &history); err != nil {
		t.Fatalf("failed to decode stored history: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("expected 3 messages in history, got %d", len(history))
	}
	if history[2].Role != ChatRoleAssistant || history[2].Content != "Hi there!" {
		t.Fatalf("expected assistant reply stored, got %#v", history[2])
	}
}

func TestLLMService_ProcessMessage_LoadsExistingHistory(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	mockLLM := &stubLLMClient{
		response: LLMResponse{Text: "Sure, Friday works."},
	}

	service := NewLLMService(mockLLM, client, nil, "anthropic.claude-3-haiku-20240307-v1:0", logging.Default())
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

	var history []ChatMessage
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

func TestLLMService_ProcessMessage_UnknownConversationBootstraps(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	mockLLM := &stubLLMClient{
		response: LLMResponse{Text: "Welcome back!"},
	}
	service := NewLLMService(mockLLM, client, nil, "anthropic.claude-3-haiku-20240307-v1:0", logging.Default())

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
	var history []ChatMessage
	if err := json.Unmarshal([]byte(raw), &history); err != nil {
		t.Fatalf("failed to decode history: %v", err)
	}
	if got := history[len(history)-1].Content; got != "Welcome back!" {
		t.Fatalf("expected assistant reply to be stored, got %s", got)
	}
}

func TestLLMService_StartConversation_LLMError(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	mock := &stubLLMClient{err: errors.New("quota exceeded")}
	service := NewLLMService(mock, client, nil, "anthropic.claude-3-haiku-20240307-v1:0", logging.Default())

	_, err := service.StartConversation(context.Background(), StartRequest{LeadID: "lead"})
	if err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("expected propagated LLM error, got %v", err)
	}
}

func TestLLMService_ProcessMessage_ExtractsDepositIntent(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	scripted := &stubLLMClient{
		responses: []LLMResponse{
			{Text: "Hello!"},
			{Text: "Let's lock this in. I can send a quick deposit link."},
			{Text: `{"collect":true,"amount_cents":7500,"success_url":"http://ok","cancel_url":"http://cancel","description":"Hold your spot"}`},
		},
	}

	service := NewLLMService(scripted, client, nil, "anthropic.claude-3-haiku-20240307-v1:0", logging.Default(), WithDepositConfig(DepositConfig{
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

func TestLLMService_ProcessMessage_FallbacksToHeuristicDepositIntent(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	scripted := &stubLLMClient{
		responses: []LLMResponse{
			{Text: "Hello!"},
			{Text: "Great, we do require a small deposit to hold your spot. Would you like to proceed?"},
			{Text: `{"collect":false,"amount_cents":0,"success_url":"","cancel_url":"","description":""}`},
		},
	}

	service := NewLLMService(scripted, client, nil, "anthropic.claude-3-haiku-20240307-v1:0", logging.Default(), WithDepositConfig(DepositConfig{
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

func TestLLMService_ProcessMessage_FallbacksOnGenericYesAfterDepositAsk(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	scripted := &stubLLMClient{
		responses: []LLMResponse{
			// StartConversation assistant reply
			{Text: "Hello!"},
			// First user turn assistant reply (asks for deposit)
			{Text: "We do require a small refundable deposit to hold your spot. Would you like to proceed?"},
			// Classifier for first user turn (skip)
			{Text: `{"collect":false,"amount_cents":0,"success_url":"","cancel_url":"","description":""}`},
			// Second user turn assistant reply (any content)
			{Text: "Great!"},
			// Classifier for second user turn (skip, forcing heuristic)
			{Text: `{"collect":false,"amount_cents":0,"success_url":"","cancel_url":"","description":""}`},
		},
	}

	service := NewLLMService(scripted, client, nil, "anthropic.claude-3-haiku-20240307-v1:0", logging.Default(), WithDepositConfig(DepositConfig{
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

func TestLLMService_UsesRAGContext(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	mockLLM := &stubLLMClient{
		response: LLMResponse{Text: "Response"},
	}
	rag := &stubRAG{contexts: []string{"Dermaplaning removes peach fuzz"}}

	service := NewLLMService(mockLLM, client, rag, "anthropic.claude-3-haiku-20240307-v1:0", logging.Default())
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
	for _, sys := range mockLLM.lastReq.System {
		if strings.Contains(sys, "Dermaplaning removes peach fuzz") {
			foundContext = true
			break
		}
	}
	if !foundContext {
		t.Fatal("expected RAG context to be injected into system prompt")
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

func TestLLMService_AppendsPaidDepositContext_DoesNotPromptForConfirmationRepeat(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	mockLLM := &stubLLMClient{
		response: LLMResponse{Text: "Ok!"},
	}
	checker := &stubOpenDepositStatusChecker{status: "succeeded"}

	service := NewLLMService(mockLLM, client, nil, "anthropic.claude-3-haiku-20240307-v1:0", logging.Default(), WithPaymentChecker(checker))
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
	for _, sys := range mockLLM.lastReq.System {
		if strings.Contains(sys, "ALREADY PAID their deposit") {
			found = true
			if strings.Contains(strings.ToLower(sys), "acknowledge") {
				t.Fatalf("expected paid-deposit context to avoid prompting for an acknowledgment, got %q", sys)
			}
			if !strings.Contains(sys, "already sent a payment confirmation SMS") {
				t.Fatalf("expected paid-deposit context to mention confirmation already sent, got %q", sys)
			}
		}
	}
	if !found {
		t.Fatalf("expected paid-deposit context to be injected")
	}
}

func TestLLMService_AppendsPendingDepositContext_DoesNotClaimPaid(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	mockLLM := &stubLLMClient{
		response: LLMResponse{Text: "Ok!"},
	}
	checker := &stubOpenDepositStatusChecker{status: "deposit_pending"}

	service := NewLLMService(mockLLM, client, nil, "anthropic.claude-3-haiku-20240307-v1:0", logging.Default(), WithPaymentChecker(checker))
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
	for _, sys := range mockLLM.lastReq.System {
		if strings.Contains(sys, "deposit payment link") && strings.Contains(sys, "still pending") {
			found = true
			if strings.Contains(sys, "ALREADY PAID") {
				t.Fatalf("expected pending-deposit context to avoid claiming the deposit is paid, got %q", sys)
			}
		}
	}
	if !found {
		t.Fatalf("expected pending-deposit context to be injected")
	}
}

type stubLLMClient struct {
	response  LLMResponse
	err       error
	lastReq   LLMRequest
	requests  []LLMRequest
	responses []LLMResponse
	errs      []error
	calls     int
}

func (s *stubLLMClient) Complete(ctx context.Context, req LLMRequest) (LLMResponse, error) {
	s.lastReq = req
	s.requests = append(s.requests, req)

	if s.calls < len(s.errs) && s.errs[s.calls] != nil {
		err := s.errs[s.calls]
		s.calls++
		return LLMResponse{}, err
	}
	if len(s.responses) > 0 {
		if s.calls >= len(s.responses) {
			s.calls++
			return LLMResponse{}, errors.New("no scripted response")
		}
		resp := s.responses[s.calls]
		s.calls++
		return resp, nil
	}
	if s.err != nil {
		return LLMResponse{}, s.err
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
