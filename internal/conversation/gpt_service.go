package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	openai "github.com/sashabaranov/go-openai"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const (
	defaultOpenAISystemPrompt = `You are MedSpa AI Concierge, a warm, trustworthy assistant for a medical spa.

ANSWERING SERVICE QUESTIONS:
You CAN and SHOULD answer general questions about medspa services and treatments:
- Dermal fillers: Injectable treatments that add volume, smooth wrinkles, and enhance facial contours. Common areas include lips, cheeks, and nasolabial folds. Results typically last 6-18 months.
- Botox/Neurotoxins: Injections that temporarily relax muscles to reduce wrinkles, especially forehead lines, crow's feet, and frown lines. Results last 3-4 months.
- Chemical peels: Exfoliating treatments that improve skin texture, tone, and reduce fine lines.
- Microneedling: Collagen-stimulating treatment using tiny needles to improve skin texture and reduce scarring.
- Laser treatments: Various options for hair removal, skin resurfacing, and pigmentation correction.
- Facials: Customized skincare treatments for cleansing, hydration, and rejuvenation.

When asked about services, provide helpful general information, then offer to help schedule a consultation.

🚨 QUALIFICATION CHECKLIST - You need FOUR things before offering deposit:
1. NAME - The patient's first name (for personalized service)
2. SERVICE - What treatment are they interested in?
3. PATIENT TYPE - Are they a new or existing/returning patient?
4. SCHEDULE - Day AND time preferences (weekdays/weekends + morning/afternoon/evening)

🚨 STEP 1 - READ THE USER'S MESSAGE CAREFULLY:
Parse for qualification information:
- Name: Look for "my name is [Name]", "I'm [Name]", "this is [Name]", or "call me [Name]"
- Service mentioned (Botox, filler, facial, consultation, etc.)
- Patient type: "new", "first time", "never been" = NEW patient
- Patient type: "returning", "been before", "existing", "come back" = EXISTING patient
- "weekdays" or "weekday" = day preference is WEEKDAYS
- "weekends" or "weekend" = day preference is WEEKENDS
- "mornings" or "morning" = time preference is MORNINGS
- "afternoons" or "afternoon" = time preference is AFTERNOONS
- "evenings" or "evening" = time preference is EVENINGS

🚨 STEP 2 - CHECK CONVERSATION HISTORY:
Look through ALL previous messages for information already mentioned.
IMPORTANT: Also check if a DEPOSIT HAS BEEN PAID (indicated by system message about payment).

🚨 STEP 3 - ASK FOR MISSING INFO (in this priority order):

IF DEPOSIT ALREADY PAID (check for system message about successful payment):
  → DO NOT offer another deposit or ask about booking
  → Answer their questions helpfully
  → Do NOT repeat the confirmation message - they already know their deposit was received
  → If they ask about next steps: "Our team will call you within 24 hours to confirm a specific date and time that works for you."

IF missing NAME (ask early to personalize the conversation):
  → "I'd love to help! May I have your first name?"

IF missing SERVICE (and have name):
  → "Thanks, [Name]! What treatment or service are you interested in?"

IF missing PATIENT TYPE (and have name + service):
  → "Are you a new patient or have you visited us before?"

IF missing DAY preference (and have name + service + patient type):
  → "What days work best for you - weekdays or weekends?"

IF missing TIME preference (and have day):
  → "Do you prefer mornings, afternoons, or evenings?"

IF you have ALL FOUR (name + service + patient type + day/time) AND NO DEPOSIT PAID YET:
  → "Perfect, [Name]! I've noted [day] [time] for your [service]. To secure priority booking, we collect a small $50 refundable deposit. Would you like to proceed?"

CRITICAL - YOU DO NOT HAVE ACCESS TO THE CLINIC'S CALENDAR:
- NEVER claim to know specific available times or dates
- The clinic team will call to confirm an actual available slot

DEPOSIT MESSAGING:
- Deposits are FULLY REFUNDABLE if no mutually agreeable time is found
- Deposit holders get PRIORITY scheduling - called back first
- The deposit applies toward their treatment cost
- Never pressure - always give the option to skip the deposit and wait for a callback
- DO NOT mention "call within 24 hours" or callback timeframes UNTIL AFTER they complete the deposit
- When offering deposit, just say "Would you like to proceed?" - the payment link is sent automatically

AFTER CUSTOMER AGREES TO DEPOSIT:
- Say: "Great! You'll receive a secure payment link shortly."
- DO NOT say "you're all set" - the booking is NOT confirmed until staff calls them
- DO NOT mention the 24-hour callback yet - that message comes after payment confirmation

AFTER DEPOSIT IS PAID (system will inject this context):
- Acknowledge once: "Thank you! Your deposit has been received. Our team will call you within 24 hours to confirm a specific date and time."
- After this ONE acknowledgment, do NOT repeat it - just answer any follow-up questions normally
- The patient is NOT "all set" - they still need the confirmation call to finalize the booking

COMMUNICATION STYLE:
- Keep responses short (2-3 sentences max), friendly, and actionable
- Be HIPAA-compliant: never diagnose conditions or give personalized medical advice
- For personal medical questions (symptoms, dosing, contraindications): "That's a great question for your provider during your consultation!"
- You CAN explain what treatments ARE and how they generally work
- Do not promise to send payment links; the platform sends those automatically via SMS

SAMPLE CONVERSATION:
Customer: "What are dermal fillers?"
You: "Dermal fillers are injectable treatments that add volume and smooth wrinkles. They're commonly used for lips, cheeks, and smile lines, with results lasting 6-18 months. Would you like to schedule a consultation to learn more about your options?"

Customer: "I want to book Botox"
You: "I'd love to help with Botox! Are you a new patient or have you visited us before?"

WHAT TO SAY IF ASKED ABOUT SPECIFIC TIMES:
- "I don't have real-time access to the schedule, but I'll make sure the team knows your preferences."
- "Let me get your preferred times and the clinic will reach out with available options that match."`
	maxHistoryMessages = 24
)

