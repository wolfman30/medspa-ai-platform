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
	"regexp"
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
	if isVoiceChannel(req.Channel) && s.voiceModel != "" {
		ctx = context.WithValue(ctx, ctxKeyVoiceModel, s.voiceModel)
	}
	if strings.TrimSpace(req.ConversationID) == "" {
		return nil, errors.New("conversation: conversationID required")
	}

	rawMessage := req.Message
	filter := FilterInbound(rawMessage)
	redactedMessage := filter.RedactedMsg
	sawPHI := filter.SawPHI
	medicalKeywords := filter.MedicalKW

	s.events.MessageReceived(ctx, req.ConversationID, req.OrgID, req.LeadID, rawMessage)

	// Prompt injection detection — scan inbound messages before they reach the LLM.
	if filter.DeflectionMsg == blockedReply {
		injectionResult := ScanForPromptInjection(rawMessage)
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
	if filter.Sanitized != rawMessage {
		injectionResult := ScanForPromptInjection(rawMessage)
		s.logger.Warn("ProcessMessage: prompt injection WARNING",
			"conversation_id", req.ConversationID,
			"org_id", req.OrgID,
			"score", injectionResult.Score,
			"reasons", injectionResult.Reasons,
		)
		rawMessage = filter.Sanitized
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
	// Trim voice history more aggressively for lower latency
	if isVoiceChannel(req.Channel) && len(history) > maxVoiceHistoryMessages {
		history = trimHistory(history, maxVoiceHistoryMessages)
	}
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
			if s.moxieClient != nil && cfg != nil && cfg.MoxieConfig != nil {

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

		// Pre-fetch availability as soon as we know the service.
		if prefs.ServiceInterest != "" && s.prefetcher != nil {
			s.prefetcher.StartPrefetch(ctx, req.OrgID, cfg, prefs.ServiceInterest, prefs.ProviderPreference)
		}

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
	moxieAPIReady := s.moxieClient != nil && clinicCfg != nil && clinicCfg.MoxieConfig != nil
	qualificationsMet := ShouldFetchAvailabilityWithConfig(history, nil, clinicCfg)
	shouldTriggerTimeSelection := moxieAPIReady && timeSelectionState == nil
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
		"moxie_api_ready", moxieAPIReady,
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

			// Voice calls: return filler immediately and fetch availability async via SMS.
			// Time slots are hard to communicate verbally — SMS is the right UX.
			if isVoiceChannel(req.Channel) {
				s.logger.Info("voice channel: deferring availability to async SMS",
					"conversation_id", req.ConversationID,
					"service", prefs.ServiceInterest,
				)
				return &Response{
					ConversationID: req.ConversationID,
					Message:        fmt.Sprintf("Let me check what's available for %s. I'll text you the options in just a moment so you can pick the best time.", prefs.ServiceInterest),
					Timestamp:      time.Now().UTC(),
					AsyncAvailability: &AsyncAvailabilityRequest{
						OrgID:           req.OrgID,
						ConversationID:  req.ConversationID,
						From:            req.From,
						To:              req.To,
						BookingURL:      bookingURL,
						ServiceInterest: prefs.ServiceInterest,
					},
				}, nil
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
