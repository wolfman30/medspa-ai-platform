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
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestTierA_CI03_ExistingLeadResumes_NoDuplicateWelcome(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	scripted := &stubLLMClient{
		responses: []LLMResponse{
			{Text: "Welcome!"},
			{Text: "Follow-up reply"},
		},
	}
	service := NewLLMService(scripted, client, nil, "anthropic.claude-3-haiku-20240307-v1:0", logging.Default())

	start, err := service.StartConversation(context.Background(), StartRequest{
		ConversationID: "conv-resume",
		LeadID:         "lead-1",
		OrgID:          "org-1",
		Intro:          "Hi",
		Channel:        ChannelSMS,
	})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	resp, err := service.ProcessMessage(context.Background(), MessageRequest{
		ConversationID: start.ConversationID,
		Message:        "hello again",
		Channel:        ChannelSMS,
		OrgID:          "org-1",
	})
	if err != nil {
		t.Fatalf("process failed: %v", err)
	}
	if resp.Message != "Follow-up reply" {
		t.Fatalf("expected follow-up reply, got %q", resp.Message)
	}

	raw, err := mr.DB(0).Get(conversationKey(start.ConversationID))
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	var history []ChatMessage
	if err := json.Unmarshal([]byte(raw), &history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	welcomeCount := 0
	for _, msg := range history {
		if msg.Role == ChatRoleAssistant && msg.Content == "Welcome!" {
			welcomeCount++
		}
	}
	if welcomeCount != 1 {
		t.Fatalf("expected welcome message once, got %d", welcomeCount)
	}
}

func TestTierA_CI04_PriceServiceInquiry_UsesClinicConfigAndTagsLead(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	clinicStore := clinic.NewStore(client)

	cfg := clinic.DefaultConfig("org-1")
	cfg.ServicePriceText = map[string]string{"botox": "$12/unit"}
	cfg.ServiceDepositAmountCents = map[string]int{"botox": 5000}
	if err := clinicStore.Set(ctx, cfg); err != nil {
		t.Fatalf("set clinic config: %v", err)
	}

	leadsRepo := leads.NewInMemoryRepository()
	lead, err := leadsRepo.Create(ctx, &leads.CreateLeadRequest{
		OrgID:   "org-1",
		Name:    "Test Lead",
		Phone:   "+15550000000",
		Source:  "sms",
		Message: "",
	})
	if err != nil {
		t.Fatalf("create lead: %v", err)
	}

	mockLLM := &stubLLMClient{responses: []LLMResponse{{Text: "Hello!"}}}
	service := NewLLMService(mockLLM, client, nil, "anthropic.claude-3-haiku-20240307-v1:0", logging.Default(), WithClinicStore(clinicStore), WithLeadsRepo(leadsRepo))

	start, err := service.StartConversation(ctx, StartRequest{
		ConversationID: "conv-price",
		LeadID:         lead.ID,
		OrgID:          "org-1",
		Intro:          "Hi",
		Channel:        ChannelSMS,
	})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}

	resp, err := service.ProcessMessage(ctx, MessageRequest{
		ConversationID: start.ConversationID,
		LeadID:         lead.ID,
		OrgID:          "org-1",
		Message:        "How much is Botox?",
		Channel:        ChannelSMS,
	})
	if err != nil {
		t.Fatalf("process failed: %v", err)
	}
	if !strings.Contains(resp.Message, "Botox pricing: $12/unit") {
		t.Fatalf("expected price response, got %q", resp.Message)
	}
	if !strings.Contains(resp.Message, "refundable deposit") || !strings.Contains(resp.Message, "$50") {
		t.Fatalf("expected deposit policy in reply, got %q", resp.Message)
	}
	if len(mockLLM.requests) != 1 {
		t.Fatalf("expected no extra LLM calls for price inquiry, got %d", len(mockLLM.requests))
	}
	updated, err := leadsRepo.GetByID(ctx, "org-1", lead.ID)
	if err != nil {
		t.Fatalf("load lead: %v", err)
	}
	if !strings.Contains(updated.SchedulingNotes, "tag:price_shopper") {
		t.Fatalf("expected lead tagged as price_shopper, got %q", updated.SchedulingNotes)
	}
}