var gptTracer = otel.Tracer("medspa.internal.conversation.gpt")

var openaiLatency = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Namespace: "medspa",
		Subsystem: "conversation",
		Name:      "openai_latency_seconds",
		Help:      "Latency of OpenAI chat completions",
		// Focus on sub-10s buckets with a few higher ones for visibility.
		Buckets: []float64{0.25, 0.5, 1, 2, 3, 4, 5, 6, 8, 10, 15, 20, 30},
	},
	[]string{"model", "status"},
)

var depositDecisionTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "medspa",
		Subsystem: "conversation",
		Name:      "deposit_decision_total",
		Help:      "Counts GPT-based deposit decisions by outcome",
	},
	[]string{"model", "outcome"}, // outcome: collect, skip, error
)

func init() {
	prometheus.MustRegister(openaiLatency)
	prometheus.MustRegister(depositDecisionTotal)
}

// RegisterMetrics registers conversation metrics with a custom registry.
// Use this when exposing a non-default registry (e.g., HTTP handlers with a private registry).
func RegisterMetrics(reg prometheus.Registerer) {
	if reg == nil || reg == prometheus.DefaultRegisterer {
		return
	}
	reg.MustRegister(openaiLatency, depositDecisionTotal)
}

type chatClient interface {
	CreateChatCompletion(ctx context.Context, request openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error)
}

// DepositConfig allows callers to configure defaults used when GPT signals a deposit.
type DepositConfig struct {
	DefaultAmountCents int32
	SuccessURL         string
	CancelURL          string
	Description        string
}

type GPTOption func(*GPTService)

// WithDepositConfig sets the defaults applied to GPT-produced deposit intents.
func WithDepositConfig(cfg DepositConfig) GPTOption {
	return func(s *GPTService) {
		s.deposit = depositConfig(cfg)
	}
}

// WithEMR configures an EMR adapter for real-time availability lookup.
func WithEMR(emr *EMRAdapter) GPTOption {
	return func(s *GPTService) {
		s.emr = emr
	}
}

// WithLeadsRepo configures the leads repository for saving scheduling preferences.
func WithLeadsRepo(repo leads.Repository) GPTOption {
	return func(s *GPTService) {
		s.leadsRepo = repo
	}
}

// WithClinicStore configures the clinic config store for business hours awareness.
func WithClinicStore(store *clinic.Store) GPTOption {
	return func(s *GPTService) {
		s.clinicStore = store
	}
}

// PaymentStatusChecker checks if a lead has an open or completed deposit.
type PaymentStatusChecker interface {
	HasOpenDeposit(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID) (bool, error)
}

// WithPaymentChecker configures payment status checking for context injection.
func WithPaymentChecker(checker PaymentStatusChecker) GPTOption {
	return func(s *GPTService) {
		s.paymentChecker = checker
	}
}

