package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/compliance"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	moxieclient "github.com/wolfman30/medspa-ai-platform/internal/moxie"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"regexp"
	"strings"
	"time"
)

const (
	maxHistoryMessages           = 40
	phiDeflectionReply           = "Thanks for sharing. I can help with booking and general questions, but I can't provide medical advice over text. Please call the clinic for medical guidance or discuss this with your provider during your consultation."
	medicalAdviceDeflectionReply = "I can help with booking and general questions, but I can't provide medical advice over text. Please call the clinic for medical guidance or discuss this with your provider during your consultation."
)

var llmTracer = otel.Tracer("medspa.internal.conversation.llm")

var llmLatency = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Namespace: "medspa",
		Subsystem: "conversation",
		Name:      "llm_latency_seconds",
		Help:      "Latency of LLM completions",
		// Focus on sub-10s buckets with a few higher ones for visibility.
		Buckets: []float64{0.25, 0.5, 1, 2, 3, 4, 5, 6, 8, 10, 15, 20, 30},
	},
	[]string{"model", "status"},
)

var llmTokensTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "medspa",
		Subsystem: "conversation",
		Name:      "llm_tokens_total",
		Help:      "Tokens used by the LLM",
	},
	[]string{"model", "type"}, // type: input, output, total
)

var depositDecisionTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "medspa",
		Subsystem: "conversation",
		Name:      "deposit_decision_total",
		Help:      "Counts LLM-based deposit decisions by outcome",
	},
	[]string{"model", "outcome"}, // outcome: collect, skip, error
)

var (
	depositAffirmativeRE = regexp.MustCompile(`(?i)(?:\b(?:yes|yeah|yea|sure|ok|okay|absolutely|definitely|proceed)\b|let'?s do it|i'?ll pay|i will pay)`)
	depositNegativeRE    = regexp.MustCompile(`(?i)(?:no deposit|don'?t want|do not want|not paying|not now|maybe(?: later)?|later|skip|no thanks|nope)`)
	depositKeywordRE     = regexp.MustCompile(`(?i)(?:\b(?:deposit|payment)\b|\bpay\b|secure (?:my|your) spot|hold (?:my|your) spot)`)
	depositAskRE         = regexp.MustCompile(`(?i)(?:\bdeposit\b|refundable deposit|payment link|secure (?:my|your) spot|hold (?:my|your) spot|pay a deposit)`)

	// sanitizeSMSResponse regexes
	smsItalicRE     = regexp.MustCompile(`\*([^\s*][^*]*[^\s*])\*`)
	smsBulletRE     = regexp.MustCompile(`(?m)^[\s]*[-•]\s+`)
	smsNumberedRE   = regexp.MustCompile(`(?m)^[\s]*\d+\.\s+`)
	smsMultiSpaceRE = regexp.MustCompile(`\s{2,}`)
)

func init() {
	prometheus.MustRegister(llmLatency)
	prometheus.MustRegister(llmTokensTotal)
	prometheus.MustRegister(depositDecisionTotal)
}

// RegisterMetrics registers conversation metrics with a custom registry.
// Use this when exposing a non-default registry (e.g., HTTP handlers with a private registry).
func RegisterMetrics(reg prometheus.Registerer) {
	if reg == nil || reg == prometheus.DefaultRegisterer {
		return
	}
	reg.MustRegister(llmLatency, llmTokensTotal, depositDecisionTotal)
}

// DepositConfig allows callers to configure defaults used when the LLM signals a deposit.
type DepositConfig struct {
	DefaultAmountCents int32
	SuccessURL         string
	CancelURL          string
	Description        string
}

type LLMOption func(*LLMService)

// WithDepositConfig sets the defaults applied to LLM-produced deposit intents.
func WithDepositConfig(cfg DepositConfig) LLMOption {
	return func(s *LLMService) {
		s.deposit = depositConfig(cfg)
	}
}

// WithEMR configures an EMR adapter for real-time availability lookup.
func WithEMR(emr *EMRAdapter) LLMOption {
	return func(s *LLMService) {
		s.emr = emr
	}
}

// WithBrowserAdapter configures a browser adapter for scraping booking page availability.
// This is used when EMR integration is not available but a booking URL is configured.
func WithBrowserAdapter(browser *BrowserAdapter) LLMOption {
	return func(s *LLMService) {
		s.browser = browser
	}
}

// WithMoxieClient configures the direct Moxie GraphQL API client for fast availability queries.
func WithMoxieClient(client *moxieclient.Client) LLMOption {
	return func(s *LLMService) {
		s.moxieClient = client
	}
}

// WithLeadsRepo configures the leads repository for saving scheduling preferences.
func WithLeadsRepo(repo leads.Repository) LLMOption {
	return func(s *LLMService) {
		s.leadsRepo = repo
	}
}

// WithClinicStore configures the clinic config store for business hours awareness.
func WithClinicStore(store *clinic.Store) LLMOption {
	return func(s *LLMService) {
		s.clinicStore = store
	}
}

// WithAuditService configures compliance audit logging.
func WithAuditService(audit *compliance.AuditService) LLMOption {
	return func(s *LLMService) {
		s.audit = audit
	}
}

// PaymentStatusChecker checks if a lead has an open or completed deposit.
type PaymentStatusChecker interface {
	HasOpenDeposit(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID) (bool, error)
}

// WithPaymentChecker configures payment status checking for context injection.
func WithPaymentChecker(checker PaymentStatusChecker) LLMOption {
	return func(s *LLMService) {
		s.paymentChecker = checker
	}
}

// WithAPIBaseURL sets the public API base URL (used for building callback URLs).
func WithAPIBaseURL(url string) LLMOption {
	return func(s *LLMService) {
		s.apiBaseURL = url
	}
}

type depositConfig struct {
	DefaultAmountCents int32
	SuccessURL         string
	CancelURL          string
	Description        string
}

// LLMService produces conversation responses using a configured LLM and stores context in Redis.
type LLMService struct {
	client          LLMClient
	rag             RAGRetriever
	emr             *EMRAdapter
	browser         *BrowserAdapter
	moxieClient     *moxieclient.Client
	model           string
	logger          *logging.Logger
	history         *historyStore
	deposit         depositConfig
	leadsRepo       leads.Repository
	clinicStore     *clinic.Store
	audit           *compliance.AuditService
	paymentChecker  PaymentStatusChecker
	faqClassifier   *FAQClassifier
	variantResolver *VariantResolver
	apiBaseURL      string // Public API base URL for callback URLs
	events          *EventLogger
}