func TestTierA_CI05_AmbiguousMessage_AsksClarifyingQuestionAndTagsLead(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	leadsRepo := leads.NewInMemoryRepository()
	lead, err := leadsRepo.Create(ctx, &leads.CreateLeadRequest{
		OrgID:   "org-1",
		Name:    "Test Lead",
		Phone:   "+15550000001",
		Source:  "sms",
		Message: "",
	})
	if err != nil {
		t.Fatalf("create lead: %v", err)
	}

	mockLLM := &stubLLMClient{responses: []LLMResponse{{Text: "Hello!"}}}
	service := NewLLMService(mockLLM, client, nil, "anthropic.claude-3-haiku-20240307-v1:0", logging.Default(), WithLeadsRepo(leadsRepo))

	start, err := service.StartConversation(ctx, StartRequest{
		ConversationID: "conv-help",
		LeadID:         lead.ID,
		OrgID:          "org-1",
		Intro:          "Hi",
		Channel:        ChannelSMS,
	})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}

	resp, err := service.ProcessMessage(ctx, MessageRequest{
		ConversationID: start.ConversationID,
		LeadID:         lead.ID,
		OrgID:          "org-1",
		Message:        "I need help",
		Channel:        ChannelSMS,
	})
	if err != nil {
		t.Fatalf("process failed: %v", err)
	}
	if !strings.Contains(strings.ToLower(resp.Message), "are you looking to book") {
		t.Fatalf("expected clarifying question, got %q", resp.Message)
	}
	if len(mockLLM.requests) != 1 {
		t.Fatalf("expected no extra LLM calls for ambiguous help, got %d", len(mockLLM.requests))
	}
	updated, err := leadsRepo.GetByID(ctx, "org-1", lead.ID)
	if err != nil {
		t.Fatalf("load lead: %v", err)
	}
	if !strings.Contains(updated.SchedulingNotes, "state:needs_intent") {
		t.Fatalf("expected lead tagged needs_intent, got %q", updated.SchedulingNotes)
	}
}

func TestTierA_CI12_DepositRulesCorrectness_ServiceOverridesApply(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	clinicStore := clinic.NewStore(client)

	cfg := clinic.DefaultConfig("org-1")
	cfg.ServiceDepositAmountCents = map[string]int{
		"botox":  5000,
		"filler": 10000,
	}
	if err := clinicStore.Set(ctx, cfg); err != nil {
		t.Fatalf("set clinic config: %v", err)
	}

	leadsRepo := leads.NewInMemoryRepository()
	lead, err := leadsRepo.Create(ctx, &leads.CreateLeadRequest{
		OrgID:   "org-1",
		Name:    "Test Lead",
		Phone:   "+15550000002",
		Source:  "sms",
		Message: "",
	})
	if err != nil {
		t.Fatalf("create lead: %v", err)
	}

	scripted := &stubLLMClient{
		responses: []LLMResponse{
			{Text: "Hello!"}, // start #1
			{Text: "We do require a refundable deposit to hold your spot."},                                                                      // reply #1
			{Text: `{"collect":true,"amount_cents":7500,"success_url":"http://ok","cancel_url":"http://cancel","description":"Hold your spot"}`}, // classifier #1
			{Text: "Hello!"}, // start #2
			{Text: "We do require a refundable deposit to hold your spot."},                                                                      // reply #2
			{Text: `{"collect":true,"amount_cents":7500,"success_url":"http://ok","cancel_url":"http://cancel","description":"Hold your spot"}`}, // classifier #2
		},
	}

	service := NewLLMService(scripted, client, nil, "anthropic.claude-3-haiku-20240307-v1:0", logging.Default(), WithClinicStore(clinicStore), WithLeadsRepo(leadsRepo), WithDepositConfig(DepositConfig{
		DefaultAmountCents: 5000,
		SuccessURL:         "http://default-success",
		CancelURL:          "http://default-cancel",
	}))

	start1, err := service.StartConversation(ctx, StartRequest{
		ConversationID: "conv-botox",
		LeadID:         lead.ID,
		OrgID:          "org-1",
		Intro:          "Hi",
		Channel:        ChannelSMS,
	})
	if err != nil {
		t.Fatalf("start #1 failed: %v", err)
	}
	resp1, err := service.ProcessMessage(ctx, MessageRequest{
		ConversationID: start1.ConversationID,
		LeadID:         lead.ID,
		OrgID:          "org-1",
		Message:        "I want to book botox",
		Channel:        ChannelSMS,
	})
	if err != nil {
		t.Fatalf("process #1 failed: %v", err)
	}
	if resp1.DepositIntent == nil || resp1.DepositIntent.AmountCents != 5000 {
		t.Fatalf("expected botox deposit override to 5000, got %#v", resp1.DepositIntent)
	}

	start2, err := service.StartConversation(ctx, StartRequest{
		ConversationID: "conv-filler",
		LeadID:         lead.ID,
		OrgID:          "org-1",
		Intro:          "Hi",
		Channel:        ChannelSMS,
	})
	if err != nil {
		t.Fatalf("start #2 failed: %v", err)
	}
	resp2, err := service.ProcessMessage(ctx, MessageRequest{
		ConversationID: start2.ConversationID,
		LeadID:         lead.ID,
		OrgID:          "org-1",
		Message:        "I want to book filler",
		Channel:        ChannelSMS,
	})
	if err != nil {
		t.Fatalf("process #2 failed: %v", err)
	}
	if resp2.DepositIntent == nil || resp2.DepositIntent.AmountCents != 10000 {
		t.Fatalf("expected filler deposit override to 10000, got %#v", resp2.DepositIntent)
	}
}