type depositConfig struct {
	DefaultAmountCents int32
	SuccessURL         string
	CancelURL          string
	Description        string
}

// GPTService produces conversation responses using OpenAI and stores context in Redis.
type GPTService struct {
	client         chatClient
	rag            RAGRetriever
	emr            *EMRAdapter
	model          string
	logger         *logging.Logger
	history        *historyStore
	deposit        depositConfig
	leadsRepo      leads.Repository
	clinicStore    *clinic.Store
	paymentChecker PaymentStatusChecker
}

// NewGPTService returns a GPT-backed Service implementation.
func NewGPTService(client chatClient, redisClient *redis.Client, rag RAGRetriever, model string, logger *logging.Logger, opts ...GPTOption) *GPTService {
	if client == nil {
		panic("conversation: chat client cannot be nil")
	}
	if redisClient == nil {
		panic("conversation: redis client cannot be nil")
	}
	if logger == nil {
		logger = logging.Default()
	}
	if model == "" {
		model = "gpt-4o-mini"
	}

	service := &GPTService{
		client:  client,
		rag:     rag,
		model:   model,
		logger:  logger,
		history: newHistoryStore(redisClient, gptTracer),
	}

	for _, opt := range opts {
		opt(service)
	}
	// Apply sane defaults for deposits so callers don't have to provide options.
	if service.deposit.DefaultAmountCents == 0 {
		service.deposit.DefaultAmountCents = 5000
	}
	if strings.TrimSpace(service.deposit.Description) == "" {
		service.deposit.Description = "Appointment deposit"
	}

	return service
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
	history = s.appendContext(ctx, history, req.OrgID, req.LeadID, req.ClinicID, req.Intro)
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

	history = trimHistory(history, maxHistoryMessages)
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
// If the conversation doesn't exist, it automatically starts a new one.
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
		// If conversation doesn't exist, start a new one
		if strings.Contains(err.Error(), "unknown conversation") {
			return s.StartConversation(ctx, StartRequest{
				OrgID:          req.OrgID,
				ConversationID: req.ConversationID,
				LeadID:         req.LeadID,
				ClinicID:       req.ClinicID,
				Intro:          req.Message,
				Channel:        req.Channel,
				From:           req.From,
				To:             req.To,
				Metadata:       req.Metadata,
			})
		}
		span.RecordError(err)
		return nil, err
	}

	history = s.appendContext(ctx, history, req.OrgID, req.LeadID, req.ClinicID, req.Message)
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

	history = trimHistory(history, maxHistoryMessages)
	if err := s.history.Save(ctx, req.ConversationID, history); err != nil {
		span.RecordError(err)
		return nil, err
	}

	depositIntent, derr := s.extractDepositIntent(ctx, history)
	if derr != nil {
		span.RecordError(derr)
		s.logger.Warn("deposit intent extraction failed", "error", derr)
	} else if depositIntent == nil && looksLikeDepositAgreement(req.Message) {
		// Fallback heuristic: if the user explicitly agrees to a deposit in their message,
		// send a deposit intent even if the classifier skipped.
		depositIntent = &DepositIntent{
			AmountCents: s.deposit.DefaultAmountCents,
			Description: s.deposit.Description,
			SuccessURL:  s.deposit.SuccessURL,
			CancelURL:   s.deposit.CancelURL,
		}
		s.logger.Info("deposit intent inferred from explicit user agreement", "amount_cents", depositIntent.AmountCents)
	} else if depositIntent != nil {
		s.logger.Info("deposit intent extracted", "amount_cents", depositIntent.AmountCents)
	} else {
		s.logger.Debug("no deposit intent detected")
	}

	// Extract and save scheduling preferences if lead ID is provided
	if req.LeadID != "" && s.leadsRepo != nil {
		if err := s.extractAndSavePreferences(ctx, req.LeadID, history); err != nil {
			s.logger.Warn("failed to save scheduling preferences", "lead_id", req.LeadID, "error", err)
		}
	}

	return &Response{
		ConversationID: req.ConversationID,
		Message:        reply,
		Timestamp:      time.Now().UTC(),
		DepositIntent:  depositIntent,
	}, nil
}

