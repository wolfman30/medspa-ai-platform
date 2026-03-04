package conversation

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/compliance"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	moxieclient "github.com/wolfman30/medspa-ai-platform/internal/moxie"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
	"go.opentelemetry.io/otel/attribute"
	"strings"
	"time"
)

type contextKey string

const ctxKeyVoiceModel contextKey = "voiceModel"

const (
	maxHistoryMessages           = 40
	maxVoiceHistoryMessages      = 20
	phiDeflectionReply           = "Thanks for sharing. I can help with booking and general questions, but I can't provide medical advice over text. Please call the clinic for medical guidance or discuss this with your provider during your consultation."
	medicalAdviceDeflectionReply = "I can help with booking and general questions, but I can't provide medical advice over text. Please call the clinic for medical guidance or discuss this with your provider during your consultation."
)

// LLMService produces conversation responses using a configured LLM and stores context in Redis.
type LLMService struct {
	client          LLMClient
	rag             RAGRetriever
	emr             *EMRAdapter
	moxieClient     *moxieclient.Client
	model           string
	voiceModel      string
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
	prefetcher      *AvailabilityPrefetcher
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
	if isVoiceChannel(req.Channel) && s.voiceModel != "" {
		ctx = context.WithValue(ctx, ctxKeyVoiceModel, s.voiceModel)
	}
	filter := FilterInbound(req.Intro)
	redactedIntro := filter.RedactedMsg
	sawPHI := filter.SawPHI
	medicalKeywords := filter.MedicalKW

	// Prompt injection detection on first message.
	if filter.DeflectionMsg == blockedReply {
		injectionResult := ScanForPromptInjection(req.Intro)
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
	if filter.Sanitized != req.Intro {
		injectionResult := ScanForPromptInjection(req.Intro)
		s.events.PromptInjectionDetected(ctx, req.ConversationID, req.OrgID, false, injectionResult.Score, injectionResult.Reasons)
		s.logger.Warn("StartConversation: prompt injection WARNING",
			"org_id", req.OrgID,
			"score", injectionResult.Score,
			"reasons", injectionResult.Reasons,
		)
		req.Intro = filter.Sanitized
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

		// Pre-fetch availability as soon as we know the service — while we're
		// still collecting name, patient type, etc. By the time qualifications
		// are done, slots are already cached.
		if prefs.ServiceInterest != "" && s.prefetcher != nil {
			s.prefetcher.StartPrefetch(ctx, req.OrgID, startCfg, prefs.ServiceInterest, prefs.ProviderPreference)
		}

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

	// Voice: inject state summary so LLM knows what's already collected and doesn't re-ask
	if isVoiceChannel(req.Channel) && startCfg != nil {
		prefs, _ := extractPreferences(history, serviceAliasesFromConfig(startCfg))
		var collected []string
		if prefs.Name != "" {
			collected = append(collected, fmt.Sprintf("Name: %s", prefs.Name))
		}
		if prefs.ServiceInterest != "" {
			collected = append(collected, fmt.Sprintf("Service: %s", prefs.ServiceInterest))
		}
		if prefs.PatientType != "" {
			collected = append(collected, fmt.Sprintf("Patient type: %s", prefs.PatientType))
		}
		if prefs.PreferredDays != "" || prefs.PreferredTimes != "" {
			collected = append(collected, fmt.Sprintf("Schedule: %s %s", prefs.PreferredDays, prefs.PreferredTimes))
		}
		if len(collected) > 0 {
			history = append(history, ChatMessage{
				Role: ChatRoleSystem,
				Content: fmt.Sprintf("[STATE SUMMARY] Already collected from this patient: %s. "+
					"Do NOT re-ask for any of these. Move to the NEXT missing qualification only.",
					strings.Join(collected, ", ")),
			})
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
	if moxieAPIReady && usesMoxie && ShouldFetchAvailabilityWithConfig(history, nil, startCfg) {
		prefs, _ := extractPreferences(history, serviceAliasesFromConfig(startCfg))

		// SCHEDULE CHECK — if we have name + service + patient type but no schedule
		// preferences, don't skip to availability. Let the normal flow ask about
		// preferred days/times first (matches ProcessMessage guardrail).
		if !hasSchedulePreferences(&prefs) {
			s.logger.Info("StartConversation: skipping time selection — no schedule preferences yet",
				"conversation_id", conversationID)
			return resp, nil
		}

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
//
// The method is organised into sequential phases, each extracted into its own
// method on LLMService. A shared processContext carries state between phases.
func (s *LLMService) ProcessMessage(ctx context.Context, req MessageRequest) (*Response, error) {
	if isVoiceChannel(req.Channel) && s.voiceModel != "" {
		ctx = context.WithValue(ctx, ctxKeyVoiceModel, s.voiceModel)
	}
	if strings.TrimSpace(req.ConversationID) == "" {
		return nil, errors.New("conversation: conversationID required")
	}

	// Phase 1: Inbound filtering (prompt injection, PHI, medical keywords)
	pc, earlyResp := s.newProcessContext(ctx, req)
	if earlyResp != nil {
		return earlyResp, nil
	}

	s.logger.Info("ProcessMessage called",
		"conversation_id", req.ConversationID,
		"org_id", req.OrgID,
		"lead_id", req.LeadID,
		"message", pc.redactedMessage,
	)

	ctx, span := llmTracer.Start(ctx, "conversation.message")
	defer span.End()
	span.SetAttributes(
		attribute.String("medspa.org_id", req.OrgID),
		attribute.String("medspa.conversation_id", req.ConversationID),
		attribute.String("medspa.channel", string(req.Channel)),
	)
	pc.span = span

	// Phase 2: Load conversation history (or bootstrap a new conversation)
	resp, histErr := s.loadHistory(ctx, pc)
	if histErr != nil {
		return nil, histErr
	}
	if resp != nil {
		// loadHistory returns a deflection response for PHI/medical on new conversations.
		// For the "unknown conversation + clean message" case, it returns nil and we
		// need to check if history is still empty (meaning StartConversation is needed).
		return resp, nil
	}
	if pc.history == nil {
		// Unknown conversation, no PHI/medical — delegate to StartConversation
		return s.StartConversation(ctx, StartRequest{
			OrgID:          req.OrgID,
			ConversationID: req.ConversationID,
			LeadID:         req.LeadID,
			ClinicID:       req.ClinicID,
			Intro:          pc.rawMessage,
			Channel:        req.Channel,
			From:           req.From,
			To:             req.To,
			Metadata:       req.Metadata,
		})
	}

	// Phase 3: Safety deflections (PHI / medical advice on existing conversations)
	if resp := s.handleSafetyDeflections(ctx, pc); resp != nil {
		return resp, nil
	}

	// Append context and user message to history
	pc.history = s.appendContext(ctx, pc.history, req.OrgID, req.LeadID, req.ClinicID, pc.rawMessage)
	pc.history = append(pc.history, ChatMessage{
		Role:    ChatRoleUser,
		Content: pc.rawMessage,
	})

	// Load clinic config for deterministic guardrails
	if s.clinicStore != nil && req.OrgID != "" {
		if loaded, err := s.clinicStore.Get(ctx, req.OrgID); err == nil {
			pc.cfg = loaded
		}
	}

	// Phase 4: Deterministic guardrails (price inquiry, question selection, ambiguous help)
	if resp := s.handleDeterministicGuardrails(ctx, pc); resp != nil {
		return resp, nil
	}

	// Phase 5: FAQ classification
	if resp := s.handleFAQClassification(ctx, pc); resp != nil {
		return resp, nil
	}

	// Phase 6: Time selection state (load, new-service detection, active selection)
	s.loadTimeSelectionState(ctx, pc)
	s.handleActiveTimeSelection(ctx, pc)

	// Phase 7: Moxie qualification guardrails
	s.injectMoxieQualificationGuardrails(ctx, pc)

	// Phase 8: LLM response generation
	reply, err := s.generateResponse(ctx, pc.history)
	if err != nil {
		return nil, err
	}
	reply = sanitizeSMSResponse(reply)
	pc.reply = reply
	pc.history = append(pc.history, ChatMessage{
		Role:    ChatRoleAssistant,
		Content: reply,
	})
	pc.history = trimHistory(pc.history, maxHistoryMessages)
	if err := s.history.Save(ctx, req.ConversationID, pc.history); err != nil {
		span.RecordError(err)
		return nil, err
	}

	// Phase 9: Post-LLM processing (deposit, preferences, time selection trigger, booking)
	s.handlePostLLMResponse(ctx, pc)

	return &Response{
		ConversationID:        req.ConversationID,
		Message:               pc.reply,
		Timestamp:             time.Now().UTC(),
		DepositIntent:         pc.depositIntent,
		TimeSelectionResponse: pc.timeSelectionResponse,
		BookingRequest:        pc.bookingRequest,
		AsyncAvailability:     pc.asyncAvailability,
	}, nil
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

	model := s.model
	if m, ok := ctx.Value(ctxKeyVoiceModel).(string); ok && m != "" {
		model = m
	}
	req := LLMRequest{
		Model:       model,
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

// ClearLeadPreferences resets scheduling preferences for a lead identified by
// org + phone. Used when a new voice call starts to prevent stale data from
// a previous call leaking into qualification checks.
func (s *LLMService) ClearLeadPreferences(ctx context.Context, orgID, phone string) error {
	if s.leadsRepo == nil {
		return nil
	}
	lead, err := s.leadsRepo.GetOrCreateByPhone(ctx, orgID, phone, "voice", "")
	if err != nil {
		return fmt.Errorf("get lead: %w", err)
	}
	return s.leadsRepo.UpdateSchedulingPreferences(ctx, lead.ID, leads.SchedulingPreferences{})
}