func TestTierA_CI13_HIPAA_PHIDeflection_NoLeadDiagnosisUpdates(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	leadsRepo := leads.NewInMemoryRepository()
	lead, err := leadsRepo.Create(ctx, &leads.CreateLeadRequest{
		OrgID:   "org-1",
		Name:    "Test Lead",
		Phone:   "+15550000003",
		Source:  "sms",
		Message: "",
	})
	if err != nil {
		t.Fatalf("create lead: %v", err)
	}

	mockLLM := &stubLLMClient{responses: []LLMResponse{{Text: "Hello!"}}}
	service := NewLLMService(mockLLM, client, nil, "anthropic.claude-3-haiku-20240307-v1:0", logging.Default(), WithLeadsRepo(leadsRepo))

	start, err := service.StartConversation(ctx, StartRequest{
		ConversationID: "conv-phi",
		LeadID:         lead.ID,
		OrgID:          "org-1",
		Intro:          "Hi",
		Channel:        ChannelSMS,
	})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}

	resp, err := service.ProcessMessage(ctx, MessageRequest{
		ConversationID: start.ConversationID,
		LeadID:         lead.ID,
		OrgID:          "org-1",
		Message:        "I have diabetes and I'm worried about botox side effects",
		Channel:        ChannelSMS,
	})
	if err != nil {
		t.Fatalf("process failed: %v", err)
	}
	if !strings.Contains(strings.ToLower(resp.Message), "can't provide medical advice") {
		t.Fatalf("expected PHI deflection reply, got %q", resp.Message)
	}
	if len(mockLLM.requests) != 1 {
		t.Fatalf("expected no extra LLM calls for PHI deflection, got %d", len(mockLLM.requests))
	}
	updated, err := leadsRepo.GetByID(ctx, "org-1", lead.ID)
	if err != nil {
		t.Fatalf("load lead: %v", err)
	}
	if updated.ServiceInterest != "" || updated.PatientType != "" {
		t.Fatalf("expected lead profile not updated with PHI, got %#v", updated)
	}
	raw, err := mr.DB(0).Get(conversationKey(start.ConversationID))
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	var history []ChatMessage
	if err := json.Unmarshal([]byte(raw), &history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	foundRedacted := false
	for _, msg := range history {
		if msg.Role == ChatRoleUser && msg.Content == "[REDACTED]" {
			foundRedacted = true
		}
		if strings.Contains(strings.ToLower(msg.Content), "diabetes") {
			t.Fatalf("expected PHI to be redacted from history, got %q", msg.Content)
		}
	}
	if !foundRedacted {
		t.Fatalf("expected redacted PHI entry in history")
	}
}