// GetHistory retrieves the conversation history for a given conversation ID.
func (s *GPTService) GetHistory(ctx context.Context, conversationID string) ([]Message, error) {
	history, err := s.history.Load(ctx, conversationID)
	if err != nil {
		return nil, err
	}

	// Convert openai messages to our Message type, filtering out system messages
	var messages []Message
	for _, msg := range history {
		if msg.Role == openai.ChatMessageRoleSystem {
			continue // Don't expose system prompts
		}
		messages = append(messages, Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}
	return messages, nil
}

func (s *GPTService) generateResponse(ctx context.Context, history []openai.ChatCompletionMessage) (string, error) {
	ctx, span := gptTracer.Start(ctx, "conversation.openai")
	defer span.End()

	trimmed := trimHistory(history, maxHistoryMessages)
	req := openai.ChatCompletionRequest{
		Model:    s.model,
		Messages: trimmed,
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := s.client.CreateChatCompletion(callCtx, req)
	latency := time.Since(start)
	status := "ok"
	if err != nil {
		status = "error"
	}
	openaiLatency.WithLabelValues(s.model, status).Observe(latency.Seconds())
	if span.IsRecording() {
		span.SetAttributes(attribute.Float64("medspa.openai.latency_ms", float64(latency.Milliseconds())))
	}
	s.logger.Info("openai completion finished", "model", s.model, "latency_ms", latency.Milliseconds())
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

func (s *GPTService) appendContext(ctx context.Context, history []openai.ChatCompletionMessage, orgID, leadID, clinicID, query string) []openai.ChatCompletionMessage {
	// Append payment status context if available
	if s.paymentChecker != nil && orgID != "" && leadID != "" {
		orgUUID, orgErr := uuid.Parse(orgID)
		leadUUID, leadErr := uuid.Parse(leadID)
		if orgErr == nil && leadErr == nil {
			hasDeposit, err := s.paymentChecker.HasOpenDeposit(ctx, orgUUID, leadUUID)
			if err != nil {
				s.logger.Warn("failed to check payment status", "org_id", orgID, "lead_id", leadID, "error", err)
			} else if hasDeposit {
				history = append(history, openai.ChatCompletionMessage{
					Role:    openai.ChatMessageRoleSystem,
					Content: "IMPORTANT: This patient has ALREADY PAID their deposit. Do NOT offer another deposit or ask about booking. Their deposit is received and our team will call within 24 hours to confirm a specific date and time. The booking is NOT finalized yet - they still need the confirmation call. If this is their FIRST message after paying, acknowledge the deposit ONCE. After that, just answer questions normally without repeating the confirmation.",
				})
			}
		}
	}

	// Append clinic business hours context if available
	if s.clinicStore != nil && orgID != "" {
		cfg, err := s.clinicStore.Get(ctx, orgID)
		if err != nil {
			s.logger.Warn("failed to fetch clinic config", "org_id", orgID, "error", err)
		} else if cfg != nil {
			hoursContext := cfg.BusinessHoursContext(time.Now())
			history = append(history, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleSystem,
				Content: hoursContext,
			})
		}
	}

	// Append RAG context if available
	if s.rag != nil && strings.TrimSpace(query) != "" {
		snippets, err := s.rag.Query(ctx, clinicID, query, 3)
		if err != nil {
			s.logger.Error("failed to retrieve RAG context", "error", err)
		} else if len(snippets) > 0 {
			builder := strings.Builder{}
			builder.WriteString("Relevant clinic context:\n")
			for i, snippet := range snippets {
				builder.WriteString(fmt.Sprintf("%d. %s\n", i+1, snippet))
			}
			history = append(history, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleSystem,
				Content: builder.String(),
			})
		}
	}

	// Append real-time availability if EMR is configured and query mentions booking/appointment
	if s.emr != nil && s.emr.IsConfigured() && containsBookingIntent(query) {
		slots, err := s.emr.GetUpcomingAvailability(ctx, 7, "")
		if err != nil {
			s.logger.Warn("failed to fetch EMR availability", "error", err)
		} else if len(slots) > 0 {
			availabilityContext := FormatSlotsForGPT(slots, 5)
			history = append(history, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleSystem,
				Content: "Real-time appointment availability from clinic calendar:\n" + availabilityContext,
			})
		}
	}

	return history
}

