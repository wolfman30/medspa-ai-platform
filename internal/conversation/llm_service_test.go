package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

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
		Name:    "",
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
		Message:        "I'm Andrew Doe. How much is Botox?",
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
	if updated.Name != "Andrew Doe" {
		t.Fatalf("expected lead name updated, got %q", updated.Name)
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

func TestTierA_CI05b_QuickQuestionPromptsForDetails(t *testing.T) {
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
		ConversationID: "conv-question",
		LeadID:         lead.ID,
		OrgID:          "org-1",
		Intro:          "Hi",
		Channel:        ChannelSMS,
	})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}

	for _, msg := range []string{
		"Quick question.",
		"I had a quick question.",
		"I just had a quick question.",
	} {
		resp, err := service.ProcessMessage(ctx, MessageRequest{
			ConversationID: start.ConversationID,
			LeadID:         lead.ID,
			OrgID:          "org-1",
			Message:        msg,
			Channel:        ChannelSMS,
		})
		if err != nil {
			t.Fatalf("process failed: %v", err)
		}
		lowered := strings.ToLower(resp.Message)
		if strings.Contains(lowered, "are you looking to book") {
			t.Fatalf("expected no repeated intent question, got %q", resp.Message)
		}
		if !strings.Contains(lowered, "what can i help") {
			t.Fatalf("expected prompt for question details, got %q", resp.Message)
		}
	}
	if len(mockLLM.requests) != 1 {
		t.Fatalf("expected no extra LLM calls for quick question, got %d", len(mockLLM.requests))
	}
	updated, err := leadsRepo.GetByID(ctx, "org-1", lead.ID)
	if err != nil {
		t.Fatalf("load lead: %v", err)
	}
	if strings.Contains(updated.SchedulingNotes, "state:needs_intent") {
		t.Fatalf("did not expect needs_intent tag, got %q", updated.SchedulingNotes)
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

func TestLLMService_IncludesPerfectDermaHighlightWhenPeelAsked(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	clinicStore := clinic.NewStore(client)
	cfg := clinic.DefaultConfig("org-peel")
	cfg.Services = []string{"Perfect Derma Peel", "Botox"}
	if err := clinicStore.Set(ctx, cfg); err != nil {
		t.Fatalf("set clinic config: %v", err)
	}

	mockLLM := &stubLLMClient{
		response: LLMResponse{Text: "Response"},
	}
	service := NewLLMService(mockLLM, client, nil, "anthropic.claude-3-haiku-20240307-v1:0", logging.Default(), WithClinicStore(clinicStore))

	_, err := service.StartConversation(ctx, StartRequest{
		LeadID:   "lead-42",
		Intro:    "Do you offer chemical peels?",
		Source:   "sms",
		Channel:  ChannelSMS,
		OrgID:    "org-peel",
		ClinicID: "clinic-peel",
	})
	if err != nil {
		t.Fatalf("StartConversation returned error: %v", err)
	}

	foundHighlight := false
	for _, sys := range mockLLM.lastReq.System {
		if strings.Contains(sys, "Perfect Derma Peel") {
			foundHighlight = true
			break
		}
	}
	if !foundHighlight {
		t.Fatal("expected Perfect Derma Peel highlight to be injected into system prompt")
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

func TestBuildSystemPrompt_ReplacesDepositAmount(t *testing.T) {
	tests := []struct {
		name            string
		depositCents    int
		wantContains    string
		wantNotContains string
	}{
		{
			name:         "default $50",
			depositCents: 5000,
			wantContains: "$50",
		},
		{
			name:            "custom $75",
			depositCents:    7500,
			wantContains:    "$75",
			wantNotContains: "$50",
		},
		{
			name:            "custom $100",
			depositCents:    10000,
			wantContains:    "$100",
			wantNotContains: "$50",
		},
		{
			name:         "zero defaults to $50",
			depositCents: 0,
			wantContains: "$50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := buildSystemPrompt(tt.depositCents, false)
			if !strings.Contains(prompt, tt.wantContains) {
				t.Errorf("buildSystemPrompt(%d, false) should contain %q", tt.depositCents, tt.wantContains)
			}
			if tt.wantNotContains != "" && strings.Contains(prompt, tt.wantNotContains) {
				t.Errorf("buildSystemPrompt(%d, false) should NOT contain %q", tt.depositCents, tt.wantNotContains)
			}
		})
	}
}

func TestBuildSystemPrompt_IncludesDeliverabilityGuardrails(t *testing.T) {
	prompt := buildSystemPrompt(5000, false)
	if !strings.Contains(prompt, "DELIVERABILITY SAFETY") {
		t.Fatalf("expected deliverability guardrails in system prompt")
	}
	if !strings.Contains(prompt, "GLP-1") {
		t.Fatalf("expected GLP-1 guardrail in system prompt")
	}
}

func TestBuildSystemPrompt_MoxieInstructions(t *testing.T) {
	// Non-Moxie prompt should NOT include Moxie instructions
	promptNonMoxie := buildSystemPrompt(5000, false)
	if strings.Contains(promptNonMoxie, "MOXIE BOOKING CLINIC") {
		t.Errorf("non-Moxie prompt should NOT contain Moxie instructions")
	}

	// Moxie prompt SHOULD include Moxie instructions
	promptMoxie := buildSystemPrompt(5000, true)
	if !strings.Contains(promptMoxie, "MOXIE BOOKING CLINIC") {
		t.Errorf("Moxie prompt should contain 'MOXIE BOOKING CLINIC'")
	}
	// Email is now part of the qualification checklist
	if !strings.Contains(promptMoxie, "EMAIL") {
		t.Errorf("Moxie prompt should include EMAIL as a qualification item")
	}
	if !strings.Contains(promptMoxie, "SERVICE CLARIFICATION") {
		t.Errorf("Moxie prompt should contain service clarification instructions")
	}
	if !strings.Contains(promptMoxie, "TIME SELECTION BEFORE BOOKING LINK") {
		t.Errorf("Moxie prompt should explain time selection before booking link")
	}
	if !strings.Contains(promptMoxie, "PHONE VERIFICATION (CONDITIONAL)") {
		t.Errorf("Moxie prompt should include conditional phone verification instructions")
	}
}

func TestLLMService_InjectsDepositAmountContext(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	clinicStore := clinic.NewStore(client)

	// Set up clinic with custom deposit amount of $75
	cfg := clinic.DefaultConfig("org-deposit-test")
	cfg.DepositAmountCents = 7500
	if err := clinicStore.Set(ctx, cfg); err != nil {
		t.Fatalf("set clinic config: %v", err)
	}

	mockLLM := &stubLLMClient{response: LLMResponse{Text: "Hello!"}}
	service := NewLLMService(mockLLM, client, nil, "anthropic.claude-3-haiku-20240307-v1:0", logging.Default(), WithClinicStore(clinicStore))

	_, err := service.StartConversation(ctx, StartRequest{
		ConversationID: "conv-deposit-context",
		LeadID:         "lead-1",
		OrgID:          "org-deposit-test",
		Intro:          "Hi",
		Channel:        ChannelSMS,
	})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Check that the deposit amount context was injected
	foundDepositContext := false
	for _, sys := range mockLLM.lastReq.System {
		if strings.Contains(sys, "DEPOSIT AMOUNT") && strings.Contains(sys, "$75") {
			foundDepositContext = true
			// Verify it says to never give a range
			if !strings.Contains(sys, "NEVER say a range") {
				t.Errorf("deposit context should warn against ranges, got %q", sys)
			}
			break
		}
	}
	if !foundDepositContext {
		t.Fatalf("expected deposit amount context with $75 to be injected into system prompts")
	}
}

func TestSplitName(t *testing.T) {
	tests := []struct {
		full      string
		wantFirst string
		wantLast  string
	}{
		{"Andy Wolf", "Andy", "Wolf"},
		{"Madonna", "Madonna", ""},
		{"", "", ""},
		{"  ", "", ""},
		{"Mary Jane Watson", "Mary", "Jane Watson"},
		{"  Spaced  Name  ", "Spaced", "Name"},
	}
	for _, tt := range tests {
		t.Run(tt.full, func(t *testing.T) {
			first, last := splitName(tt.full)
			if first != tt.wantFirst || last != tt.wantLast {
				t.Errorf("splitName(%q) = (%q, %q), want (%q, %q)", tt.full, first, last, tt.wantFirst, tt.wantLast)
			}
		})
	}
}

func TestMoxieTimeSelection_BookingRequestPopulated(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	clinicStore := clinic.NewStore(rdb)

	// Set up Moxie clinic config
	cfg := clinic.DefaultConfig("org-moxie")
	cfg.BookingPlatform = "moxie"
	cfg.BookingURL = "https://app.joinmoxie.com/booking/forever-22"
	if err := clinicStore.Set(ctx, cfg); err != nil {
		t.Fatalf("save clinic config: %v", err)
	}

	// Create lead
	leadsRepo := leads.NewInMemoryRepository()
	lead, err := leadsRepo.Create(ctx, &leads.CreateLeadRequest{
		OrgID:   "org-moxie",
		Name:    "Andy Wolf",
		Phone:   "+15551234567",
		Email:   "andy@example.com",
		Source:  "sms",
		Message: "I want Botox",
	})
	if err != nil {
		t.Fatalf("create lead: %v", err)
	}

	// Build LLM service
	mockLLM := &stubLLMClient{
		responses: []LLMResponse{
			{Text: "Welcome!"},
			{Text: "Great choice! I've booked your Botox appointment."},
		},
	}
	svc := NewLLMService(mockLLM, rdb, nil, "test-model", logging.Default(),
		WithLeadsRepo(leadsRepo),
		WithClinicStore(clinicStore),
		WithAPIBaseURL("https://api.example.com"),
	)

	// Start conversation
	convID := "conv-moxie-" + uuid.NewString()
	_, err = svc.StartConversation(ctx, StartRequest{
		ConversationID: convID,
		LeadID:         lead.ID,
		OrgID:          "org-moxie",
		Intro:          "I want Botox",
		Channel:        ChannelSMS,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// Simulate: save time selection state as if slots were already presented
	slotTime := time.Date(2026, 2, 10, 15, 30, 0, 0, time.UTC)
	state := &TimeSelectionState{
		PresentedSlots: []PresentedSlot{
			{Index: 1, DateTime: slotTime, TimeStr: "Mon Feb 10 at 3:30 PM", Service: "Botox", Available: true},
			{Index: 2, DateTime: slotTime.Add(time.Hour), TimeStr: "Mon Feb 10 at 4:30 PM", Service: "Botox", Available: true},
		},
		Service:     "Botox",
		BookingURL:  "https://app.joinmoxie.com/booking/forever-22",
		PresentedAt: time.Now(),
	}
	if err := svc.history.SaveTimeSelectionState(ctx, convID, state); err != nil {
		t.Fatalf("save time selection state: %v", err)
	}

	// User selects slot 1
	resp, err := svc.ProcessMessage(ctx, MessageRequest{
		ConversationID: convID,
		Message:        "1",
		LeadID:         lead.ID,
		OrgID:          "org-moxie",
		Channel:        ChannelSMS,
		From:           "+15551234567",
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}

	// BookingRequest should be populated
	if resp.BookingRequest == nil {
		t.Fatal("expected BookingRequest to be populated for Moxie time selection")
	}
	br := resp.BookingRequest
	if br.BookingURL != "https://app.joinmoxie.com/booking/forever-22" {
		t.Errorf("BookingURL = %q, want Moxie URL", br.BookingURL)
	}
	if br.Date != "2026-02-10" {
		t.Errorf("Date = %q, want 2026-02-10", br.Date)
	}
	if br.Time != "3:30pm" {
		t.Errorf("Time = %q, want 3:30pm", br.Time)
	}
	if br.FirstName != "Andy" {
		t.Errorf("FirstName = %q, want Andy", br.FirstName)
	}
	if br.LastName != "Wolf" {
		t.Errorf("LastName = %q, want Wolf", br.LastName)
	}
	if br.Phone != "+15551234567" {
		t.Errorf("Phone = %q, want +15551234567", br.Phone)
	}
	if br.Email != "andy@example.com" {
		t.Errorf("Email = %q, want andy@example.com", br.Email)
	}
	if br.LeadID != lead.ID {
		t.Errorf("LeadID = %q, want %q", br.LeadID, lead.ID)
	}
	if br.OrgID != "org-moxie" {
		t.Errorf("OrgID = %q, want org-moxie", br.OrgID)
	}
	if br.Service != "Botox" {
		t.Errorf("Service = %q, want Botox", br.Service)
	}
	if !strings.Contains(br.CallbackURL, "/webhooks/booking/callback") {
		t.Errorf("CallbackURL = %q, want to contain /webhooks/booking/callback", br.CallbackURL)
	}
	if !strings.Contains(br.CallbackURL, "orgId=org-moxie") {
		t.Errorf("CallbackURL = %q, want to contain orgId=org-moxie", br.CallbackURL)
	}

	// Deposit intent should be nil for Moxie
	if resp.DepositIntent != nil {
		t.Error("expected DepositIntent to be nil for Moxie clinic")
	}
}

func TestSquareTimeSelection_BookingRequestNil(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	clinicStore := clinic.NewStore(rdb)

	// Set up Square clinic config (default)
	cfg := clinic.DefaultConfig("org-square")
	cfg.BookingPlatform = "square"
	if err := clinicStore.Set(ctx, cfg); err != nil {
		t.Fatalf("save clinic config: %v", err)
	}

	leadsRepo := leads.NewInMemoryRepository()
	lead, err := leadsRepo.Create(ctx, &leads.CreateLeadRequest{
		OrgID:   "org-square",
		Name:    "Jane Doe",
		Phone:   "+15559876543",
		Source:  "sms",
		Message: "Botox please",
	})
	if err != nil {
		t.Fatalf("create lead: %v", err)
	}

	mockLLM := &stubLLMClient{
		responses: []LLMResponse{
			{Text: "Welcome!"},
			{Text: "Great, let me send you the deposit link."},
		},
	}
	svc := NewLLMService(mockLLM, rdb, nil, "test-model", logging.Default(),
		WithLeadsRepo(leadsRepo),
		WithClinicStore(clinicStore),
		WithAPIBaseURL("https://api.example.com"),
	)

	convID := "conv-square-" + uuid.NewString()
	_, err = svc.StartConversation(ctx, StartRequest{
		ConversationID: convID,
		LeadID:         lead.ID,
		OrgID:          "org-square",
		Intro:          "Botox please",
		Channel:        ChannelSMS,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// Simulate time selection state
	slotTime := time.Date(2026, 2, 10, 14, 0, 0, 0, time.UTC)
	state := &TimeSelectionState{
		PresentedSlots: []PresentedSlot{
			{Index: 1, DateTime: slotTime, TimeStr: "Mon Feb 10 at 2:00 PM", Service: "Botox", Available: true},
		},
		Service:     "Botox",
		PresentedAt: time.Now(),
	}
	if err := svc.history.SaveTimeSelectionState(ctx, convID, state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	resp, err := svc.ProcessMessage(ctx, MessageRequest{
		ConversationID: convID,
		Message:        "1",
		LeadID:         lead.ID,
		OrgID:          "org-square",
		Channel:        ChannelSMS,
		From:           "+15559876543",
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}

	// BookingRequest should be nil for Square clinics
	if resp.BookingRequest != nil {
		t.Error("expected BookingRequest to be nil for Square clinic")
	}
}

func TestMoxieTimeSelection_NoBookingURL_NoBookingRequest(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	clinicStore := clinic.NewStore(rdb)

	// Moxie clinic but no booking URL
	cfg := clinic.DefaultConfig("org-nourl")
	cfg.BookingPlatform = "moxie"
	cfg.BookingURL = "" // No URL configured
	if err := clinicStore.Set(ctx, cfg); err != nil {
		t.Fatalf("save clinic config: %v", err)
	}

	leadsRepo := leads.NewInMemoryRepository()
	lead, err := leadsRepo.Create(ctx, &leads.CreateLeadRequest{
		OrgID:   "org-nourl",
		Name:    "Test User",
		Phone:   "+15551111111",
		Source:  "sms",
		Message: "Botox",
	})
	if err != nil {
		t.Fatalf("create lead: %v", err)
	}

	mockLLM := &stubLLMClient{
		responses: []LLMResponse{
			{Text: "Welcome!"},
			{Text: "OK, noted."},
		},
	}
	svc := NewLLMService(mockLLM, rdb, nil, "test-model", logging.Default(),
		WithLeadsRepo(leadsRepo),
		WithClinicStore(clinicStore),
	)

	convID := "conv-nourl-" + uuid.NewString()
	_, err = svc.StartConversation(ctx, StartRequest{
		ConversationID: convID,
		LeadID:         lead.ID,
		OrgID:          "org-nourl",
		Intro:          "Botox",
		Channel:        ChannelSMS,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	slotTime := time.Date(2026, 2, 10, 10, 0, 0, 0, time.UTC)
	state := &TimeSelectionState{
		PresentedSlots: []PresentedSlot{
			{Index: 1, DateTime: slotTime, TimeStr: "Mon Feb 10 at 10:00 AM", Service: "Botox", Available: true},
		},
		Service:     "Botox",
		PresentedAt: time.Now(),
	}
	if err := svc.history.SaveTimeSelectionState(ctx, convID, state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	resp, err := svc.ProcessMessage(ctx, MessageRequest{
		ConversationID: convID,
		Message:        "1",
		LeadID:         lead.ID,
		OrgID:          "org-nourl",
		Channel:        ChannelSMS,
		From:           "+15551111111",
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}

	if resp.BookingRequest != nil {
		t.Error("expected BookingRequest to be nil when no booking URL is configured")
	}
}

func TestMoxieTimeSelection_NoEmail_BookingBlocked(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	clinicStore := clinic.NewStore(rdb)

	cfg := clinic.DefaultConfig("org-noemail")
	cfg.BookingPlatform = "moxie"
	cfg.BookingURL = "https://app.joinmoxie.com/booking/forever-22"
	if err := clinicStore.Set(ctx, cfg); err != nil {
		t.Fatalf("save clinic config: %v", err)
	}

	// Lead created WITHOUT email
	leadsRepo := leads.NewInMemoryRepository()
	lead, err := leadsRepo.Create(ctx, &leads.CreateLeadRequest{
		OrgID:   "org-noemail",
		Name:    "Andy Wolf",
		Phone:   "+15551234567",
		Source:  "sms",
		Message: "Botox",
	})
	if err != nil {
		t.Fatalf("create lead: %v", err)
	}

	mockLLM := &stubLLMClient{
		responses: []LLMResponse{
			{Text: "Welcome!"},
			{Text: "I need your email to complete the booking."},
		},
	}
	svc := NewLLMService(mockLLM, rdb, nil, "test-model", logging.Default(),
		WithLeadsRepo(leadsRepo),
		WithClinicStore(clinicStore),
		WithAPIBaseURL("https://api.example.com"),
	)

	convID := "conv-noemail-" + uuid.NewString()
	_, err = svc.StartConversation(ctx, StartRequest{
		ConversationID: convID,
		LeadID:         lead.ID,
		OrgID:          "org-noemail",
		Intro:          "Botox please",
		Channel:        ChannelSMS,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	slotTime := time.Date(2026, 2, 10, 15, 30, 0, 0, time.UTC)
	state := &TimeSelectionState{
		PresentedSlots: []PresentedSlot{
			{Index: 1, DateTime: slotTime, TimeStr: "Mon Feb 10 at 3:30 PM", Service: "Botox", Available: true},
		},
		Service:     "Botox",
		BookingURL:  "https://app.joinmoxie.com/booking/forever-22",
		PresentedAt: time.Now(),
	}
	if err := svc.history.SaveTimeSelectionState(ctx, convID, state); err != nil {
		t.Fatalf("save time selection state: %v", err)
	}

	// User selects slot but NO email in conversation or lead
	resp, err := svc.ProcessMessage(ctx, MessageRequest{
		ConversationID: convID,
		Message:        "1",
		LeadID:         lead.ID,
		OrgID:          "org-noemail",
		Channel:        ChannelSMS,
		From:           "+15551234567",
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}

	// BookingRequest should be nil — email required for Moxie
	if resp.BookingRequest != nil {
		t.Error("expected BookingRequest to be nil when email is missing")
	}
}

func TestMoxieTimeSelection_EmailFallbackFromConversation(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	clinicStore := clinic.NewStore(rdb)

	cfg := clinic.DefaultConfig("org-emailfb")
	cfg.BookingPlatform = "moxie"
	cfg.BookingURL = "https://app.joinmoxie.com/booking/forever-22"
	if err := clinicStore.Set(ctx, cfg); err != nil {
		t.Fatalf("save clinic config: %v", err)
	}

	// Lead created WITHOUT email
	leadsRepo := leads.NewInMemoryRepository()
	lead, err := leadsRepo.Create(ctx, &leads.CreateLeadRequest{
		OrgID:   "org-emailfb",
		Name:    "Andy Wolf",
		Phone:   "+15551234567",
		Source:  "sms",
		Message: "Botox",
	})
	if err != nil {
		t.Fatalf("create lead: %v", err)
	}

	mockLLM := &stubLLMClient{
		responses: []LLMResponse{
			{Text: "Welcome!"},
			{Text: "Thanks for your email!"},
			{Text: "Great, I've booked your appointment."},
		},
	}
	svc := NewLLMService(mockLLM, rdb, nil, "test-model", logging.Default(),
		WithLeadsRepo(leadsRepo),
		WithClinicStore(clinicStore),
		WithAPIBaseURL("https://api.example.com"),
	)

	convID := "conv-emailfb-" + uuid.NewString()
	_, err = svc.StartConversation(ctx, StartRequest{
		ConversationID: convID,
		LeadID:         lead.ID,
		OrgID:          "org-emailfb",
		Intro:          "Botox please",
		Channel:        ChannelSMS,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// User provides email in a follow-up message (gets saved to lead via extraction)
	_, err = svc.ProcessMessage(ctx, MessageRequest{
		ConversationID: convID,
		Message:        "My email is andy@example.com",
		LeadID:         lead.ID,
		OrgID:          "org-emailfb",
		Channel:        ChannelSMS,
		From:           "+15551234567",
	})
	if err != nil {
		t.Fatalf("process email message: %v", err)
	}

	// Verify email was saved to lead
	updatedLead, err := leadsRepo.GetByID(ctx, "org-emailfb", lead.ID)
	if err != nil {
		t.Fatalf("get lead: %v", err)
	}
	if updatedLead.Email != "andy@example.com" {
		t.Fatalf("expected lead email to be andy@example.com, got %q", updatedLead.Email)
	}

	// Now set up time selection state and select a slot
	slotTime := time.Date(2026, 2, 10, 15, 30, 0, 0, time.UTC)
	state := &TimeSelectionState{
		PresentedSlots: []PresentedSlot{
			{Index: 1, DateTime: slotTime, TimeStr: "Mon Feb 10 at 3:30 PM", Service: "Botox", Available: true},
		},
		Service:     "Botox",
		BookingURL:  "https://app.joinmoxie.com/booking/forever-22",
		PresentedAt: time.Now(),
	}
	if err := svc.history.SaveTimeSelectionState(ctx, convID, state); err != nil {
		t.Fatalf("save time selection state: %v", err)
	}

	resp, err := svc.ProcessMessage(ctx, MessageRequest{
		ConversationID: convID,
		Message:        "1",
		LeadID:         lead.ID,
		OrgID:          "org-emailfb",
		Channel:        ChannelSMS,
		From:           "+15551234567",
	})
	if err != nil {
		t.Fatalf("process slot selection: %v", err)
	}

	// BookingRequest should be populated with email from lead (saved earlier via extraction)
	if resp.BookingRequest == nil {
		t.Fatal("expected BookingRequest to be populated when email was provided in conversation")
	}
	if resp.BookingRequest.Email != "andy@example.com" {
		t.Errorf("Email = %q, want andy@example.com", resp.BookingRequest.Email)
	}
}