func TestLLMService_ProcessMessage_MedicalAdviceDeflection(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	mockLLM := &stubLLMClient{responses: []LLMResponse{{Text: "Hello!"}}}
	service := NewLLMService(mockLLM, client, nil, "anthropic.claude-3-haiku-20240307-v1:0", logging.Default())

	start, err := service.StartConversation(ctx, StartRequest{
		ConversationID: "conv-medical-advice",
		LeadID:         "lead-1",
		OrgID:          "org-1",
		Intro:          "Hi",
		Channel:        ChannelSMS,
	})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}

	resp, err := service.ProcessMessage(ctx, MessageRequest{
		ConversationID: start.ConversationID,
		LeadID:         "lead-1",
		OrgID:          "org-1",
		Message:        "Is it safe for me to take ibuprofen before Botox?",
		Channel:        ChannelSMS,
	})
	if err != nil {
		t.Fatalf("process failed: %v", err)
	}
	if !strings.Contains(strings.ToLower(resp.Message), "can't provide medical advice") {
		t.Fatalf("expected medical advice deflection reply, got %q", resp.Message)
	}
	if len(mockLLM.requests) != 1 {
		t.Fatalf("expected no extra LLM calls for medical advice deflection, got %d", len(mockLLM.requests))
	}

	raw, err := mr.DB(0).Get(conversationKey(start.ConversationID))
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	var history []ChatMessage
	if err := json.Unmarshal([]byte(raw), &history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	foundRedacted := false
	for _, msg := range history {
		if msg.Role == ChatRoleUser && msg.Content == "[REDACTED]" {
			foundRedacted = true
		}
		if strings.Contains(strings.ToLower(msg.Content), "ibuprofen") {
			t.Fatalf("expected medical advice content to be redacted from history, got %q", msg.Content)
		}
	}
	if !foundRedacted {
		t.Fatalf("expected redacted medical advice entry in history")
	}
}

func TestLLMService_StartConversation_RedactsPHI(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	mockLLM := &stubLLMClient{response: LLMResponse{Text: "Hello"}}
	service := NewLLMService(mockLLM, client, nil, "anthropic.claude-3-haiku-20240307-v1:0", logging.Default())

	resp, err := service.StartConversation(ctx, StartRequest{
		ConversationID: "conv-phi-start",
		LeadID:         "lead-1",
		OrgID:          "org-1",
		Intro:          "I have diabetes and need advice",
		Channel:        ChannelSMS,
	})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if !strings.Contains(strings.ToLower(resp.Message), "can't provide medical advice") {
		t.Fatalf("expected PHI deflection reply, got %q", resp.Message)
	}
	if len(mockLLM.requests) != 0 {
		t.Fatalf("expected no LLM calls for PHI intro, got %d", len(mockLLM.requests))
	}

	raw, err := mr.DB(0).Get(conversationKey(resp.ConversationID))
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	var history []ChatMessage
	if err := json.Unmarshal([]byte(raw), &history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if !strings.Contains(raw, "[REDACTED]") {
		t.Fatalf("expected redacted PHI in history")
	}
	for _, msg := range history {
		if strings.Contains(strings.ToLower(msg.Content), "diabetes") {
			t.Fatalf("expected PHI to be redacted from history, got %q", msg.Content)
		}
	}
}

func TestLLMService_StartConversation_MedicalAdviceDeflection(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	mockLLM := &stubLLMClient{response: LLMResponse{Text: "Hello"}}
	service := NewLLMService(mockLLM, client, nil, "anthropic.claude-3-haiku-20240307-v1:0", logging.Default())

	resp, err := service.StartConversation(ctx, StartRequest{
		ConversationID: "conv-medical-advice-start",
		LeadID:         "lead-1",
		OrgID:          "org-1",
		Intro:          "Is it safe for me to take ibuprofen before Botox?",
		Channel:        ChannelSMS,
	})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if !strings.Contains(strings.ToLower(resp.Message), "can't provide medical advice") {
		t.Fatalf("expected medical advice deflection reply, got %q", resp.Message)
	}
	if len(mockLLM.requests) != 0 {
		t.Fatalf("expected no LLM calls for medical advice intro, got %d", len(mockLLM.requests))
	}

	raw, err := mr.DB(0).Get(conversationKey(resp.ConversationID))
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if !strings.Contains(raw, "[REDACTED]") {
		t.Fatalf("expected redacted medical advice in history")
	}
	var history []ChatMessage
	if err := json.Unmarshal([]byte(raw), &history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	for _, msg := range history {
		if strings.Contains(strings.ToLower(msg.Content), "ibuprofen") {
			t.Fatalf("expected medical advice content to be redacted from history, got %q", msg.Content)
		}
	}
}

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