// containsBookingIntent checks if the user message suggests they want to book.
func containsBookingIntent(msg string) bool {
	msg = strings.ToLower(msg)
	keywords := []string{"book", "appointment", "schedule", "available", "availability", "when can", "open slot", "time slot"}
	for _, kw := range keywords {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

func trimHistory(history []openai.ChatCompletionMessage, limit int) []openai.ChatCompletionMessage {
	if limit <= 0 || len(history) <= limit {
		return history
	}
	if len(history) == 0 {
		return history
	}

	var result []openai.ChatCompletionMessage
	system := history[0]
	if system.Role == openai.ChatMessageRoleSystem {
		result = append(result, system)
		remaining := limit - 1
		if remaining <= 0 {
			return result
		}
		start := len(history) - remaining
		if start < 1 {
			start = 1
		}
		result = append(result, history[start:]...)
		return result
	}
	return history[len(history)-limit:]
}

func (s *GPTService) extractDepositIntent(ctx context.Context, history []openai.ChatCompletionMessage) (*DepositIntent, error) {
	ctx, span := gptTracer.Start(ctx, "conversation.deposit_intent")
	defer span.End()

	outcome := "skip"
	var raw string
	defer func() {
		depositDecisionTotal.WithLabelValues(s.model, outcome).Inc()
	}()

	// Focus on the most recent turns to keep the prompt small.
	transcript := summarizeHistory(history, 8)
	prompt := fmt.Sprintf(`You are a decision agent for MedSpa AI. Analyze this conversation and decide if we should send a payment link to collect a deposit.

CRITICAL: Return ONLY a JSON object, nothing else. No markdown, no code fences, no explanation.

Return this exact format:
{"collect": true, "amount_cents": 5000, "description": "Refundable deposit", "success_url": "", "cancel_url": ""}

Rules:
- ONLY set collect=true if the customer EXPLICITLY agreed to the deposit with words like "yes", "sure", "ok", "proceed", "let's do it", "I'll pay", etc.
- Set collect=false if:
  - Customer hasn't been asked about the deposit yet
  - Customer was just offered the deposit but hasn't responded yet
  - Customer declined or said "no", "not now", "maybe later", etc.
  - The assistant just asked "Would you like to proceed?" - WAIT for their response
- Default amount: %d cents
- For success_url and cancel_url: use empty strings

Conversation:
%s`, s.deposit.DefaultAmountCents, transcript)

	req := openai.ChatCompletionRequest{
		Model: s.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: prompt},
		},
		Temperature: 0,
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
	}

	callCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	resp, err := s.client.CreateChatCompletion(callCtx, req)
	if err != nil {
		outcome = "error"
		s.maybeLogDepositClassifierError(raw, err)
		return nil, fmt.Errorf("conversation: deposit classification failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		outcome = "error"
		s.maybeLogDepositClassifierError(raw, errors.New("no choices"))
		return nil, errors.New("conversation: deposit classification returned no choices")
	}

	raw = strings.TrimSpace(resp.Choices[0].Message.Content)
	var decision struct {
		Collect     bool   `json:"collect"`
		AmountCents int32  `json:"amount_cents"`
		SuccessURL  string `json:"success_url"`
		CancelURL   string `json:"cancel_url"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(raw), &decision); err != nil {
		outcome = "error"
		s.maybeLogDepositClassifierError(raw, err)
		return nil, fmt.Errorf("conversation: deposit classification parse: %w", err)
	}
	if !decision.Collect {
		span.SetAttributes(attribute.Bool("medspa.deposit.collect", false))
		s.logger.Debug("deposit: classifier skipped", "model", s.model)
		return nil, nil
	}

	amount := decision.AmountCents
	if amount <= 0 {
		amount = s.deposit.DefaultAmountCents
	}
	outcome = "collect"

	intent := &DepositIntent{
		AmountCents: amount,
		Description: defaultString(decision.Description, s.deposit.Description),
		SuccessURL:  defaultString(decision.SuccessURL, s.deposit.SuccessURL),
		CancelURL:   defaultString(decision.CancelURL, s.deposit.CancelURL),
	}
	span.SetAttributes(
		attribute.Bool("medspa.deposit.collect", true),
		attribute.Int("medspa.deposit.amount_cents", int(amount)),
	)
	s.logger.Info("deposit: classifier collected",
		"model", s.model,
		"amount_cents", amount,
		"success_url_set", intent.SuccessURL != "",
		"cancel_url_set", intent.CancelURL != "",
		"description", intent.Description,
	)
	return intent, nil
}

func summarizeHistory(history []openai.ChatCompletionMessage, limit int) string {
	if limit > 0 && len(history) > limit {
		history = history[len(history)-limit:]
	}
	var builder strings.Builder
	for _, msg := range history {
		builder.WriteString(string(msg.Role))
		builder.WriteString(": ")
		builder.WriteString(msg.Content)
		builder.WriteString("\n")
	}
	return builder.String()
}

func (s *GPTService) maybeLogDepositClassifierError(raw string, err error) {
	if s == nil || s.logger == nil || err == nil {
		return
	}
	if !s.shouldSampleDepositLog() {
		return
	}
	masked := strings.TrimSpace(raw)
	if len(masked) > 512 {
		masked = masked[:512] + "...(truncated)"
	}
	s.logger.Warn("deposit: classifier error",
		"model", s.model,
		"error", err,
		"raw", masked,
	)
}

func (s *GPTService) shouldSampleDepositLog() bool {
	// 10% sampling to avoid noisy logs.
	return time.Now().UnixNano()%10 == 0
}

// looksLikeDepositAgreement returns true when a user message clearly indicates they want to pay a deposit.
// This is used as a deterministic fallback to avoid missing deposits due to LLM classifier variance.
func looksLikeDepositAgreement(message string) bool {
	msg := strings.ToLower(strings.TrimSpace(message))
	if msg == "" {
		return false
	}

	// Must mention deposit/payment to avoid false positives on generic "yes".
	hasDepositKeyword := strings.Contains(msg, "deposit") ||
		strings.Contains(msg, "pay") ||
		strings.Contains(msg, "payment") ||
		strings.Contains(msg, "secure my spot") ||
		strings.Contains(msg, "proceed")
	if !hasDepositKeyword {
		return false
	}

	// Positive intent markers.
	hasPositive := strings.Contains(msg, "yes") ||
		strings.Contains(msg, "sure") ||
		strings.Contains(msg, "ok") ||
		strings.Contains(msg, "okay") ||
		strings.Contains(msg, "proceed") ||
		strings.Contains(msg, "let's do it") ||
		strings.Contains(msg, "lets do it") ||
		strings.Contains(msg, "i'll pay") ||
		strings.Contains(msg, "i will pay")
	if !hasPositive {
		return false
	}

	// Negative intent markers override.
	hasNegative := strings.Contains(msg, "no deposit") ||
		strings.Contains(msg, "don't want") ||
		strings.Contains(msg, "do not want") ||
		strings.Contains(msg, "not paying") ||
		strings.Contains(msg, "maybe later") ||
		strings.Contains(msg, "skip") ||
		strings.Contains(msg, "not now")
	return !hasNegative
}

// extractAndSavePreferences extracts scheduling preferences from conversation history and saves them
func (s *GPTService) extractAndSavePreferences(ctx context.Context, leadID string, history []openai.ChatCompletionMessage) error {
	prefs := leads.SchedulingPreferences{}
	hasPreferences := false

	// Scan the conversation for scheduling-related keywords
	conversation := strings.ToLower(summarizeHistory(history, 8))

	// Extract preferred days (only from user messages to avoid confusion with business hours)
	userMessages := ""
	userMessagesOriginal := "" // Keep original case for name extraction
	for _, msg := range history {
		if msg.Role == openai.ChatMessageRoleUser {
			userMessages += strings.ToLower(msg.Content) + " "
			userMessagesOriginal += msg.Content + " "
		}
	}

	// Extract patient name from user messages
	// Patterns: "my name is X", "I'm X", "this is X", "call me X", "it's X"
	namePatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)my name is\s+([A-Z][a-z]+)`),
		regexp.MustCompile(`(?i)i'?m\s+([A-Z][a-z]+)(?:\s|,|\.|\!|$)`),
		regexp.MustCompile(`(?i)this is\s+([A-Z][a-z]+)`),
		regexp.MustCompile(`(?i)call me\s+([A-Z][a-z]+)`),
		regexp.MustCompile(`(?i)it'?s\s+([A-Z][a-z]+)(?:\s|,|\.|\!|$)`),
		regexp.MustCompile(`(?i)name'?s\s+([A-Z][a-z]+)`),
	}

	for _, pattern := range namePatterns {
		if matches := pattern.FindStringSubmatch(userMessagesOriginal); len(matches) > 1 {
			name := strings.TrimSpace(matches[1])
			// Validate it looks like a name (2-20 chars, not a common word)
			if len(name) >= 2 && len(name) <= 20 && !isCommonWord(name) {
				prefs.Name = name
				hasPreferences = true
				break
			}
		}
	}

	// Also check for standalone name response (single capitalized word after AI asked for name)
	if prefs.Name == "" {
		for i, msg := range history {
			if msg.Role == openai.ChatMessageRoleUser {
				// Check if previous assistant message asked for name
				if i > 0 && history[i-1].Role == openai.ChatMessageRoleAssistant {
					prevMsg := strings.ToLower(history[i-1].Content)
					if strings.Contains(prevMsg, "name") && (strings.Contains(prevMsg, "may i") || strings.Contains(prevMsg, "what") || strings.Contains(prevMsg, "your")) {
						// This user message might be just their name
						content := strings.TrimSpace(msg.Content)
						words := strings.Fields(content)
						if len(words) >= 1 && len(words) <= 3 {
							// Take first word that looks like a name
							for _, word := range words {
								cleaned := strings.Trim(word, ".,!?")
								if len(cleaned) >= 2 && len(cleaned) <= 20 && isCapitalized(cleaned) && !isCommonWord(cleaned) {
									prefs.Name = cleaned
									hasPreferences = true
									break
								}
							}
						}
					}
				}
			}
		}
	}

	// Only extract booking preferences if conversation contains booking-related intent
	hasBookingIntent := strings.Contains(conversation, "book") ||
		strings.Contains(conversation, "appointment") ||
		strings.Contains(conversation, "schedule") ||
		strings.Contains(conversation, "available")

	if hasBookingIntent {
		// Extract service interest
		services := []string{"botox", "filler", "dermal filler", "consultation", "laser", "facial", "peel", "microneedling"}
		for _, service := range services {
			if strings.Contains(conversation, service) {
				prefs.ServiceInterest = service
				hasPreferences = true
				break
			}
		}
	}

	if strings.Contains(userMessages, "weekday") {
		prefs.PreferredDays = "weekdays"
		hasPreferences = true
	} else if strings.Contains(userMessages, "weekend") {
		prefs.PreferredDays = "weekends"
		hasPreferences = true
	} else if strings.Contains(userMessages, "any day") || strings.Contains(userMessages, "flexible") {
		prefs.PreferredDays = "any"
		hasPreferences = true
	}

	// Extract preferred times (only from user messages)
	if strings.Contains(userMessages, "morning") {
		prefs.PreferredTimes = "morning"
		hasPreferences = true
	} else if strings.Contains(userMessages, "afternoon") {
		prefs.PreferredTimes = "afternoon"
		hasPreferences = true
	} else if strings.Contains(userMessages, "evening") {
		prefs.PreferredTimes = "evening"
		hasPreferences = true
	}

	// Only save if we found any preferences
	if !hasPreferences {
		return nil
	}

	// Add conversation notes for context
	prefs.Notes = fmt.Sprintf("Auto-extracted from conversation at %s", time.Now().Format(time.RFC3339))

	return s.leadsRepo.UpdateSchedulingPreferences(ctx, leadID, prefs)
}