// NewLLMService returns an LLM-backed Service implementation.
func NewLLMService(client LLMClient, redisClient *redis.Client, rag RAGRetriever, model string, logger *logging.Logger, opts ...LLMOption) *LLMService {
	if client == nil {
		panic("conversation: llm client cannot be nil")
	}
	if redisClient == nil {
		panic("conversation: redis client cannot be nil")
	}
	if logger == nil {
		logger = logging.Default()
	}
	if model == "" {
		// Widely available small model; override in config for Claude Haiku 4.5, etc.
		model = "anthropic.claude-3-haiku-20240307-v1:0"
	}

	service := &LLMService{
		client:          client,
		rag:             rag,
		model:           model,
		logger:          logger,
		history:         newHistoryStore(redisClient, llmTracer),
		faqClassifier:   NewFAQClassifier(client),
		variantResolver: NewVariantResolver(client, model, logger),
		events:          NewEventLogger(logger),
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
func (s *LLMService) StartConversation(ctx context.Context, req StartRequest) (*Response, error) {
	redactedIntro, sawPHI := RedactPHI(req.Intro)
	medicalKeywords := []string(nil)
	if !sawPHI {
		medicalKeywords = detectMedicalAdvice(req.Intro)
		if len(medicalKeywords) > 0 {
			redactedIntro = "[REDACTED]"
		}
	}

	// Prompt injection detection on first message.
	injectionResult := ScanForPromptInjection(req.Intro)
	if injectionResult.Blocked {
		s.events.PromptInjectionDetected(ctx, req.ConversationID, req.OrgID, true, injectionResult.Score, injectionResult.Reasons)
		s.logger.Warn("StartConversation: prompt injection BLOCKED",
			"org_id", req.OrgID,
			"score", injectionResult.Score,
			"reasons", injectionResult.Reasons,
		)
		if s.audit != nil && strings.TrimSpace(req.OrgID) != "" {
			_ = s.audit.LogPromptInjection(ctx, req.OrgID, req.ConversationID, req.LeadID, injectionResult.Reasons)
		}
		return &Response{ConversationID: req.ConversationID, Message: blockedReply, Timestamp: time.Now().UTC()}, nil
	}
	if injectionResult.Score >= warnThreshold {
		s.events.PromptInjectionDetected(ctx, req.ConversationID, req.OrgID, false, injectionResult.Score, injectionResult.Reasons)
		s.logger.Warn("StartConversation: prompt injection WARNING",
			"org_id", req.OrgID,
			"score", injectionResult.Score,
			"reasons", injectionResult.Reasons,
		)
		req.Intro = SanitizeForLLM(req.Intro)
	}

	s.events.ConversationStarted(ctx, req.ConversationID, req.OrgID, req.LeadID, req.From, string(req.Source))

	s.logger.Info("StartConversation called",
		"conversation_id", req.ConversationID,
		"org_id", req.OrgID,
		"intro", redactedIntro,
		"source", req.Source,
	)

	ctx, span := llmTracer.Start(ctx, "conversation.start")
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

	safeReq := req
	if sawPHI {
		safeReq.Intro = redactedIntro
	}

	// Get clinic-configured deposit amount and booking platform for system prompt customization
	depositCents := s.deposit.DefaultAmountCents
	var usesMoxie bool
	var startCfg *clinic.Config
	if s.clinicStore != nil && req.OrgID != "" {
		if cfg, err := s.clinicStore.Get(ctx, req.OrgID); err == nil && cfg != nil {
			startCfg = cfg
			if cfg.DepositAmountCents > 0 {
				depositCents = int32(cfg.DepositAmountCents)
			}
			usesMoxie = cfg.UsesMoxieBooking()
		}
	}
	var systemPrompt string
	if isVoiceChannel(req.Channel) {
		systemPrompt = buildVoiceSystemPrompt(int(depositCents), usesMoxie, startCfg)
	} else {
		systemPrompt = buildSystemPrompt(int(depositCents), usesMoxie, startCfg)
	}

	if req.Silent {
		history := []ChatMessage{
			{Role: ChatRoleSystem, Content: systemPrompt},
		}
		history = s.appendContext(ctx, history, req.OrgID, req.LeadID, req.ClinicID, "")
		// Add the ack message to history so the AI knows what was already said
		if req.AckMessage != "" {
			history = append(history, ChatMessage{
				Role:    ChatRoleAssistant,
				Content: req.AckMessage,
			})
		}
		history = append(history, ChatMessage{
			Role:    ChatRoleSystem,
			Content: "Context: The auto-reply above was already sent. Do NOT greet again, do NOT say 'Hey there' or 'Hi there' or 'Thanks for reaching out'. Just respond directly to whatever the patient says next.",
		})
		history = trimHistory(history, maxHistoryMessages)
		if err := s.history.Save(ctx, conversationID, history); err != nil {
			span.RecordError(err)
			return nil, err
		}
		if sawPHI && s.audit != nil && strings.TrimSpace(req.OrgID) != "" {
			_ = s.audit.LogPHIDetected(ctx, req.OrgID, conversationID, req.LeadID, req.Intro, "keyword")
		}
		return &Response{
			ConversationID: conversationID,
			Message:        "",
			Timestamp:      time.Now().UTC(),
		}, nil
	}

	if sawPHI {
		history := []ChatMessage{
			{Role: ChatRoleSystem, Content: systemPrompt},
		}
		history = s.appendContext(ctx, history, req.OrgID, req.LeadID, req.ClinicID, "")
		history = append(history, ChatMessage{
			Role:    ChatRoleUser,
			Content: formatIntroMessage(safeReq, conversationID),
		})
		history = append(history, ChatMessage{
			Role:    ChatRoleAssistant,
			Content: phiDeflectionReply,
		})
		history = trimHistory(history, maxHistoryMessages)
		if err := s.history.Save(ctx, conversationID, history); err != nil {
			span.RecordError(err)
			return nil, err
		}
		if s.audit != nil && strings.TrimSpace(req.OrgID) != "" {
			_ = s.audit.LogPHIDetected(ctx, req.OrgID, conversationID, req.LeadID, req.Intro, "keyword")
		}
		return &Response{
			ConversationID: conversationID,
			Message:        phiDeflectionReply,
			Timestamp:      time.Now().UTC(),
		}, nil
	}

	if len(medicalKeywords) > 0 {
		history := []ChatMessage{
			{Role: ChatRoleSystem, Content: systemPrompt},
		}
		safeReq := req
		safeReq.Intro = "[REDACTED]"
		history = s.appendContext(ctx, history, req.OrgID, req.LeadID, req.ClinicID, "")
		history = append(history, ChatMessage{
			Role:    ChatRoleUser,
			Content: formatIntroMessage(safeReq, conversationID),
		})
		history = append(history, ChatMessage{
			Role:    ChatRoleAssistant,
			Content: medicalAdviceDeflectionReply,
		})
		history = trimHistory(history, maxHistoryMessages)
		if err := s.history.Save(ctx, conversationID, history); err != nil {
			span.RecordError(err)
			return nil, err
		}
		if s.audit != nil && strings.TrimSpace(req.OrgID) != "" {
			_ = s.audit.LogMedicalAdviceRefused(ctx, req.OrgID, conversationID, req.LeadID, "[REDACTED]", medicalKeywords)
		}
		return &Response{
			ConversationID: conversationID,
			Message:        medicalAdviceDeflectionReply,
			Timestamp:      time.Now().UTC(),
		}, nil
	}

	history := []ChatMessage{
		{Role: ChatRoleSystem, Content: systemPrompt},
	}
	history = s.appendContext(ctx, history, req.OrgID, req.LeadID, req.ClinicID, req.Intro)
	history = append(history, ChatMessage{
		Role:    ChatRoleUser,
		Content: formatIntroMessage(safeReq, conversationID),
	})

	// Apply qualification ordering guardrails (same as ProcessMessage).
	if startCfg != nil && startCfg.UsesMoxieBooking() {
		prefs, _ := extractPreferences(history, serviceAliasesFromConfig(startCfg))

		// Name guardrail: ask for name first if we have service but not name.
		// Skip if the last assistant message already asked for the name (avoid duplicate asks).
		if prefs.ServiceInterest != "" && prefs.Name == "" && !lastAssistantAskedForName(history) {
			history = append(history, ChatMessage{
				Role: ChatRoleSystem,
				Content: "[SYSTEM GUARDRAIL] The patient mentioned a service but you do NOT have their name yet. " +
					"NAME is #1 in the Moxie checklist and MUST be collected before anything else. " +
					"You MUST ask for their full name NOW. Do NOT ask about patient type, schedule, provider, or email yet. " +
					"Ask something like: 'Great choice! May I have your full name?'",
			})
		}

		if prefs.ServiceInterest != "" && prefs.Name != "" && prefs.PatientType == "" {
			history = append(history, ChatMessage{
				Role: ChatRoleSystem,
				Content: "[SYSTEM GUARDRAIL] You have the patient's name and service interest. " +
					"Next in the checklist is PATIENT TYPE (#3). " +
					"You MUST ask if they are a new or returning patient NOW. Do NOT ask about schedule, email, or provider yet. " +
					"Ask something like: 'Have you visited us before, or would this be your first time?'",
			})
		}

		if prefs.ServiceInterest != "" && prefs.Name != "" && prefs.PatientType != "" &&
			prefs.PreferredDays == "" && prefs.PreferredTimes == "" {
			history = append(history, ChatMessage{
				Role: ChatRoleSystem,
				Content: "[SYSTEM GUARDRAIL] You have the patient's name, service, and patient type. " +
					"Next in the Moxie checklist is SCHEDULE (#4). " +
					"You MUST ask about their preferred days and times NOW. Do NOT ask for email or provider preference yet.",
			})
		}

		if prefs.ServiceInterest != "" && prefs.ProviderPreference == "" &&
			(prefs.PreferredDays != "" || prefs.PreferredTimes != "") {
			resolvedService := startCfg.ResolveServiceName(prefs.ServiceInterest)
			if startCfg.ServiceNeedsProviderPreference(resolvedService) {
				providerNames := make([]string, 0)
				if startCfg.MoxieConfig != nil {
					for _, name := range startCfg.MoxieConfig.ProviderNames {
						providerNames = append(providerNames, name)
					}
				}
				var providerList string
				if len(providerNames) > 0 {
					providerList = fmt.Sprintf(" Available providers: %s.", strings.Join(providerNames, ", "))
				}
				history = append(history, ChatMessage{
					Role: ChatRoleSystem,
					Content: fmt.Sprintf("[SYSTEM GUARDRAIL] The patient wants %s which has multiple providers.%s "+
						"You MUST ask about provider preference NOW. Do NOT ask for email yet. "+
						"Ask: 'Do you have a provider preference, or would you like the first available appointment?'",
						prefs.ServiceInterest, providerList),
				})
			}
		}
	}

	reply, err := s.generateResponse(ctx, history)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	// Sanitize reply to strip any markdown that slipped through (LLM sometimes ignores instructions)
	reply = sanitizeSMSResponse(reply)
	history = append(history, ChatMessage{
		Role:    ChatRoleAssistant,
		Content: reply,
	})

	history = trimHistory(history, maxHistoryMessages)
	if err := s.history.Save(ctx, conversationID, history); err != nil {
		span.RecordError(err)
		return nil, err
	}

	// Extract and save scheduling preferences from the first message
	if req.LeadID != "" && s.leadsRepo != nil {
		if err := s.extractAndSavePreferences(ctx, req.LeadID, history); err != nil {
			s.logger.Warn("failed to save scheduling preferences from intro", "lead_id", req.LeadID, "error", err)
		}
		if email := ExtractEmailFromHistory(history); email != "" {
			if err := s.leadsRepo.UpdateEmail(ctx, req.LeadID, email); err != nil {
				s.logger.Warn("failed to save email", "lead_id", req.LeadID, "error", err)
			}
		}
	}

	resp := &Response{
		ConversationID: conversationID,
		Message:        reply,
		Timestamp:      time.Now().UTC(),
	}

	// Check if all qualifications are met on the first message — if so, trigger
	// time selection immediately instead of requiring a second message.
	moxieAPIReady := s.moxieClient != nil && startCfg != nil && startCfg.MoxieConfig != nil
	browserReady := s.browser != nil && s.browser.IsConfigured()
	if (moxieAPIReady || browserReady) && usesMoxie && ShouldFetchAvailabilityWithConfig(history, nil, startCfg) {
		prefs, _ := extractPreferences(history, serviceAliasesFromConfig(startCfg))

		// SERVICE VARIANT CHECK — resolve delivery variants (e.g. in-person vs virtual).
		variantResp, variantErr := s.handleVariantResolution(ctx, startCfg, &prefs, history, "", conversationID, req.OrgID)
		if variantErr != nil {
			return nil, variantErr
		}
		if variantResp != nil {
			resp.Message = variantResp.Message
			return resp, nil
		}

		// After variant resolution, check if provider preference is needed.
		if provResp := s.handleProviderPreference(startCfg, &prefs, prefs.ServiceInterest, conversationID); provResp != nil {
			resp.Message = provResp.Message
			return resp, nil
		}

		// Fetch real-time availability and present time slots.
		tsResp := s.fetchAndPresentAvailability(ctx, &prefs, startCfg, startCfg.BookingURL, conversationID, req.OrgID, nil)
		if tsResp != nil && len(tsResp.Slots) > 0 {
			tsResp.SavedToHistory = true
			resp.TimeSelectionResponse = tsResp

			// Replace the LLM reply in history with what we're actually sending
			for i := len(history) - 1; i >= 0; i-- {
				if history[i].Role == ChatRoleAssistant {
					history[i].Content = resp.TimeSelectionResponse.SMSMessage
					break
				}
			}
			if saveErr := s.history.Save(ctx, conversationID, history); saveErr != nil {
				s.logger.Warn("StartConversation: failed to re-save history after time selection", "error", saveErr)
			}
		} else if tsResp != nil && tsResp.SMSMessage != "" {
			resp.TimeSelectionResponse = tsResp
		}
	}

	return resp, nil
}

// ProcessMessage continues an existing conversation with Redis-backed context.
// If the conversation doesn't exist, it automatically starts a new one.
func (s *LLMService) ProcessMessage(ctx context.Context, req MessageRequest) (*Response, error) {
	if strings.TrimSpace(req.ConversationID) == "" {
		return nil, errors.New("conversation: conversationID required")
	}

	rawMessage := req.Message
	redactedMessage, sawPHI := RedactPHI(rawMessage)
	medicalKeywords := []string(nil)
	if !sawPHI {
		medicalKeywords = detectMedicalAdvice(rawMessage)
		if len(medicalKeywords) > 0 {
			redactedMessage = "[REDACTED]"
		}
	}

	s.events.MessageReceived(ctx, req.ConversationID, req.OrgID, req.LeadID, rawMessage)

	// Prompt injection detection — scan inbound messages before they reach the LLM.
	injectionResult := ScanForPromptInjection(rawMessage)
	if injectionResult.Blocked {
		s.events.PromptInjectionDetected(ctx, req.ConversationID, req.OrgID, true, injectionResult.Score, injectionResult.Reasons)
		s.logger.Warn("ProcessMessage: prompt injection BLOCKED",
			"conversation_id", req.ConversationID,
			"org_id", req.OrgID,
			"score", injectionResult.Score,
			"reasons", injectionResult.Reasons,
		)
		if s.audit != nil && strings.TrimSpace(req.OrgID) != "" {
			_ = s.audit.LogPromptInjection(ctx, req.OrgID, req.ConversationID, req.LeadID, injectionResult.Reasons)
		}
		return &Response{ConversationID: req.ConversationID, Message: blockedReply, Timestamp: time.Now().UTC()}, nil
	}
	if injectionResult.Score >= warnThreshold {
		s.logger.Warn("ProcessMessage: prompt injection WARNING",
			"conversation_id", req.ConversationID,
			"org_id", req.OrgID,
			"score", injectionResult.Score,
			"reasons", injectionResult.Reasons,
		)
		rawMessage = SanitizeForLLM(rawMessage)
	}

	s.logger.Info("ProcessMessage called",
		"conversation_id", req.ConversationID,
		"org_id", req.OrgID,
		"lead_id", req.LeadID,
		"message", redactedMessage,
	)

	ctx, span := llmTracer.Start(ctx, "conversation.message")
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
			s.logger.Info("ProcessMessage: conversation not found, starting new",
				"conversation_id", req.ConversationID,
				"message", redactedMessage,
			)
			if sawPHI {
				safeStart := StartRequest{
					OrgID:          req.OrgID,
					ConversationID: req.ConversationID,
					LeadID:         req.LeadID,
					ClinicID:       req.ClinicID,
					Intro:          redactedMessage,
					Channel:        req.Channel,
					From:           req.From,
					To:             req.To,
					Metadata:       req.Metadata,
				}
				// Get clinic-configured deposit amount and booking platform for system prompt
				depositCents := s.deposit.DefaultAmountCents
				var usesMoxiePHI bool
				if s.clinicStore != nil && req.OrgID != "" {
					if cfg, err := s.clinicStore.Get(ctx, req.OrgID); err == nil && cfg != nil {
						if cfg.DepositAmountCents > 0 {
							depositCents = int32(cfg.DepositAmountCents)
						}
						usesMoxiePHI = cfg.UsesMoxieBooking()
					}
				}
				history := []ChatMessage{
					{Role: ChatRoleSystem, Content: buildSystemPrompt(int(depositCents), usesMoxiePHI)},
				}
				history = s.appendContext(ctx, history, req.OrgID, req.LeadID, req.ClinicID, "")
				history = append(history, ChatMessage{
					Role:    ChatRoleUser,
					Content: formatIntroMessage(safeStart, req.ConversationID),
				})
				history = append(history, ChatMessage{
					Role:    ChatRoleAssistant,
					Content: phiDeflectionReply,
				})
				history = trimHistory(history, maxHistoryMessages)
				if err := s.history.Save(ctx, req.ConversationID, history); err != nil {
					span.RecordError(err)
					return nil, err
				}
				if s.audit != nil && strings.TrimSpace(req.OrgID) != "" {
					_ = s.audit.LogPHIDetected(ctx, req.OrgID, req.ConversationID, req.LeadID, rawMessage, "keyword")
				}
				return &Response{ConversationID: req.ConversationID, Message: phiDeflectionReply, Timestamp: time.Now().UTC()}, nil
			}
			if len(medicalKeywords) > 0 {
				safeStart := StartRequest{
					OrgID:          req.OrgID,
					ConversationID: req.ConversationID,
					LeadID:         req.LeadID,
					ClinicID:       req.ClinicID,
					Intro:          "[REDACTED]",
					Channel:        req.Channel,
					From:           req.From,
					To:             req.To,
					Metadata:       req.Metadata,
				}
				// Get clinic-configured deposit amount and booking platform for system prompt
				depositCents := s.deposit.DefaultAmountCents
				var usesMoxieMed bool
				if s.clinicStore != nil && req.OrgID != "" {
					if cfg, err := s.clinicStore.Get(ctx, req.OrgID); err == nil && cfg != nil {
						if cfg.DepositAmountCents > 0 {
							depositCents = int32(cfg.DepositAmountCents)
						}
						usesMoxieMed = cfg.UsesMoxieBooking()
					}
				}
				history := []ChatMessage{
					{Role: ChatRoleSystem, Content: buildSystemPrompt(int(depositCents), usesMoxieMed)},
				}
				history = s.appendContext(ctx, history, req.OrgID, req.LeadID, req.ClinicID, "")
				history = append(history, ChatMessage{
					Role:    ChatRoleUser,
					Content: formatIntroMessage(safeStart, req.ConversationID),
				})
				history = append(history, ChatMessage{
					Role:    ChatRoleAssistant,
					Content: medicalAdviceDeflectionReply,
				})
				history = trimHistory(history, maxHistoryMessages)
				if err := s.history.Save(ctx, req.ConversationID, history); err != nil {
					span.RecordError(err)
					return nil, err
				}
				if s.audit != nil && strings.TrimSpace(req.OrgID) != "" {
					_ = s.audit.LogMedicalAdviceRefused(ctx, req.OrgID, req.ConversationID, req.LeadID, "[REDACTED]", medicalKeywords)
				}
				return &Response{ConversationID: req.ConversationID, Message: medicalAdviceDeflectionReply, Timestamp: time.Now().UTC()}, nil
			}
			return s.StartConversation(ctx, StartRequest{
				OrgID:          req.OrgID,
				ConversationID: req.ConversationID,
				LeadID:         req.LeadID,
				ClinicID:       req.ClinicID,
				Intro:          rawMessage,
				Channel:        req.Channel,
				From:           req.From,
				To:             req.To,
				Metadata:       req.Metadata,
			})
		}
		span.RecordError(err)
		return nil, err
	}

	s.logger.Info("ProcessMessage: history loaded",
		"conversation_id", req.ConversationID,
		"history_length", len(history),
	)

	if sawPHI {
		history = s.appendContext(ctx, history, req.OrgID, req.LeadID, req.ClinicID, "")
		history = append(history, ChatMessage{
			Role:    ChatRoleUser,
			Content: redactedMessage,
		})
		history = append(history, ChatMessage{Role: ChatRoleAssistant, Content: phiDeflectionReply})
		history = trimHistory(history, maxHistoryMessages)
		if err := s.history.Save(ctx, req.ConversationID, history); err != nil {
			span.RecordError(err)
			return nil, err
		}
		if s.audit != nil && strings.TrimSpace(req.OrgID) != "" {
			_ = s.audit.LogPHIDetected(ctx, req.OrgID, req.ConversationID, req.LeadID, rawMessage, "keyword")
		}
		return &Response{ConversationID: req.ConversationID, Message: phiDeflectionReply, Timestamp: time.Now().UTC()}, nil
	}
	if len(medicalKeywords) > 0 {
		history = s.appendContext(ctx, history, req.OrgID, req.LeadID, req.ClinicID, "")
		history = append(history, ChatMessage{
			Role:    ChatRoleUser,
			Content: "[REDACTED]",
		})
		history = append(history, ChatMessage{Role: ChatRoleAssistant, Content: medicalAdviceDeflectionReply})
		history = trimHistory(history, maxHistoryMessages)
		if err := s.history.Save(ctx, req.ConversationID, history); err != nil {
			span.RecordError(err)
			return nil, err
		}
		if s.audit != nil && strings.TrimSpace(req.OrgID) != "" {
			_ = s.audit.LogMedicalAdviceRefused(ctx, req.OrgID, req.ConversationID, req.LeadID, "[REDACTED]", medicalKeywords)
		}
		return &Response{ConversationID: req.ConversationID, Message: medicalAdviceDeflectionReply, Timestamp: time.Now().UTC()}, nil
	}

	history = s.appendContext(ctx, history, req.OrgID, req.LeadID, req.ClinicID, rawMessage)
	history = append(history, ChatMessage{
		Role:    ChatRoleUser,
		Content: rawMessage,
	})

	// Deterministic guardrails (avoid the LLM for sensitive or highly structured requests).
	var cfg *clinic.Config
	if s.clinicStore != nil && req.OrgID != "" {
		if loaded, err := s.clinicStore.Get(ctx, req.OrgID); err == nil {
			cfg = loaded
		}
	}
	if cfg != nil && isPriceInquiry(rawMessage) {
		service := detectServiceKey(rawMessage, cfg)
		if service != "" {
			if price, ok := cfg.PriceTextForService(service); ok {
				depositCents := cfg.DepositAmountForService(service)
				depositDollars := float64(depositCents) / 100.0
				// Use the canonical service name for display but capitalize nicely.
				displayName := strings.Title(service) //nolint:staticcheck
				// Find a matching service in the config list for proper casing.
				for _, svc := range cfg.Services {
					if strings.EqualFold(svc, service) {
						displayName = svc
						break
					}
				}
				reply := fmt.Sprintf("%s pricing: %s. To secure priority booking, we collect a small refundable deposit of $%.0f that applies toward your treatment. Would you like to proceed?", displayName, price, depositDollars)
				// Best-effort tagging for analytics/triage.
				s.appendLeadNote(ctx, req.OrgID, req.LeadID, "tag:price_shopper")

				history = append(history, ChatMessage{Role: ChatRoleAssistant, Content: reply})
				history = trimHistory(history, maxHistoryMessages)
				if err := s.history.Save(ctx, req.ConversationID, history); err != nil {
					span.RecordError(err)
					return nil, err
				}
				s.savePreferencesNoNote(ctx, req.LeadID, history, "price_inquiry")
				return &Response{ConversationID: req.ConversationID, Message: reply, Timestamp: time.Now().UTC()}, nil
			}
		}
	}
	if isQuestionSelection(rawMessage) {
		reply := "Absolutely - what can I help with? If it's about a specific service (Botox, fillers, facials, lasers), let me know which one."

		history = append(history, ChatMessage{Role: ChatRoleAssistant, Content: reply})
		history = trimHistory(history, maxHistoryMessages)
		if err := s.history.Save(ctx, req.ConversationID, history); err != nil {
			span.RecordError(err)
			return nil, err
		}
		s.savePreferencesNoNote(ctx, req.LeadID, history, "question_selection")
		return &Response{ConversationID: req.ConversationID, Message: reply, Timestamp: time.Now().UTC()}, nil
	}
	if isAmbiguousHelp(rawMessage) {
		reply := "Happy to help. Are you looking to book an appointment, or do you have a question about a specific service (Botox, fillers, facials, lasers)?"
		s.appendLeadNote(ctx, req.OrgID, req.LeadID, "state:needs_intent")

		history = append(history, ChatMessage{Role: ChatRoleAssistant, Content: reply})
		history = trimHistory(history, maxHistoryMessages)
		if err := s.history.Save(ctx, req.ConversationID, history); err != nil {
			span.RecordError(err)
			return nil, err
		}
		s.savePreferencesNoNote(ctx, req.LeadID, history, "ambiguous_help")
		return &Response{ConversationID: req.ConversationID, Message: reply, Timestamp: time.Now().UTC()}, nil
	}

	// Use LLM classifier for FAQ responses to common questions
	// Falls back to regex pattern matching if classifier fails
	isComparison := IsServiceComparisonQuestion(rawMessage)
	msgPreview := rawMessage
	if len(msgPreview) > 50 {
		msgPreview = msgPreview[:50] + "..."
	}
	s.logger.Info("FAQ classifier check", "is_comparison_question", isComparison, "message_preview", msgPreview)
	if isComparison {
		var faqReply string
		var faqSource string

		// Try LLM classifier first (more accurate)
		if s.faqClassifier != nil {
			category, classifyErr := s.faqClassifier.ClassifyQuestion(ctx, rawMessage)
			s.logger.Info("FAQ LLM classifier result", "category", category, "error", classifyErr)
			if classifyErr == nil && category != FAQCategoryOther {
				faqReply = GetFAQResponse(category)
				faqSource = "llm_classifier"
			} else if classifyErr != nil {
				s.logger.Warn("FAQ LLM classification failed, trying regex fallback", "error", classifyErr)
			}
		}

		// Fallback to regex pattern matching
		if faqReply == "" {
			if regexReply, found := CheckFAQCache(rawMessage); found {
				faqReply = regexReply
				faqSource = "regex_fallback"
				s.logger.Info("FAQ regex fallback hit", "conversation_id", req.ConversationID)
			}
		}

		// Return cached FAQ response if found
		if faqReply != "" {
			s.logger.Info("FAQ response returned", "source", faqSource, "conversation_id", req.ConversationID)
			history = append(history, ChatMessage{Role: ChatRoleAssistant, Content: faqReply})
			history = trimHistory(history, maxHistoryMessages)
			if err := s.history.Save(ctx, req.ConversationID, history); err != nil {
				span.RecordError(err)
				return nil, err
			}
			s.savePreferencesNoNote(ctx, req.LeadID, history, "faq_response")
			return &Response{ConversationID: req.ConversationID, Message: faqReply, Timestamp: time.Now().UTC()}, nil
		}

		s.logger.Info("FAQ: no match from classifier or regex, falling through to full LLM")
	}

	// Load time selection state BEFORE the LLM call so we can:
	// 1. Inject presented slots into context (prevents LLM from hallucinating times)
	// 2. Detect if the user is selecting a slot (so LLM can confirm)
	timeSelectionState, tsErr := s.history.LoadTimeSelectionState(ctx, req.ConversationID)
	if tsErr != nil {
		s.logger.Warn("failed to load time selection state", "error", tsErr, "conversation_id", req.ConversationID)
	} else {
		s.logger.Info("time selection state loaded",
			"conversation_id", req.ConversationID,
			"state_exists", timeSelectionState != nil,
			"slots_count", func() int {
				if timeSelectionState != nil {
					return len(timeSelectionState.PresentedSlots)
				}
				return 0
			}(),
			"slot_selected", func() bool {
				if timeSelectionState != nil {
					return timeSelectionState.SlotSelected
				}
				return false
			}(),
		)
	}

	// NEW SERVICE DETECTION: If the patient just booked one service (SlotSelected=true)
	// and is now asking about a DIFFERENT service, reset the time selection state so
	// the availability flow triggers fresh for the new service.
	if timeSelectionState != nil && timeSelectionState.SlotSelected {
		// Check if the current message mentions a service different from the booked one.
		// Use extractPreferences on the full history (which now includes the new message)
		// to see if service interest has shifted.
		bookedService := strings.ToLower(strings.TrimSpace(timeSelectionState.Service))
		// Also try detectServiceKey for exact config matches
		newServiceExact := ""
		if cfg != nil {
			newServiceExact = detectServiceKey(rawMessage, cfg)
		}
		// Check if the raw message mentions wanting/booking something (broad intent)
		msgLower := strings.ToLower(rawMessage)
		mentionsNewService := false
		if newServiceExact != "" {
			resolvedNew := strings.ToLower(newServiceExact)
			resolvedOld := bookedService
			if cfg != nil {
				resolvedNew = strings.ToLower(cfg.ResolveServiceName(newServiceExact))
				resolvedOld = strings.ToLower(cfg.ResolveServiceName(bookedService))
			}
			mentionsNewService = resolvedNew != resolvedOld
		}
		// Broader detection: if the message says "I want X too", "also book X", "book X",
		// where X doesn't match the previously booked service name
		if !mentionsNewService {
			newServicePatterns := []string{
				`(?i)(?:i\s+(?:also\s+)?want|(?:also|can\s+i)\s+(?:book|get|schedule)|book\s+(?:me\s+)?(?:for\s+)?|i.+?too$)`,
			}
			for _, pat := range newServicePatterns {
				if re, err := regexp.Compile(pat); err == nil && re.MatchString(msgLower) {
					// The message has booking intent — check if it mentions a different service
					// by seeing if the booked service name is NOT in the message
					if !strings.Contains(msgLower, bookedService) {
						mentionsNewService = true
					}
					break
				}
			}
		}
		if mentionsNewService {
			s.logger.Info("new service detected after previous booking — resetting time selection state",
				"conversation_id", req.ConversationID,
				"old_service", timeSelectionState.Service,
				"message", rawMessage,
			)
			if err := s.history.ClearTimeSelectionState(ctx, req.ConversationID); err != nil {
				s.logger.Warn("failed to clear time selection state for new service", "error", err)
			}
			timeSelectionState = nil
			if req.LeadID != "" && s.leadsRepo != nil {
				if uerr := s.leadsRepo.ClearSelectedAppointment(ctx, req.LeadID); uerr != nil {
					s.logger.Warn("failed to clear selected appointment for new service", "error", uerr)
				}
			}
		}
	}

	// Handle time selection if user is in that flow
	var timeSelectionResponse *TimeSelectionResponse
	var selectedSlot *PresentedSlot
	if timeSelectionState != nil && len(timeSelectionState.PresentedSlots) > 0 {
		// Build time preferences from conversation for disambiguation
		selectionPrefs := TimePreferences{}
		if convPrefs, ok := extractPreferences(history, serviceAliasesFromConfig(cfg)); ok {
			selectionPrefs = ExtractTimePreferences(convPrefs.PreferredDays + " " + convPrefs.PreferredTimes)
		}
		// User may be selecting a time slot
		selectedSlot = DetectTimeSelection(rawMessage, timeSelectionState.PresentedSlots, selectionPrefs)
		if selectedSlot != nil {
			s.events.TimeSlotSelected(ctx, req.ConversationID, req.OrgID, selectedSlot.DateTime.Format(time.RFC3339), selectedSlot.Index)
			s.logger.Info("time slot selected",
				"slot_index", selectedSlot.Index,
				"time", selectedSlot.DateTime,
				"service", timeSelectionState.Service,
			)

			// Store the selected appointment in the lead
			if req.LeadID != "" && s.leadsRepo != nil {
				if err := s.leadsRepo.UpdateSelectedAppointment(ctx, req.LeadID, leads.SelectedAppointment{
					DateTime: &selectedSlot.DateTime,
					Service:  timeSelectionState.Service,
				}); err != nil {
					s.logger.Warn("failed to save selected appointment", "lead_id", req.LeadID, "error", err)
				}
			}

			// Mark slot as selected (don't clear to nil — that would re-trigger scraping)
			timeSelectionState.SlotSelected = true
			timeSelectionState.PresentedSlots = nil // Clear slots so we don't re-present them
			if err := s.history.SaveTimeSelectionState(ctx, req.ConversationID, timeSelectionState); err != nil {
				s.logger.Warn("failed to save time selection completion state", "error", err)
			}

			// Inject slot selection into history so LLM generates an appropriate confirmation
			history = append(history, ChatMessage{
				Role:    ChatRoleSystem,
				Content: fmt.Sprintf("[SYSTEM] The patient selected time slot #%d: %s for %s. Confirm their selection and proceed with booking.", selectedSlot.Index, selectedSlot.TimeStr, timeSelectionState.Service),
			})
		} else if isMoreTimesRequest(strings.ToLower(rawMessage)) {
			// Patient wants more/different/later times — re-fetch with refined preferences
			s.logger.Info("patient requesting more times",
				"conversation_id", req.ConversationID,
				"message", rawMessage,
			)

			// Try to re-fetch with the patient's refined request
			moreTimesHandled := false
			if (s.moxieClient != nil && cfg != nil && cfg.MoxieConfig != nil) ||
				(s.browser != nil && s.browser.IsConfigured()) {

				prefs, _ := extractPreferences(history, serviceAliasesFromConfig(cfg))
				service := timeSelectionState.Service
				scraperServiceName := service
				if cfg != nil {
					scraperServiceName = cfg.ResolveServiceName(scraperServiceName)
				}

				// Build refined time preferences from the patient's "more times" message
				refinedPrefs := buildRefinedTimePreferences(rawMessage, prefs, timeSelectionState.PresentedSlots)

				s.logger.Info("re-fetching availability with refined preferences",
					"conversation_id", req.ConversationID,
					"original_after", ExtractTimePreferences(prefs.PreferredDays+" "+prefs.PreferredTimes).AfterTime,
					"refined_after", refinedPrefs.AfterTime,
					"refined_days", refinedPrefs.DaysOfWeek,
					"excluded_times", len(timeSelectionState.PresentedSlots),
				)

				fetchCtx, fetchCancel := context.WithTimeout(ctx, 120*time.Second)
				var result *AvailabilityResult
				var fetchErr error

				if s.moxieClient != nil && cfg != nil && cfg.MoxieConfig != nil {
					result, fetchErr = FetchAvailableTimesFromMoxieAPIWithProvider(fetchCtx, s.moxieClient, cfg,
						scraperServiceName, prefs.ProviderPreference, refinedPrefs, req.OnProgress, service)
				} else if cfg != nil {
					result, fetchErr = FetchAvailableTimesWithFallback(fetchCtx, s.browser,
						cfg.BookingURL, scraperServiceName, refinedPrefs, req.OnProgress, service)
				}
				fetchCancel()

				if fetchErr == nil && result != nil {
					// Filter out slots that were already presented
					newSlots := filterOutPreviousSlots(result.Slots, timeSelectionState.PresentedSlots)

					if len(newSlots) > 0 {
						// Re-index
						for i := range newSlots {
							newSlots[i].Index = i + 1
						}
						// Save new time selection state
						state := &TimeSelectionState{
							PresentedSlots: newSlots,
							Service:        service,
							BookingURL:     timeSelectionState.BookingURL,
							PresentedAt:    time.Now(),
						}
						if err := s.history.SaveTimeSelectionState(ctx, req.ConversationID, state); err != nil {
							s.logger.Error("failed to save refined time selection state", "error", err)
						}
						timeSelectionResponse = &TimeSelectionResponse{
							Slots:      newSlots,
							Service:    service,
							ExactMatch: true,
							SMSMessage: FormatTimeSlotsForSMS(newSlots, service, true),
						}
						moreTimesHandled = true
					} else {
						// No new slots — tell the patient
						timeSelectionResponse = &TimeSelectionResponse{
							Slots:      nil,
							Service:    service,
							ExactMatch: false,
							SMSMessage: fmt.Sprintf("Those are the latest available times on those days for %s. Would you like to try different days, or would one of the times I showed work for you?", service),
						}
						moreTimesHandled = true
					}
				}
			}

			if !moreTimesHandled {
				// Fallback: clear state so the normal re-fetch triggers below
				timeSelectionState = nil
				if err := s.history.SaveTimeSelectionState(ctx, req.ConversationID, nil); err != nil {
					s.logger.Warn("failed to clear time selection state", "error", err)
				}
			}
		} else {
			// User sent a message but didn't select a slot — inject the presented slots
			// so the LLM knows what real times are available and doesn't hallucinate
			var slotList strings.Builder
			for _, slot := range timeSelectionState.PresentedSlots {
				slotList.WriteString(fmt.Sprintf("  %d. %s\n", slot.Index, slot.TimeStr))
			}
			history = append(history, ChatMessage{
				Role: ChatRoleSystem,
				Content: fmt.Sprintf("[SYSTEM] The following REAL appointment times for %s were already presented to the patient:\n%s"+
					"ONLY reference these times. Do NOT invent, guess, or fabricate any other times. "+
					"If the patient wants different times, offer to check again with different preferences.",
					timeSelectionState.Service, slotList.String()),
			})
		}
	}

	// Deterministic guardrails: enforce Moxie qualification order.
	// Order: name → service → patient type → schedule → provider → email
	if cfg != nil && cfg.UsesMoxieBooking() {
		prefs, _ := extractPreferences(history, serviceAliasesFromConfig(cfg))

		// Name guardrail: if we have service but no name, ask for name FIRST.
		// Name is #1 in the qualification checklist.
		// Skip if the last assistant message already asked for the name (avoid duplicate asks).
		if prefs.ServiceInterest != "" && prefs.Name == "" && !lastAssistantAskedForName(history) {
			history = append(history, ChatMessage{
				Role: ChatRoleSystem,
				Content: "[SYSTEM GUARDRAIL] The patient mentioned a service but you do NOT have their name yet. " +
					"NAME is #1 in the Moxie checklist and MUST be collected before anything else. " +
					"You MUST ask for their full name NOW. Do NOT ask about patient type, schedule, provider, or email yet. " +
					"Ask something like: 'Great choice! May I have your full name?'",
			})
		}

		// Patient type guardrail: if we have name + service but no patient type,
		// force the LLM to ask about patient type before schedule/email.
		if prefs.ServiceInterest != "" && prefs.Name != "" && prefs.PatientType == "" {
			history = append(history, ChatMessage{
				Role: ChatRoleSystem,
				Content: "[SYSTEM GUARDRAIL] You have the patient's name and service interest. " +
					"Next in the checklist is PATIENT TYPE (#3). " +
					"You MUST ask if they are a new or returning patient NOW. Do NOT ask about schedule, email, or provider yet. " +
					"Ask something like: 'Have you visited us before, or would this be your first time?'",
			})
		}

		// Schedule guardrail: if we have name + service + patient type but no schedule,
		// force the LLM to ask about schedule before anything else.
		if prefs.ServiceInterest != "" && prefs.Name != "" && prefs.PatientType != "" &&
			prefs.PreferredDays == "" && prefs.PreferredTimes == "" {
			history = append(history, ChatMessage{
				Role: ChatRoleSystem,
				Content: "[SYSTEM GUARDRAIL] You have the patient's name, service, and patient type. " +
					"Next in the Moxie checklist is SCHEDULE (#4). " +
					"You MUST ask about their preferred days and times NOW. Do NOT ask for email or provider preference yet. " +
					"Ask something like: 'What days and times work best for you?'",
			})
		}

		// Provider preference guardrail: if service needs it and we don't have it.
		if prefs.ServiceInterest != "" && prefs.ProviderPreference == "" &&
			(prefs.PreferredDays != "" || prefs.PreferredTimes != "") {
			resolvedService := cfg.ResolveServiceName(prefs.ServiceInterest)
			if cfg.ServiceNeedsProviderPreference(resolvedService) {
				providerNames := make([]string, 0)
				if cfg.MoxieConfig != nil {
					for _, name := range cfg.MoxieConfig.ProviderNames {
						providerNames = append(providerNames, name)
					}
				}
				var providerList string
				if len(providerNames) > 0 {
					providerList = fmt.Sprintf(" Available providers: %s.", strings.Join(providerNames, ", "))
				}
				history = append(history, ChatMessage{
					Role: ChatRoleSystem,
					Content: fmt.Sprintf("[SYSTEM GUARDRAIL] The patient wants %s which has multiple providers.%s "+
						"You MUST ask about provider preference NOW. Do NOT ask for email yet. "+
						"Ask: 'Do you have a provider preference, or would you like the first available appointment?'",
						prefs.ServiceInterest, providerList),
				})
			}
		}
	}

	reply, err := s.generateResponse(ctx, history)
	if err != nil {
		return nil, err
	}
	// Sanitize reply to strip any markdown that slipped through (LLM sometimes ignores instructions)
	reply = sanitizeSMSResponse(reply)
	history = append(history, ChatMessage{
		Role:    ChatRoleAssistant,
		Content: reply,
	})

	history = trimHistory(history, maxHistoryMessages)
	if err := s.history.Save(ctx, req.ConversationID, history); err != nil {
		span.RecordError(err)
		return nil, err
	}

	depositIntent := s.handleDepositFlow(ctx, history)

	// Extract and save scheduling preferences if lead ID is provided
	if req.LeadID != "" && s.leadsRepo != nil {
		if err := s.extractAndSavePreferences(ctx, req.LeadID, history); err != nil {
			s.logger.Warn("failed to save scheduling preferences", "lead_id", req.LeadID, "error", err)
		}
		if email := ExtractEmailFromHistory(history); email != "" {
			if err := s.leadsRepo.UpdateEmail(ctx, req.LeadID, email); err != nil {
				s.logger.Warn("failed to save email", "lead_id", req.LeadID, "error", err)
			}
		}
	}

	// Check if clinic uses Moxie booking (determines flow: Moxie handoff vs Square deposit)
	var usesMoxie bool
	var clinicCfg *clinic.Config
	if s.clinicStore != nil && req.OrgID != "" {
		if cfg, err := s.clinicStore.Get(ctx, req.OrgID); err == nil && cfg != nil {
			clinicCfg = cfg
			usesMoxie = cfg.UsesMoxieBooking()
		}
	}

	// Enforce clinic-configured deposit amounts for Square clinics
	if depositIntent != nil && clinicCfg != nil && !usesMoxie {
		if prefs, ok := extractPreferences(history, serviceAliasesFromConfig(clinicCfg)); ok && prefs.ServiceInterest != "" {
			if amount := clinicCfg.DepositAmountForService(prefs.ServiceInterest); amount > 0 {
				depositIntent.AmountCents = int32(amount)
			}
		}
	}

	// GUARD: For Moxie clinics, NEVER send a deposit without time selection first.
	// The correct flow is: qualify → present slots → patient picks → THEN deposit.
	// If the deposit classifier fires before time selection, suppress it.
	if depositIntent != nil && usesMoxie && (timeSelectionState == nil || !timeSelectionState.SlotSelected) {
		s.logger.Warn("deposit intent suppressed: Moxie clinic requires time selection before deposit",
			"conversation_id", req.ConversationID,
			"slot_selected", timeSelectionState != nil && timeSelectionState.SlotSelected,
		)
		depositIntent = nil
	}

	// Check if we should trigger time selection flow
	// For Moxie: trigger when qualifications are met (no deposit intent needed)
	// For Square: trigger when deposit intent exists AND qualifications are met
	browserReady := s.browser != nil && s.browser.IsConfigured()
	moxieAPIReady := s.moxieClient != nil && clinicCfg != nil && clinicCfg.MoxieConfig != nil
	qualificationsMet := ShouldFetchAvailabilityWithConfig(history, nil, clinicCfg)
	shouldTriggerTimeSelection := (browserReady || moxieAPIReady) && timeSelectionState == nil
	// Don't re-scrape if a slot was already selected (patient is now providing email, etc.)
	if timeSelectionState != nil && timeSelectionState.SlotSelected {
		shouldTriggerTimeSelection = false
	}
	if usesMoxie {
		// Moxie clinics: trigger time selection when lead is qualified (deposit flows through Moxie)
		shouldTriggerTimeSelection = shouldTriggerTimeSelection && qualificationsMet
	} else {
		// Square clinics: trigger time selection only when deposit intent exists
		shouldTriggerTimeSelection = shouldTriggerTimeSelection && depositIntent != nil && qualificationsMet
	}

	s.logger.Info("time selection trigger check",
		"conversation_id", req.ConversationID,
		"browser_ready", browserReady,
		"qualifications_met", qualificationsMet,
		"time_selection_state_exists", timeSelectionState != nil,
		"uses_moxie", usesMoxie,
		"should_trigger", shouldTriggerTimeSelection,
	)

	if shouldTriggerTimeSelection {
		// Get booking URL from clinic config
		var bookingURL string
		if clinicCfg != nil {
			bookingURL = clinicCfg.BookingURL
		}

		if bookingURL != "" {
			// Extract service and time preferences
			prefs, _ := extractPreferences(history, serviceAliasesFromConfig(clinicCfg))

			// SERVICE VARIANT CHECK: resolve delivery variants (e.g. in-person vs virtual).
			variantResp, variantErr := s.handleVariantResolution(ctx, clinicCfg, &prefs, history, rawMessage, req.ConversationID, req.OrgID)
			if variantErr != nil {
				return nil, variantErr
			}
			if variantResp != nil {
				return variantResp, nil
			}

			// After variant resolution, check if provider preference is needed.
			if provResp := s.handleProviderPreference(clinicCfg, &prefs, prefs.ServiceInterest, req.ConversationID); provResp != nil {
				return provResp, nil
			}

			// Fetch real-time availability and present time slots.
			timeSelectionResponse = s.fetchAndPresentAvailability(ctx, &prefs, clinicCfg, bookingURL, req.ConversationID, req.OrgID, req.OnProgress)
			if timeSelectionResponse != nil && len(timeSelectionResponse.Slots) > 0 {
				// Clear deposit intent - time selection must happen first
				depositIntent = nil
			}
		}
	}

	// When time selection takes over, replace the LLM reply in history.
	// The LLM reply was saved before we knew time selection would trigger.
	// Without this fix, the stale LLM reply sits in history and confuses the
	// LLM on the next turn (it doesn't know about the time options that were sent).
	if timeSelectionResponse != nil && timeSelectionResponse.SMSMessage != "" {
		// Find and replace the last assistant message (the LLM reply) with
		// a note that time options were presented instead
		for i := len(history) - 1; i >= 0; i-- {
			if history[i].Role == ChatRoleAssistant {
				history[i].Content = timeSelectionResponse.SMSMessage
				break
			}
		}
		if err := s.history.Save(ctx, req.ConversationID, history); err != nil {
			s.logger.Warn("failed to re-save history after time selection", "error", err)
		}
		timeSelectionResponse.SavedToHistory = true
	}

	// For Moxie clinics: always clear Square deposit intent.
	// Moxie clinics never use Square — the patient pays directly on Moxie's
	// Step 5 payment page after the sidecar auto-fills Steps 1-4.
	if usesMoxie && depositIntent != nil {
		s.logger.Info("clinic uses Moxie booking - skipping Square deposit intent", "org_id", req.OrgID)
		depositIntent = nil
	}

	// For Moxie clinics: build a BookingRequest when we have a selected slot + email.
	// The slot may have been selected on a PREVIOUS turn (stored on the lead),
	// with email arriving on this turn.
	var bookingRequest *BookingRequest

	// Check if we have a previously selected slot on the lead (from a prior turn)
	var previouslySelectedDateTime *time.Time
	var previouslySelectedService string
	if usesMoxie && selectedSlot == nil && timeSelectionState != nil && timeSelectionState.SlotSelected && req.LeadID != "" && s.leadsRepo != nil {
		if lead, err := s.leadsRepo.GetByID(ctx, req.OrgID, req.LeadID); err == nil && lead != nil && lead.SelectedDateTime != nil {
			// Convert from UTC (as stored in DB) to clinic timezone for correct formatting
			dt := *lead.SelectedDateTime
			if clinicCfg != nil && clinicCfg.Timezone != "" {
				if loc, lerr := time.LoadLocation(clinicCfg.Timezone); lerr == nil {
					dt = dt.In(loc)
				}
			}
			previouslySelectedDateTime = &dt
			previouslySelectedService = lead.SelectedService
			s.logger.Info("found previously selected slot on lead",
				"lead_id", req.LeadID,
				"date_time", lead.SelectedDateTime,
				"service", lead.SelectedService,
			)
		}
	}

	if usesMoxie && (selectedSlot != nil || previouslySelectedDateTime != nil) && clinicCfg != nil && clinicCfg.BookingURL != "" {
		firstName, lastName := splitName("")
		phone := req.From
		email := ""

		// Fetch lead details for name/email
		if req.LeadID != "" && s.leadsRepo != nil {
			if lead, err := s.leadsRepo.GetByID(ctx, req.OrgID, req.LeadID); err == nil && lead != nil {
				firstName, lastName = splitName(lead.Name)
				if lead.Phone != "" {
					phone = lead.Phone
				}
				email = lead.Email
			}
		}

		// Fallback: extract email from conversation history if not on the lead
		if email == "" {
			email = ExtractEmailFromHistory(history)
		}

		if email == "" {
			s.logger.Warn("booking blocked: no email for Moxie booking", "lead_id", req.LeadID)
			// Override the LLM reply to ask for email — the LLM already generated a reply
			// assuming the booking would proceed, but we can't book without email.
			if selectedSlot != nil {
				// Slot was selected THIS turn — override the reply
				slotTime := selectedSlot.DateTime
				reply = fmt.Sprintf("Great choice! I've got %s for %s. To complete your booking, I just need your email address. What's the best email for you?",
					slotTime.Format("Monday, January 2 at 3:04 PM"), timeSelectionState.Service)
				// Update the last assistant message in history with the overridden reply
				for i := len(history) - 1; i >= 0; i-- {
					if history[i].Role == ChatRoleAssistant {
						history[i].Content = reply
						break
					}
				}
				if err := s.history.Save(ctx, req.ConversationID, history); err != nil {
					s.logger.Warn("failed to save history after email request override", "error", err)
				}
			}
			// If previouslySelectedDateTime, the LLM should already be asking for email
			// based on conversation history context
		} else {
			// Format date and time from the selected slot (current turn or previous turn)
			var slotDateTime time.Time
			var slotService string
			if selectedSlot != nil {
				slotDateTime = selectedSlot.DateTime
				if timeSelectionState != nil {
					slotService = timeSelectionState.Service
				}
			} else if previouslySelectedDateTime != nil {
				slotDateTime = *previouslySelectedDateTime
				slotService = previouslySelectedService
			}
			dateStr := slotDateTime.Format("2006-01-02")
			timeStr := strings.ToLower(slotDateTime.Format("3:04pm"))

			// Build callback URL
			var callbackURL string
			if s.apiBaseURL != "" {
				callbackURL = fmt.Sprintf("%s/webhooks/booking/callback?orgId=%s&from=%s",
					strings.TrimRight(s.apiBaseURL, "/"), req.OrgID, req.From)
			}

			bookingRequest = &BookingRequest{
				BookingURL:  clinicCfg.BookingURL,
				Date:        dateStr,
				Time:        timeStr,
				Service:     slotService,
				LeadID:      req.LeadID,
				OrgID:       req.OrgID,
				FirstName:   firstName,
				LastName:    lastName,
				Phone:       phone,
				Email:       email,
				CallbackURL: callbackURL,
			}
			s.logger.Info("booking request prepared for Moxie",
				"booking_url", clinicCfg.BookingURL,
				"date", dateStr,
				"time", timeStr,
				"lead_id", req.LeadID,
			)
		}
	}

	return &Response{
		ConversationID:        req.ConversationID,
		Message:               reply,
		Timestamp:             time.Now().UTC(),
		DepositIntent:         depositIntent,
		TimeSelectionResponse: timeSelectionResponse,
		BookingRequest:        bookingRequest,
	}, nil
}

