package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	openai "github.com/sashabaranov/go-openai"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const (
	defaultOpenAISystemPrompt = "You are MedSpa AI Concierge, a warm, trustworthy assistant for a medical spa. Keep responses short, actionable, and compliant with HIPAA. Never invent medical advice. Guide leads toward booking by clarifying needs, suggesting available services, and offering to reserve time with a provider. Do not promise to send payment links; the platform sends those automatically via SMS. If a deposit is already collected, thank them and confirm the hold—do not say you'll send another link."
	maxHistoryMessages        = 12
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

type depositConfig struct {
	DefaultAmountCents int32
	SuccessURL         string
	CancelURL          string
	Description        string
}

// GPTService produces conversation responses using OpenAI and stores context in Redis.
type GPTService struct {
	client  chatClient
	rag     RAGRetriever
	model   string
	logger  *logging.Logger
	history *historyStore
	deposit depositConfig
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
		model = "gpt-5-mini"
	}
	if logger == nil {
		logger = logging.Default()
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

	history = trimHistory(history, maxHistoryMessages)
	if err := s.history.Save(ctx, req.ConversationID, history); err != nil {
		span.RecordError(err)
		return nil, err
	}

	depositIntent, derr := s.extractDepositIntent(ctx, history)
	if derr != nil {
		span.RecordError(derr)
	}

	return &Response{
		ConversationID: req.ConversationID,
		Message:        reply,
		Timestamp:      time.Now().UTC(),
		DepositIntent:  depositIntent,
	}, nil
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
	prompt := fmt.Sprintf(`You are a decision agent for MedSpa AI. Decide if we should send a payment link to collect a deposit to secure an appointment. Return ONLY JSON with the keys: collect (boolean), amount_cents (integer), description (string), success_url (string), cancel_url (string). Use %d cents as the default deposit if unsure. If no deposit is appropriate, return collect:false and zeros/empty strings.`,
		s.deposit.DefaultAmountCents,
	)

	req := openai.ChatCompletionRequest{
		Model: s.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: prompt},
			{Role: openai.ChatMessageRoleUser, Content: transcript},
		},
		Temperature: 0,
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
