package conversation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"go.opentelemetry.io/otel/attribute"
)

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
			usesMoxie = cfg.UsesMoxieBooking() || cfg.UsesBoulevardBooking()
		}
	}
	var systemPrompt string
	if isVoiceChannel(req.Channel) {
		systemPrompt = buildVoiceSystemPrompt(int(depositCents), usesMoxie, startCfg)
	} else {
		systemPrompt = buildSystemPrompt(int(depositCents), usesMoxie, startCfg)
	}

	if req.Silent {
		history := []ChatMessage{{Role: ChatRoleSystem, Content: systemPrompt}}
		history = s.appendContext(ctx, history, req.OrgID, req.LeadID, req.ClinicID, "")
		if req.AckMessage != "" {
			history = append(history, ChatMessage{Role: ChatRoleAssistant, Content: req.AckMessage})
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
		return &Response{ConversationID: conversationID, Message: "", Timestamp: time.Now().UTC()}, nil
	}

	if sawPHI {
		history := []ChatMessage{{Role: ChatRoleSystem, Content: systemPrompt}}
		history = s.appendContext(ctx, history, req.OrgID, req.LeadID, req.ClinicID, "")
		history = append(history, ChatMessage{Role: ChatRoleUser, Content: formatIntroMessage(safeReq, conversationID)})
		history = append(history, ChatMessage{Role: ChatRoleAssistant, Content: phiDeflectionReply})
		history = trimHistory(history, maxHistoryMessages)
		if err := s.history.Save(ctx, conversationID, history); err != nil {
			span.RecordError(err)
			return nil, err
		}
		if s.audit != nil && strings.TrimSpace(req.OrgID) != "" {
			_ = s.audit.LogPHIDetected(ctx, req.OrgID, conversationID, req.LeadID, req.Intro, "keyword")
		}
		return &Response{ConversationID: conversationID, Message: phiDeflectionReply, Timestamp: time.Now().UTC()}, nil
	}

	if len(medicalKeywords) > 0 {
		history := []ChatMessage{{Role: ChatRoleSystem, Content: systemPrompt}}
		safeReq := req
		safeReq.Intro = "[REDACTED]"
		history = s.appendContext(ctx, history, req.OrgID, req.LeadID, req.ClinicID, "")
		history = append(history, ChatMessage{Role: ChatRoleUser, Content: formatIntroMessage(safeReq, conversationID)})
		history = append(history, ChatMessage{Role: ChatRoleAssistant, Content: medicalAdviceDeflectionReply})
		history = trimHistory(history, maxHistoryMessages)
		if err := s.history.Save(ctx, conversationID, history); err != nil {
			span.RecordError(err)
			return nil, err
		}
		if s.audit != nil && strings.TrimSpace(req.OrgID) != "" {
			_ = s.audit.LogMedicalAdviceRefused(ctx, req.OrgID, conversationID, req.LeadID, "[REDACTED]", medicalKeywords)
		}
		return &Response{ConversationID: conversationID, Message: medicalAdviceDeflectionReply, Timestamp: time.Now().UTC()}, nil
	}

	history := []ChatMessage{{Role: ChatRoleSystem, Content: systemPrompt}}
	history = s.appendContext(ctx, history, req.OrgID, req.LeadID, req.ClinicID, req.Intro)
	history = append(history, ChatMessage{Role: ChatRoleUser, Content: formatIntroMessage(safeReq, conversationID)})

	if startCfg != nil && (startCfg.UsesMoxieBooking() || startCfg.UsesBoulevardBooking()) {
		prefs, _ := extractPreferences(history, serviceAliasesFromConfig(startCfg))
		if prefs.ServiceInterest != "" && s.prefetcher != nil {
			s.prefetcher.StartPrefetch(ctx, req.OrgID, startCfg, prefs.ServiceInterest, prefs.ProviderPreference)
		}
		if prefs.ServiceInterest != "" && prefs.Name == "" && !lastAssistantAskedForName(history) {
			history = append(history, ChatMessage{Role: ChatRoleSystem, Content: "[SYSTEM GUARDRAIL] The patient mentioned a service but you do NOT have their name yet. NAME is #1 in the booking checklist and MUST be collected before anything else. You MUST ask for their full name NOW. Do NOT ask about patient type, schedule, provider, or email yet. Do NOT present availability. Ask something like: 'Great choice! May I have your full name?'"})
		}
		if prefs.ServiceInterest != "" && prefs.Name != "" && prefs.PatientType == "" {
			history = append(history, ChatMessage{Role: ChatRoleSystem, Content: "[SYSTEM GUARDRAIL] You have the patient's name and service interest. Next in the checklist is PATIENT TYPE (#3). You MUST ask if they are a new or returning patient NOW. Do NOT ask about schedule, email, or provider yet. Do NOT present availability. Ask something like: 'Have you visited us before, or would this be your first time?'"})
		}
		if prefs.ServiceInterest != "" && prefs.Name != "" && prefs.PatientType != "" && prefs.PreferredDays == "" && prefs.PreferredTimes == "" {
			history = append(history, ChatMessage{Role: ChatRoleSystem, Content: "[SYSTEM GUARDRAIL] You have the patient's name, service, and patient type. Next in the booking checklist is SCHEDULE (#4). You MUST ask about their preferred days and times NOW. Do NOT ask for email or provider preference yet. Do NOT present availability."})
		}
		if prefs.ServiceInterest != "" && prefs.ProviderPreference == "" && (prefs.PreferredDays != "" || prefs.PreferredTimes != "") {
			resolvedService := startCfg.ResolveServiceName(prefs.ServiceInterest)
			if startCfg.ServiceNeedsProviderPreference(resolvedService) {
				providerList := buildProviderGuardrailList(startCfg, resolvedService)
				history = append(history, ChatMessage{Role: ChatRoleSystem, Content: fmt.Sprintf("[SYSTEM GUARDRAIL] The patient wants %s which has multiple providers.%s You MUST ask about provider preference NOW. Do NOT ask for email yet. Do NOT present availability yet. Ask: 'Do you have a provider preference, or would you like the first available appointment?'", prefs.ServiceInterest, providerList)})
			}
		}
	}

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
			history = append(history, ChatMessage{Role: ChatRoleSystem, Content: fmt.Sprintf("[STATE SUMMARY] Already collected from this patient: %s. Do NOT re-ask for any of these. Move to the NEXT missing qualification only.", strings.Join(collected, ", "))})
		}
	}

	reply, err := s.generateResponse(ctx, history)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	reply = sanitizeSMSResponse(reply)
	history = append(history, ChatMessage{Role: ChatRoleAssistant, Content: reply})

	history = trimHistory(history, maxHistoryMessages)
	if err := s.history.Save(ctx, conversationID, history); err != nil {
		span.RecordError(err)
		return nil, err
	}

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

	resp := &Response{ConversationID: conversationID, Message: reply, Timestamp: time.Now().UTC()}

	// Count user messages in history to detect first message. On the very first
	// message, old lead data may satisfy ShouldFetchAvailability but the patient
	// hasn't actually gone through the qualification flow in THIS conversation.
	// Force them through name → patient type → schedule → provider before fetching.
	userMsgCount := 0
	for _, m := range history {
		if m.Role == ChatRoleUser {
			userMsgCount++
		}
	}

	moxieAPIReady := s.moxieClient != nil && startCfg != nil && startCfg.MoxieConfig != nil
	boulevardReady := s.boulevardAdapter != nil && startCfg != nil && startCfg.UsesBoulevardBooking()
	bookingAPIReady := moxieAPIReady || boulevardReady
	if bookingAPIReady && usesMoxie && userMsgCount > 1 && ShouldFetchAvailabilityWithConfig(history, nil, startCfg) {
		prefs, _ := extractPreferences(history, serviceAliasesFromConfig(startCfg))
		if !hasSchedulePreferences(&prefs) {
			s.logger.Info("StartConversation: skipping time selection — no schedule preferences yet", "conversation_id", conversationID)
			return resp, nil
		}

		variantResp, variantErr := s.handleVariantResolution(ctx, startCfg, &prefs, history, "", conversationID, req.OrgID)
		if variantErr != nil {
			return nil, variantErr
		}
		if variantResp != nil {
			resp.Message = variantResp.Message
			return resp, nil
		}

		if provResp := s.handleProviderPreference(startCfg, &prefs, prefs.ServiceInterest, conversationID); provResp != nil {
			resp.Message = provResp.Message
			return resp, nil
		}

		tsResp := s.fetchAndPresentAvailability(ctx, &prefs, startCfg, startCfg.BookingURL, conversationID, req.OrgID, nil)
		if tsResp != nil && len(tsResp.Slots) > 0 {
			tsResp.SavedToHistory = true
			resp.TimeSelectionResponse = tsResp
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

// buildProviderGuardrailList formats a provider list hint for provider preference guardrails.
func buildProviderGuardrailList(cfg *clinic.Config, resolvedService string) string {
	if cfg == nil {
		return ""
	}
	providerNames := cfg.ProviderNamesForService(resolvedService)
	if len(providerNames) == 0 {
		return ""
	}
	return fmt.Sprintf(" Available providers for %s: %s.", resolvedService, strings.Join(providerNames, ", "))
}