func shouldAttemptDepositClassification(history []ChatMessage) bool {
	checked := 0
	for i := len(history) - 1; i >= 0 && checked < 8; i-- {
		if history[i].Role == ChatRoleSystem {
			continue
		}
		msg := strings.TrimSpace(history[i].Content)
		if msg == "" {
			continue
		}
		if depositKeywordRE.MatchString(msg) || depositAskRE.MatchString(msg) {
			return true
		}
		checked++
	}
	return false
}

// GetHistory retrieves the conversation history for a given conversation ID.
func (s *LLMService) GetHistory(ctx context.Context, conversationID string) ([]Message, error) {
	history, err := s.history.Load(ctx, conversationID)
	if err != nil {
		return nil, err
	}

	// Convert chat messages to our Message type, filtering out system messages.
	var messages []Message
	for _, msg := range history {
		if msg.Role == ChatRoleSystem {
			continue // Don't expose system prompts
		}
		messages = append(messages, Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}
	return messages, nil
}

func (s *LLMService) generateResponse(ctx context.Context, history []ChatMessage) (string, error) {
	ctx, span := llmTracer.Start(ctx, "conversation.llm")
	defer span.End()

	trimmed := trimHistory(history, maxHistoryMessages)
	system, messages := splitSystemAndMessages(trimmed)

	req := LLMRequest{
		Model:       s.model,
		System:      system,
		Messages:    messages,
		MaxTokens:   450,
		Temperature: 0.2,
	}
	callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := s.client.Complete(callCtx, req)
	latency := time.Since(start)
	status := "ok"
	if err != nil {
		status = "error"
	}
	llmLatency.WithLabelValues(s.model, status).Observe(latency.Seconds())
	if span.IsRecording() {
		span.SetAttributes(
			attribute.Float64("medspa.llm.latency_ms", float64(latency.Milliseconds())),
			attribute.String("medspa.llm.model", s.model),
			attribute.Int("medspa.llm.input_tokens", int(resp.Usage.InputTokens)),
			attribute.Int("medspa.llm.output_tokens", int(resp.Usage.OutputTokens)),
			attribute.Int("medspa.llm.total_tokens", int(resp.Usage.TotalTokens)),
			attribute.String("medspa.llm.stop_reason", resp.StopReason),
		)
	}
	if err != nil {
		span.RecordError(err)
		s.logger.Warn("llm completion failed", "model", s.model, "latency_ms", latency.Milliseconds(), "error", err)
		return "", fmt.Errorf("conversation: llm completion failed: %w", err)
	}
	if resp.Usage.InputTokens > 0 {
		llmTokensTotal.WithLabelValues(s.model, "input").Add(float64(resp.Usage.InputTokens))
	}
	if resp.Usage.OutputTokens > 0 {
		llmTokensTotal.WithLabelValues(s.model, "output").Add(float64(resp.Usage.OutputTokens))
	}
	if resp.Usage.TotalTokens > 0 {
		llmTokensTotal.WithLabelValues(s.model, "total").Add(float64(resp.Usage.TotalTokens))
	}

	text := strings.TrimSpace(resp.Text)
	s.logger.Info("llm completion finished",
		"model", s.model,
		"latency_ms", latency.Milliseconds(),
		"input_tokens", resp.Usage.InputTokens,
		"output_tokens", resp.Usage.OutputTokens,
		"total_tokens", resp.Usage.TotalTokens,
		"stop_reason", resp.StopReason,
	)
	if text == "" {
		err := errors.New("conversation: llm returned empty response")
		span.RecordError(err)
		return "", err
	}
	return text, nil
}

func splitSystemAndMessages(history []ChatMessage) ([]string, []ChatMessage) {
	if len(history) == 0 {
		return nil, nil
	}
	system := make([]string, 0, 4)
	messages := make([]ChatMessage, 0, len(history))
	for _, msg := range history {
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		if msg.Role == ChatRoleSystem {
			system = append(system, msg.Content)
			continue
		}
		messages = append(messages, msg)
	}
	return system, messages
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

func (s *LLMService) appendContext(ctx context.Context, history []ChatMessage, orgID, leadID, clinicID, query string) []ChatMessage {
	// Append payment status context if available
	depositContextInjected := false
	if s.paymentChecker != nil && orgID != "" && leadID != "" {
		orgUUID, orgErr := uuid.Parse(orgID)
		leadUUID, leadErr := uuid.Parse(leadID)
		if orgErr == nil && leadErr == nil {
			type openDepositStatusChecker interface {
				OpenDepositStatus(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID) (string, error)
			}
			if statusChecker, ok := s.paymentChecker.(openDepositStatusChecker); ok {
				status, err := statusChecker.OpenDepositStatus(ctx, orgUUID, leadUUID)
				if err != nil {
					s.logger.Warn("failed to check payment status", "org_id", orgID, "lead_id", leadID, "error", err)
				} else if strings.TrimSpace(status) != "" {
					content := "IMPORTANT: This patient has an existing deposit in progress. Do NOT offer another deposit. Do NOT restart intake or offer to schedule a consultation again. Answer their questions normally and defer personalized/medical advice to the practitioner during their consultation."
					switch status {
					case "succeeded":
						content = "IMPORTANT: This patient has ALREADY PAID their deposit. The platform already sent a payment confirmation SMS automatically when the payment succeeded. Do NOT offer another deposit. Do NOT restart intake or offer to schedule a consultation again. Do NOT repeat the payment confirmation message. Answer their questions normally and defer personalized/medical advice to the practitioner during their consultation. If they ask about next steps: \"Our team will call you within 24 hours to confirm a specific date and time that works for you.\""
					case "deposit_pending":
						content = "IMPORTANT: This patient was already sent a deposit payment link and it is still pending. Do NOT offer another deposit or claim the deposit is already received. Do NOT restart intake or offer to schedule a consultation again. Answer their questions normally and defer personalized/medical advice to the practitioner during their consultation. If they ask about payment, tell them to use the deposit link they received."
					}
					history = append(history, ChatMessage{
						Role:    ChatRoleSystem,
						Content: content,
					})
					depositContextInjected = true
				}
			} else {
				hasDeposit, err := s.paymentChecker.HasOpenDeposit(ctx, orgUUID, leadUUID)
				if err != nil {
					s.logger.Warn("failed to check payment status", "org_id", orgID, "lead_id", leadID, "error", err)
				} else if hasDeposit {
					history = append(history, ChatMessage{
						Role:    ChatRoleSystem,
						Content: "IMPORTANT: This patient has an existing deposit in progress (pending payment or already paid). Do NOT offer another deposit. Do NOT restart intake or offer to schedule a consultation again. Do NOT repeat any payment confirmation message. Answer their questions normally and defer personalized/medical advice to the practitioner during their consultation. If they ask about next steps: \"Our team will call you within 24 hours to confirm a specific date and time that works for you.\"",
					})
					depositContextInjected = true
				}
			}
		}
	}

	// If the payment checker is unavailable (or hasn't persisted yet) but the conversation indicates
	// the patient already agreed to a deposit, inject guardrails so we don't restart intake.
	if !depositContextInjected && conversationHasDepositAgreement(history) {
		history = append(history, ChatMessage{
			Role:    ChatRoleSystem,
			Content: "IMPORTANT: This patient already agreed to the deposit and is in the booking flow. Do NOT restart intake or offer to schedule a consultation again. Answer their questions normally and defer personalized/medical advice to the practitioner during their consultation.",
		})
	}

	// Append lead preferences so the assistant doesn't re-ask for captured info.
	if s.leadsRepo != nil && orgID != "" && leadID != "" {
		lead, err := s.leadsRepo.GetByID(ctx, orgID, leadID)
		if err != nil {
			if !errors.Is(err, leads.ErrLeadNotFound) {
				s.logger.Warn("failed to fetch lead preferences", "org_id", orgID, "lead_id", leadID, "error", err)
			}
		} else if lead != nil {
			if content := formatLeadPreferenceContext(lead); content != "" {
				history = append(history, ChatMessage{
					Role:    ChatRoleSystem,
					Content: content,
				})
			}
		}
	}

	// Append clinic business hours context and deposit amount if available
	if s.clinicStore != nil && orgID != "" {
		cfg, err := s.clinicStore.Get(ctx, orgID)
		if err != nil {
			s.logger.Warn("failed to fetch clinic config", "org_id", orgID, "error", err)
		} else if cfg != nil {
			hoursContext := cfg.BusinessHoursContext(time.Now())
			history = append(history, ChatMessage{
				Role:    ChatRoleSystem,
				Content: hoursContext,
			})
			// Explicitly state the exact deposit amount to prevent LLM from guessing ranges
			depositAmount := cfg.DepositAmountCents
			if depositAmount <= 0 {
				depositAmount = 5000 // default $50
			}
			depositDollars := depositAmount / 100
			history = append(history, ChatMessage{
				Role:    ChatRoleSystem,
				Content: fmt.Sprintf("DEPOSIT AMOUNT: This clinic's deposit is exactly $%d. NEVER say a range like '$50-100'. Always state the exact amount: $%d.", depositDollars, depositDollars),
			})
			// Add AI persona context for personalized voice
			if personaContext := cfg.AIPersonaContext(); personaContext != "" {
				history = append(history, ChatMessage{
					Role:    ChatRoleSystem,
					Content: personaContext,
				})
			}
			if highlightContext := buildServiceHighlightsContext(cfg, query); highlightContext != "" {
				history = append(history, ChatMessage{
					Role:    ChatRoleSystem,
					Content: highlightContext,
				})
			}
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
			history = append(history, ChatMessage{
				Role:    ChatRoleSystem,
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
			availabilityContext := FormatSlotsForLLM(slots, 5)
			history = append(history, ChatMessage{
				Role:    ChatRoleSystem,
				Content: "Real-time appointment availability from clinic calendar:\n" + availabilityContext,
			})
		}
	} else if s.browser != nil && s.browser.IsConfigured() && containsBookingIntent(query) {
		// Fall back to browser scraping if EMR is not configured but browser adapter is
		if s.clinicStore != nil {
			cfg, err := s.clinicStore.Get(ctx, orgID)
			if err == nil && cfg != nil && cfg.BookingURL != "" {
				slots, err := s.browser.GetUpcomingAvailability(ctx, cfg.BookingURL, 7)
				if err != nil {
					s.logger.Warn("failed to fetch browser availability", "error", err, "url", cfg.BookingURL)
				} else if len(slots) > 0 {
					availabilityContext := FormatSlotsForLLM(slots, 5)
					history = append(history, ChatMessage{
						Role:    ChatRoleSystem,
						Content: "Real-time appointment availability from booking page:\n" + availabilityContext,
					})
				}
			}
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

func trimHistory(history []ChatMessage, limit int) []ChatMessage {
	if limit <= 0 || len(history) <= limit {
		return history
	}
	if len(history) == 0 {
		return history
	}

	var result []ChatMessage
	system := history[0]
	if system.Role == ChatRoleSystem {
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

// sanitizeSMSResponse strips markdown formatting that doesn't render in SMS.
// This includes **bold**, *italics*, bullet points, and other markdown syntax.
func sanitizeSMSResponse(msg string) string {
	// Remove bold markers **text** -> text
	msg = strings.ReplaceAll(msg, "**", "")
	// Remove italic markers *text* -> text (be careful not to remove asterisks in lists)
	// Only remove single asterisks that are likely italics (surrounded by non-space)
	msg = smsItalicRE.ReplaceAllString(msg, "$1")
	// Remove markdown bullet points at start of lines: "- item" -> "item"
	msg = smsBulletRE.ReplaceAllString(msg, "")
	// Remove numbered list formatting: "1. item" -> "item"
	msg = smsNumberedRE.ReplaceAllString(msg, "")
	// Clean up any double spaces that might result
	msg = smsMultiSpaceRE.ReplaceAllString(msg, " ")
	return strings.TrimSpace(msg)
}

func (s *LLMService) extractDepositIntent(ctx context.Context, history []ChatMessage) (*DepositIntent, error) {
	ctx, span := llmTracer.Start(ctx, "conversation.deposit_intent")
	defer span.End()

	outcome := "skip"
	var raw string
	defer func() {
		depositDecisionTotal.WithLabelValues(s.model, outcome).Inc()
	}()

	// Focus on the most recent turns to keep the prompt small.
	transcript := summarizeHistory(history, 8)
	systemPrompt := fmt.Sprintf(`You are a decision agent for MedSpa AI. Analyze a conversation and decide if we should send a payment link to collect a deposit.

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
`, s.deposit.DefaultAmountCents)

	callCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := s.client.Complete(callCtx, LLMRequest{
		Model:  s.model,
		System: []string{systemPrompt},
		Messages: []ChatMessage{
			{Role: ChatRoleUser, Content: "Conversation:\n" + transcript},
		},
		MaxTokens:   256,
		Temperature: 0,
	})
	latency := time.Since(start)
	status := "ok"
	if err != nil {
		status = "error"
	}
	llmLatency.WithLabelValues(s.model, status).Observe(latency.Seconds())
	if resp.Usage.InputTokens > 0 {
		llmTokensTotal.WithLabelValues(s.model, "input").Add(float64(resp.Usage.InputTokens))
	}
	if resp.Usage.OutputTokens > 0 {
		llmTokensTotal.WithLabelValues(s.model, "output").Add(float64(resp.Usage.OutputTokens))
	}
	if resp.Usage.TotalTokens > 0 {
		llmTokensTotal.WithLabelValues(s.model, "total").Add(float64(resp.Usage.TotalTokens))
	}
	if span.IsRecording() {
		span.SetAttributes(
			attribute.String("medspa.llm.purpose", "deposit_classifier"),
			attribute.Float64("medspa.llm.latency_ms", float64(latency.Milliseconds())),
			attribute.Int("medspa.llm.input_tokens", int(resp.Usage.InputTokens)),
			attribute.Int("medspa.llm.output_tokens", int(resp.Usage.OutputTokens)),
			attribute.Int("medspa.llm.total_tokens", int(resp.Usage.TotalTokens)),
			attribute.String("medspa.llm.stop_reason", resp.StopReason),
		)
	}
	if err != nil {
		outcome = "error"
		s.maybeLogDepositClassifierError(raw, err)
		return nil, fmt.Errorf("conversation: deposit classification failed: %w", err)
	}

	raw = strings.TrimSpace(resp.Text)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	var decision struct {
		Collect     bool   `json:"collect"`
		AmountCents int32  `json:"amount_cents"`
		SuccessURL  string `json:"success_url"`
		CancelURL   string `json:"cancel_url"`
		Description string `json:"description"`
	}
	jsonText := raw
	if !strings.HasPrefix(jsonText, "{") {
		start := strings.Index(jsonText, "{")
		end := strings.LastIndex(jsonText, "}")
		if start >= 0 && end > start {
			jsonText = jsonText[start : end+1]
		}
	}
	if err := json.Unmarshal([]byte(jsonText), &decision); err != nil {
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

func summarizeHistory(history []ChatMessage, limit int) string {
	if limit > 0 && len(history) > limit {
		history = history[len(history)-limit:]
	}
	var builder strings.Builder
	for _, msg := range history {
		builder.WriteString(msg.Role)
		builder.WriteString(": ")
		builder.WriteString(msg.Content)
		builder.WriteString("\n")
	}
	return builder.String()
}

func (s *LLMService) maybeLogDepositClassifierError(raw string, err error) {
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

func (s *LLMService) shouldSampleDepositLog() bool {
	// 10% sampling to avoid noisy logs.
	return time.Now().UnixNano()%10 == 0
}

// latestTurnAgreedToDeposit returns true when the most recent user message clearly indicates they want to pay a deposit.
// This is used as a deterministic fallback to avoid missing deposits due to LLM classifier variance.
func latestTurnAgreedToDeposit(history []ChatMessage) bool {
	userIndex := -1
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == ChatRoleUser {
			userIndex = i
			break
		}
	}
	if userIndex == -1 {
		return false
	}

	msg := strings.TrimSpace(history[userIndex].Content)
	if msg == "" {
		return false
	}
	if depositNegativeRE.MatchString(msg) {
		return false
	}
	if !depositAffirmativeRE.MatchString(msg) {
		return false
	}
	if depositKeywordRE.MatchString(msg) {
		return true
	}

	// Generic affirmative only counts if the assistant just asked about a deposit.
	for i := userIndex - 1; i >= 0; i-- {
		switch history[i].Role {
		case ChatRoleSystem:
			continue
		case ChatRoleAssistant:
			return depositAskRE.MatchString(history[i].Content)
		default:
			return false
		}
	}
	return false
}

func conversationHasDepositAgreement(history []ChatMessage) bool {
	for i := 0; i < len(history); i++ {
		if history[i].Role != ChatRoleAssistant {
			continue
		}
		if !depositAskRE.MatchString(history[i].Content) {
			continue
		}

		// Look ahead to the next user message (skipping system messages). If they affirm, we treat the
		// deposit as agreed even if the payment record hasn't persisted yet.
		for j := i + 1; j < len(history); j++ {
			switch history[j].Role {
			case ChatRoleSystem:
				continue
			case ChatRoleUser:
				msg := strings.TrimSpace(history[j].Content)
				if msg == "" {
					break
				}
				if depositNegativeRE.MatchString(msg) {
					break
				}
				if depositAffirmativeRE.MatchString(msg) {
					return true
				}
				break
			default:
				// Another assistant turn occurred before a user reply.
				break
			}
			break
		}
	}
	return false
}

// extractAndSavePreferences extracts scheduling preferences from conversation history and saves them.
func (s *LLMService) extractAndSavePreferences(ctx context.Context, leadID string, history []ChatMessage) error {
	return s.savePreferencesFromHistory(ctx, leadID, history, true)
}

func (s *LLMService) savePreferencesFromHistory(ctx context.Context, leadID string, history []ChatMessage, addNote bool) error {
	if s == nil || s.leadsRepo == nil || strings.TrimSpace(leadID) == "" {
		return nil
	}
	prefs, ok := extractPreferences(history, nil)
	if !ok {
		return nil
	}
	if addNote {
		prefs.Notes = fmt.Sprintf("Auto-extracted from conversation at %s", time.Now().Format(time.RFC3339))
	}
	return s.leadsRepo.UpdateSchedulingPreferences(ctx, leadID, prefs)
}

func (s *LLMService) savePreferencesNoNote(ctx context.Context, leadID string, history []ChatMessage, reason string) {
	if s == nil {
		return
	}
	if err := s.savePreferencesFromHistory(ctx, leadID, history, false); err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to save scheduling preferences", "lead_id", leadID, "reason", reason, "error", err)
		}
	}
}

var (
	priceInquiryRE = regexp.MustCompile(`(?i)\b(?:how much|price|pricing|cost|rate|rates|charge)\b`)
	phiPrefaceRE   = regexp.MustCompile(`(?i)\b(?:diagnosed|diagnosis|my condition|my symptoms|i have|i've had|i am|i'm)\b`)
	// PHI keywords with word boundaries to avoid false positives (e.g., "sti" matching in "existing")
	phiKeywordsRE = regexp.MustCompile(`(?i)\b(?:diabetes|hiv|aids|cancer|hepatitis|pregnant|pregnancy|depression|anxiety|bipolar|schizophrenia|asthma|hypertension|blood pressure|infection|herpes|std|sti)\b`)
	// Strong medical advice cues — always trigger with any medical/service context
	strongMedicalCueRE = regexp.MustCompile(`(?i)\b(?:is it safe|safe to|ok to|okay to|contraindications?|side effects?|dosage|dose|mg|milligram|interactions?|mix with|stop taking)\b`)
	// Weak medical advice cues — only trigger with medical-specific context (not service names alone)
	weakMedicalCueRE = regexp.MustCompile(`(?i)\b(?:should i|can i)\b`)
	// Full medical context (services + medical terms) — used with strong cues
	medicalContextRE = regexp.MustCompile(`(?i)\b(?:botox|filler|laser|microneedling|facial|peel|dermaplaning|prp|injectable|medication|medicine|meds|prescription|ibuprofen|tylenol|acetaminophen|antibiotics?|painkillers?|blood pressure|pregnan(?:t|cy)|breastfeed(?:ing)?|allerg(?:y|ic))\b`)
	// Medical-specific context (conditions/medications only, no service names) — used with weak cues
	medicalSpecificContextRE = regexp.MustCompile(`(?i)\b(?:medication|medicine|meds|prescription|ibuprofen|tylenol|acetaminophen|antibiotics?|painkillers?|blood pressure|pregnan(?:t|cy)|breastfeed(?:ing)?|allerg(?:y|ic))\b`)
)

func isPriceInquiry(message string) bool {
	message = strings.TrimSpace(message)
	if message == "" {
		return false
	}
	return priceInquiryRE.MatchString(message) || strings.Contains(message, "$")
}

func isAmbiguousHelp(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return false
	}
	if !(strings.Contains(message, "help") || strings.Contains(message, "question") || strings.Contains(message, "info")) {
		return false
	}
	// If the user already mentioned booking or a service, let the LLM handle it.
	// "available" indicates booking intent (e.g., "do you have anything available Thursday?")
	for _, kw := range []string{"book", "appointment", "schedule", "available", "opening", "botox", "filler", "facial", "laser", "peel", "microneedling", "hydrafacial"} {
		if strings.Contains(message, kw) {
			return false
		}
	}
	return true
}

func isQuestionSelection(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return false
	}
	message = strings.Trim(message, ".!?")
	message = strings.Join(strings.Fields(message), " ")
	if strings.Contains(message, "?") {
		return false
	}

	for _, kw := range []string{"book", "appointment", "schedule", "botox", "filler", "facial", "laser", "peel", "microneedling"} {
		if strings.Contains(message, kw) {
			return false
		}
	}

	switch message {
	case "question",
		"quick question",
		"a question",
		"a quick question",
		"just a question",
		"just a quick question",
		"i had a question",
		"i had a quick question",
		"i just had a question",
		"i just had a quick question",
		"i have a question",
		"i have a quick question",
		"i just have a question",
		"i just have a quick question",
		"i got a question",
		"i got a quick question",
		"i've got a question",
		"i've got a quick question",
		"had a question",
		"had a quick question",
		"have a question",
		"have a quick question",
		"got a question",
		"got a quick question",
		"question please",
		"quick question please",
		"quick question for you",
		"i have a quick question for you",
		"i had a quick question for you",
		"i just had a quick question for you",
		"just a question please",
		"just a quick question please":
		return true
	default:
		return false
	}
}

func detectServiceKey(message string, cfg *clinic.Config) string {
	message = strings.ToLower(message)
	if strings.TrimSpace(message) == "" {
		return ""
	}
	candidates := make([]string, 0, 16)
	if cfg != nil {
		for key := range cfg.ServicePriceText {
			candidates = append(candidates, key)
		}
		for key := range cfg.ServiceDepositAmountCents {
			candidates = append(candidates, key)
		}
		for _, svc := range cfg.Services {
			candidates = append(candidates, svc)
		}
	}
	candidates = append(candidates, "botox", "filler", "dermal filler", "consultation", "laser", "facial", "peel", "microneedling")

	for _, candidate := range candidates {
		key := strings.ToLower(strings.TrimSpace(candidate))
		if key == "" {
			continue
		}
		if strings.Contains(message, key) {
			// Resolve through aliases to canonical service name for price lookup.
			if cfg != nil {
				if resolved, ok := cfg.ServiceAliases[key]; ok {
					return strings.ToLower(resolved)
				}
			}
			return key
		}
	}
	return ""
}

func detectPHI(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return false
	}
	if !phiPrefaceRE.MatchString(message) {
		return false
	}
	// Use regex with word boundaries to avoid false positives
	// (e.g., "sti" matching inside "existing")
	return phiKeywordsRE.MatchString(message)
}

func detectMedicalAdvice(message string) []string {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return nil
	}
	hasStrongCue := strongMedicalCueRE.MatchString(message)
	hasWeakCue := weakMedicalCueRE.MatchString(message)
	if !hasStrongCue && !hasWeakCue {
		return nil
	}
	// Strong cues ("is it safe", "side effects", etc.) trigger with any medical context
	// Weak cues ("can i", "should i") only trigger with medical-specific context
	// (medications, conditions) — not just service names like "botox" which indicate booking intent
	if hasStrongCue {
		if !medicalContextRE.MatchString(message) {
			return nil
		}
	} else {
		if !medicalSpecificContextRE.MatchString(message) {
			return nil
		}
	}
	keywords := []string{}
	for _, kw := range []string{
		"botox", "filler", "laser", "microneedling", "facial", "peel", "dermaplaning", "prp", "injectable",
		"medication", "medicine", "meds", "prescription", "ibuprofen", "tylenol", "acetaminophen", "antibiotic", "antibiotics",
		"painkiller", "painkillers", "blood pressure", "pregnant", "pregnancy", "breastfeeding", "allergy", "allergic",
		"contraindication", "contraindications", "side effects", "dosage", "dose", "interaction", "interactions", "mix with",
	} {
		if strings.Contains(message, kw) {
			keywords = append(keywords, kw)
		}
	}
	if len(keywords) == 0 {
		keywords = append(keywords, "medical_advice_request")
	}
	return keywords
}

func (s *LLMService) appendLeadNote(ctx context.Context, orgID, leadID, note string) {
	if s == nil || s.leadsRepo == nil {
		return
	}
	orgID = strings.TrimSpace(orgID)
	leadID = strings.TrimSpace(leadID)
	note = strings.TrimSpace(note)
	if orgID == "" || leadID == "" || note == "" {
		return
	}
	lead, err := s.leadsRepo.GetByID(ctx, orgID, leadID)
	if err != nil || lead == nil {
		return
	}
	existing := strings.TrimSpace(lead.SchedulingNotes)
	switch {
	case existing == "":
		existing = note
	case strings.Contains(existing, note):
		// Avoid duplication.
	default:
		existing = existing + " | " + note
	}
	_ = s.leadsRepo.UpdateSchedulingPreferences(ctx, leadID, leads.SchedulingPreferences{Notes: existing})
}

// isCapitalized checks if a string starts with an uppercase letter
func isCapitalized(s string) bool {
	if len(s) == 0 {
		return false
	}
	return s[0] >= 'A' && s[0] <= 'Z'
}

func formatLeadPreferenceContext(lead *leads.Lead) string {
	if lead == nil {
		return ""
	}
	lines := make([]string, 0, 5)
	name := strings.TrimSpace(lead.Name)
	if name != "" && !looksLikePhone(name, lead.Phone) {
		label := "Name"
		if len(strings.Fields(name)) == 1 {
			label = "Name (first only)"
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", label, name))
	}
	service := strings.TrimSpace(lead.ServiceInterest)
	if service != "" {
		lines = append(lines, fmt.Sprintf("- Service: %s", service))
	}
	patientType := strings.TrimSpace(lead.PatientType)
	if patientType != "" {
		lines = append(lines, fmt.Sprintf("- Patient type: %s", patientType))
	}
	days := strings.TrimSpace(lead.PreferredDays)
	if days != "" {
		lines = append(lines, fmt.Sprintf("- Preferred days: %s", days))
	}
	times := strings.TrimSpace(lead.PreferredTimes)
	if times != "" {
		lines = append(lines, fmt.Sprintf("- Preferred times: %s", times))
	}
	if len(lines) == 0 {
		return ""
	}
	return "Known scheduling preferences from earlier messages:\n" + strings.Join(lines, "\n")
}

func looksLikePhone(name string, phone string) bool {
	name = strings.TrimSpace(name)
	phone = strings.TrimSpace(phone)
	if name == "" {
		return false
	}
	if phone != "" && name == phone {
		return true
	}
	digits := 0
	for i := 0; i < len(name); i++ {
		if name[i] >= '0' && name[i] <= '9' {
			digits++
		}
	}
	return digits >= 7
}

// splitName splits a full name into first and last name.
// "Andy Wolf" → ("Andy", "Wolf"), "Madonna" → ("Madonna", ""), "  " → ("", "").
func splitName(full string) (string, string) {
	parts := strings.Fields(full)
	switch len(parts) {
	case 0:
		return "", ""
	case 1:
		return parts[0], ""
	default:
		return parts[0], strings.Join(parts[1:], " ")
	}
}

// AppendAssistantMessage appends an assistant message to the LLM conversation
// history. Used by the worker to inject time-selection SMS into history so the
// LLM knows what was presented when the patient replies.
func (s *LLMService) AppendAssistantMessage(ctx context.Context, conversationID, message string) error {
	history, err := s.history.Load(ctx, conversationID)
	if err != nil {
		return fmt.Errorf("load history: %w", err)
	}
	history = append(history, ChatMessage{
		Role:    ChatRoleAssistant,
		Content: message,
	})
	return s.history.Save(ctx, conversationID, history)
}