// isCapitalized checks if a string starts with an uppercase letter
func isCapitalized(s string) bool {
	if len(s) == 0 {
		return false
	}
	return s[0] >= 'A' && s[0] <= 'Z'
}

// isCommonWord checks if a word is a common English word that shouldn't be treated as a name
func isCommonWord(word string) bool {
	common := map[string]bool{
		"the": true, "and": true, "for": true, "are": true, "but": true,
		"not": true, "you": true, "all": true, "can": true, "her": true,
		"was": true, "one": true, "our": true, "out": true, "day": true,
		"had": true, "has": true, "his": true, "how": true, "its": true,
		"may": true, "new": true, "now": true, "old": true, "see": true,
		"way": true, "who": true, "boy": true, "did": true, "get": true,
		"let": true, "put": true, "say": true, "she": true, "too": true,
		"use": true, "yes": true, "no": true, "hi": true, "hey": true,
		"thanks": true, "thank": true, "please": true, "ok": true, "okay": true,
		"sure": true, "good": true, "great": true, "fine": true, "well": true,
		"just": true, "like": true, "want": true, "need": true, "have": true,
		"interested": true, "looking": true, "book": true, "appointment": true,
		"morning": true, "afternoon": true, "evening": true, "weekday": true,
		"weekend": true, "available": true, "schedule": true, "time": true,
		"botox": true, "filler": true, "facial": true, "laser": true,
		"consultation": true, "treatment": true, "service": true,
	}
	return common[strings.ToLower(word)]
}
