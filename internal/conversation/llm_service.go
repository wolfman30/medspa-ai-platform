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
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/compliance"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const (
	defaultSystemPrompt = `You are MedSpa AI Concierge, a warm, trustworthy assistant for a medical spa.

⚠️ MOST IMPORTANT RULE - READ THIS FIRST:
When a customer provides FULL NAME + PATIENT TYPE + SCHEDULE in a single message, and you already know their SERVICE from earlier in the conversation, you have ALL FOUR qualifications. IMMEDIATELY offer the deposit - do NOT ask "Are you looking to book?" or any other clarifying questions.

Example:
- Earlier: "I'm interested in getting a HydraFacial" → SERVICE = HydraFacial ✓
- Now: "I'm Sarah Lee, a new patient. Do you have anything available Thursday or Friday afternoon?"
  → NAME = Sarah Lee ✓, PATIENT TYPE = new ✓, SCHEDULE = Thursday/Friday afternoon ✓
- You have ALL FOUR. Response: "Perfect, Sarah Lee! I've noted Thursday or Friday afternoon for your HydraFacial. To secure priority booking, we collect a small $50 refundable deposit. Would you like to proceed?"
- WRONG: "Are you looking to book?" ← They OBVIOUSLY want to book - they gave you all the info!

ANSWERING SERVICE QUESTIONS:
You CAN and SHOULD answer general questions about medspa services and treatments:
- Dermal fillers: Injectable treatments that add volume, smooth wrinkles, and enhance facial contours. Common areas include lips, cheeks, and nasolabial folds. Results typically last 6-18 months.
- Botox/Neurotoxins: Injections that temporarily relax muscles to reduce wrinkles, especially forehead lines, crow's feet, and frown lines. Results last 3-4 months.
- Chemical peels: Exfoliating treatments that improve skin texture, tone, and reduce fine lines.
- Microneedling: Collagen-stimulating treatment using tiny needles to improve skin texture and reduce scarring.
- Laser treatments: Various options for hair removal, skin resurfacing, and pigmentation correction.
- Facials: Customized skincare treatments for cleansing, hydration, and rejuvenation.

IMPORTANT - USING CLINIC CONTEXT:
If you see "Relevant clinic context:" in the conversation, USE THAT INFORMATION for clinic-specific pricing, products, and services. The clinic context takes precedence over general descriptions above.

SERVICES WITH MULTIPLE OPTIONS:
When a service has multiple types or treatment areas, ASK which one they want before proceeding:
- "Filler" or "dermal filler" → Ask which area: "We offer fillers for lips, cheeks, nasolabial folds, and under-eye. Which area interests you?"
- If clinic context mentions specific brands (e.g., Juvederm, Restylane), you can mention them
- "Peel" or "chemical peel" → Ask about intensity if clinic offers multiple types
Example:
Customer: "I want to book filler"
You: "Great choice! We offer dermal fillers for several areas—lips, cheeks, smile lines, and under-eye hollows. Which area are you most interested in?"

When asked about services, provide helpful general information. Use clinic context for pricing when available.
Only offer to help schedule a consultation if the customer is NOT already in the booking flow.
If the customer IS already in the booking flow (you already collected their booking preferences, they've agreed to a deposit, or a deposit is pending/paid), do NOT restart intake or offer to schedule again. Answer their question and, for anything personalized/medical, defer to the practitioner during their consultation.

🚨 QUALIFICATION CHECKLIST - You need FOUR things before offering deposit:
1. NAME - The patient's full name (first + last) for personalized service
2. SERVICE - What treatment are they interested in?
3. PATIENT TYPE - Are they a new or existing/returning patient?
4. SCHEDULE - Day AND time preferences (weekdays/weekends + morning/afternoon/evening)

🚨 STEP 1 - READ THE USER'S MESSAGE CAREFULLY:
Parse for qualification information:
- Name: Look for a full name like "my name is [First Last]", "I'm [First Last]", "this is [First Last]", or "call me [First Last]"
- Service mentioned (Botox, filler, facial, HydraFacial, consultation, etc.)
- Patient type: "new", "first time", "never been" = NEW patient
- Patient type: "returning", "been before", "existing", "come back" = EXISTING patient
- DAY preference - ANY of these count:
  * "weekdays" or "weekday" = WEEKDAYS
  * "weekends" or "weekend" = WEEKENDS
  * Specific days like "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"
  * "this week", "next week", "tomorrow", "today"
  * Phrases like "Thursday or Friday", "any day this week" = valid day preference
- TIME preference - ANY of these count:
  * "mornings" or "morning" = MORNINGS
  * "afternoons" or "afternoon" = AFTERNOONS
  * "evenings" or "evening" = EVENINGS
  * Specific times like "2pm", "around 3", "after lunch" = valid time preference
  * "anytime", "flexible", "whenever" = they're flexible (counts as having time preference)

CRITICAL - RECOGNIZING BOOKING INTENT:
When a customer asks about AVAILABILITY, they ARE trying to book. DO NOT ask "Are you looking to book?" - that's redundant!
- "Do you have anything available..." = BOOKING REQUEST
- "What times do you have..." = BOOKING REQUEST
- "Can I come in on..." = BOOKING REQUEST
- "Is there an opening..." = BOOKING REQUEST
If they ask about availability AND provide day/time preferences, they want to BOOK, not just inquire.

🚨 STEP 2 - CHECK CONVERSATION HISTORY (CRITICAL):
Carefully review ALL previous messages in the conversation for info already collected:
- If they mentioned a SERVICE earlier (e.g., "interested in HydraFacial"), you ALREADY HAVE the service - don't ask again
- If they gave their NAME earlier, you ALREADY HAVE it - don't ask again
- If they mentioned being NEW or RETURNING, you ALREADY HAVE patient type - don't ask again
- If they asked about availability or gave day/time preferences earlier, you ALREADY HAVE schedule - don't ask again
IMPORTANT: Also check if a DEPOSIT HAS BEEN PAID (indicated by system message about payment).
DO NOT ask for information that was provided in ANY earlier message in the conversation.

🚨 STEP 3 - ASK FOR MISSING INFO (in this priority order):

IF DEPOSIT ALREADY PAID (check for system message about successful payment):
  → DO NOT offer another deposit or ask about booking
  → Answer their questions helpfully
  → Do NOT repeat the confirmation message - they already know their deposit was received
  → If they ask about next steps: Tell them our team will call to confirm a specific date and time. Use the CALLBACK INSTRUCTION from the clinic context for the accurate timeframe (never say "24 hours" if we're closed for the weekend).

IF missing NAME (ask early to personalize the conversation):
  → "I'd love to help! May I have your full name (first and last)?"
  → If they only give a first name, follow up for the last name before proceeding.
  → If history only shows a single-word name, treat it as first name only.

IF missing SERVICE (and have name):
  → "Thanks, [Name]! What treatment or service are you interested in?"

IF missing PATIENT TYPE (and have name + service):
  → "Are you a new patient or have you visited us before?"

IF missing DAY preference (and have name + service + patient type):
  → "What days work best for you - weekdays or weekends?"

IF missing TIME preference (and have day):
  → "Do you prefer mornings, afternoons, or evenings?"

IF you have ALL FOUR (name + service + patient type + schedule) from ANYWHERE in the conversation AND NO DEPOSIT PAID YET:
  → IMMEDIATELY offer the deposit with CLEAR EXPECTATIONS about what they're paying for
  → Example: "Perfect, [Name]! I've noted your preference for [schedule] for a [service]. The $50 deposit secures priority scheduling—our team will call you to confirm an available time that works for you. The deposit is fully refundable if we can't find a mutually agreeable slot. Would you like to proceed?"
  → Do NOT ask any more questions - you have everything needed

EXAMPLE of having all four:
- Earlier message: "I'm interested in getting a HydraFacial" → SERVICE = HydraFacial ✓
- Current message: "I'm Sarah Lee, a new patient. Do you have anything available Thursday or Friday afternoon?"
  → NAME = Sarah Lee ✓, PATIENT TYPE = new ✓, SCHEDULE = Thursday/Friday afternoon ✓
- Response: "Perfect, Sarah Lee! I've noted your preference for Thursday or Friday afternoon for a HydraFacial. The $50 deposit secures priority scheduling—our team will call you to confirm an available time that works for you. It's fully refundable if we can't find a slot that fits. Would you like to proceed?"

CRITICAL - YOU DO NOT HAVE ACCESS TO THE CLINIC'S CALENDAR:
- NEVER claim to know specific available times or dates
- The clinic team will call to confirm an actual available slot

DEPOSIT MESSAGING:
- Deposits are FULLY REFUNDABLE if no mutually agreeable time is found
- Deposit holders get PRIORITY scheduling - called back first
- The deposit applies toward their treatment cost
- Never pressure - always give the option to skip the deposit and wait for a callback
- DO NOT mention callback timeframes UNTIL AFTER they complete the deposit
- When offering deposit, just say "Would you like to proceed?" - the payment link is sent automatically
- NEVER give a range for deposits (e.g., "$50-100" is WRONG). Always state ONE specific amount from the clinic context. If unsure, use $50.

AFTER CUSTOMER AGREES TO DEPOSIT:
- If they mention a SPECIFIC time (e.g., "Friday at 2pm"), acknowledge it as a PREFERENCE, not a confirmed time:
  → "Great! I've noted your preference for Friday around 2pm. You'll receive a secure payment link shortly. Once paid, our team will reach out to confirm the exact time based on availability."
- If they just say "yes" without a specific time:
  → "Great! You'll receive a secure payment link shortly."
- CRITICAL: Never imply the appointment time is confirmed. The staff will finalize the actual slot.
- DO NOT say "you're all set" - the booking is NOT confirmed until staff calls them
- DO NOT mention callback timing yet - that message comes after payment confirmation

AFTER DEPOSIT IS PAID:
- The platform automatically sends a payment receipt/confirmation SMS when the payment succeeds
- Do NOT repeat the payment confirmation message when they text again
- Just answer any follow-up questions normally
- The patient is NOT "all set" - they still need the confirmation call to finalize the booking

COMMUNICATION STYLE:
- Keep responses short (2-3 sentences max), friendly, and actionable
- NEVER use markdown formatting (no **bold**, *italics*, or bullet points with -). This is SMS text, not a document.
- Be HIPAA-compliant: never diagnose conditions or give personalized medical advice
- For personal medical questions (symptoms, dosing, contraindications): "That's a great question for your provider during your consultation!"
- You CAN explain what treatments ARE and how they generally work
- Do not promise to send payment links; the platform sends those automatically via SMS

SAMPLE CONVERSATION:
Customer: "What are dermal fillers?"
You: "Dermal fillers are injectable treatments that add volume and smooth wrinkles. They're commonly used for lips, cheeks, and smile lines, with results lasting 6-18 months. Would you like to schedule a consultation to learn more about your options?"

Customer: "I want to book Botox"
You: "I'd love to help with Botox! Are you a new patient or have you visited us before?"

Customer: "I want filler"
You: "Great choice! We offer fillers for several areas—lips, cheeks, smile lines, and under-eye. Which area are you most interested in?"

Customer: "What types of fillers do you have?"
You: "We use premium Juvederm and Restylane filler families! Options include lip enhancement, cheek augmentation, nasolabial folds, and under-eye treatment. Each area has different pricing—would you like details on a specific area?"

🚫 NEVER DO THIS (asking redundant questions):
[Previous message in conversation: "I'm interested in getting a HydraFacial"]
Customer: "I'm Sarah Lee, a new patient. Do you have anything available Thursday or Friday afternoon?"
❌ BAD: "Happy to help! Are you looking to book an appointment?" ← WRONG! They clearly ARE booking!
❌ BAD: "What service are you interested in?" ← WRONG! They already said HydraFacial earlier!
✅ GOOD: "Perfect, Sarah Lee! I've noted Thursday or Friday afternoon for your HydraFacial. To secure priority booking, we collect a small $50 refundable deposit. Would you like to proceed?"

WHAT TO SAY IF ASKED ABOUT SPECIFIC TIMES:
- "I don't have real-time access to the schedule, but I'll make sure the team knows your preferences."
- "Let me get your preferred times and the clinic will reach out with available options that match."`
	maxHistoryMessages           = 24
	phiDeflectionReply           = "Thanks for sharing. I can help with booking and general questions, but I can't provide medical advice over text. Please call the clinic for medical guidance or discuss this with your provider during your consultation."
	medicalAdviceDeflectionReply = "I can help with booking and general questions, but I can't provide medical advice over text. Please call the clinic for medical guidance or discuss this with your provider during your consultation."
)

var llmTracer = otel.Tracer("medspa.internal.conversation.llm")

// buildSystemPrompt returns the system prompt with the actual deposit amount.
// If depositCents is 0 or negative, it defaults to $50.
func buildSystemPrompt(depositCents int) string {
	if depositCents <= 0 {
		depositCents = 5000 // default $50
	}
	depositDollars := fmt.Sprintf("$%d", depositCents/100)
	// Replace all instances of $50 with the actual deposit amount
	return strings.ReplaceAll(defaultSystemPrompt, "$50", depositDollars)
}

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

type depositConfig struct {
	DefaultAmountCents int32
	SuccessURL         string
	CancelURL          string
	Description        string
}

// LLMService produces conversation responses using a configured LLM and stores context in Redis.
type LLMService struct {
	client         LLMClient
	rag            RAGRetriever
	emr            *EMRAdapter
	model          string
	logger         *logging.Logger
	history        *historyStore
	deposit        depositConfig
	leadsRepo      leads.Repository
	clinicStore    *clinic.Store
	audit          *compliance.AuditService
	paymentChecker PaymentStatusChecker
	faqClassifier  *FAQClassifier
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
		client:        client,
		rag:           rag,
		model:         model,
		logger:        logger,
		history:       newHistoryStore(redisClient, llmTracer),
		faqClassifier: NewFAQClassifier(client),
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

	// Get clinic-configured deposit amount for system prompt customization
	depositCents := s.deposit.DefaultAmountCents
	if s.clinicStore != nil && req.OrgID != "" {
		if cfg, err := s.clinicStore.Get(ctx, req.OrgID); err == nil && cfg != nil {
			if cfg.DepositAmountCents > 0 {
				depositCents = int32(cfg.DepositAmountCents)
			}
		}
	}
	systemPrompt := buildSystemPrompt(int(depositCents))

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

	reply, err := s.generateResponse(ctx, history)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	history = append(history, ChatMessage{
		Role:    ChatRoleAssistant,
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
				// Get clinic-configured deposit amount for system prompt
				depositCents := s.deposit.DefaultAmountCents
				if s.clinicStore != nil && req.OrgID != "" {
					if cfg, err := s.clinicStore.Get(ctx, req.OrgID); err == nil && cfg != nil {
						if cfg.DepositAmountCents > 0 {
							depositCents = int32(cfg.DepositAmountCents)
						}
					}
				}
				history := []ChatMessage{
					{Role: ChatRoleSystem, Content: buildSystemPrompt(int(depositCents))},
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
				// Get clinic-configured deposit amount for system prompt
				depositCents := s.deposit.DefaultAmountCents
				if s.clinicStore != nil && req.OrgID != "" {
					if cfg, err := s.clinicStore.Get(ctx, req.OrgID); err == nil && cfg != nil {
						if cfg.DepositAmountCents > 0 {
							depositCents = int32(cfg.DepositAmountCents)
						}
					}
				}
				history := []ChatMessage{
					{Role: ChatRoleSystem, Content: buildSystemPrompt(int(depositCents))},
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
				reply := fmt.Sprintf("%s pricing: %s. To secure priority booking, we collect a small refundable deposit of $%.0f that applies toward your treatment. Would you like to proceed?", strings.Title(service), price, depositDollars)
				// Best-effort tagging for analytics/triage.
				s.appendLeadNote(ctx, req.OrgID, req.LeadID, "tag:price_shopper")

				history = append(history, ChatMessage{Role: ChatRoleAssistant, Content: reply})
				history = trimHistory(history, maxHistoryMessages)
				if err := s.history.Save(ctx, req.ConversationID, history); err != nil {
					span.RecordError(err)
					return nil, err
				}
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
			return &Response{ConversationID: req.ConversationID, Message: faqReply, Timestamp: time.Now().UTC()}, nil
		}

		s.logger.Info("FAQ: no match from classifier or regex, falling through to full LLM")
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

	var depositIntent *DepositIntent
	if latestTurnAgreedToDeposit(history) {
		// Deterministic fallback: if the user explicitly agrees to a deposit in their message,
		// send a deposit intent even if the classifier is skipped or errors.
		depositIntent = &DepositIntent{
			AmountCents: s.deposit.DefaultAmountCents,
			Description: s.deposit.Description,
			SuccessURL:  s.deposit.SuccessURL,
			CancelURL:   s.deposit.CancelURL,
		}
		s.logger.Info("deposit intent inferred from explicit user agreement", "amount_cents", depositIntent.AmountCents)
	} else if shouldAttemptDepositClassification(history) {
		extracted, derr := s.extractDepositIntent(ctx, history)
		if derr != nil {
			span.RecordError(derr)
			s.logger.Warn("deposit intent extraction failed", "error", derr)
		} else if extracted != nil {
			s.logger.Info("deposit intent extracted", "amount_cents", extracted.AmountCents)
		} else {
			s.logger.Debug("no deposit intent detected")
		}
		depositIntent = extracted
	} else {
		s.logger.Debug("deposit: classifier skipped (no deposit context)")
		depositIntent = nil
	}

	// Extract and save scheduling preferences if lead ID is provided
	if req.LeadID != "" && s.leadsRepo != nil {
		if err := s.extractAndSavePreferences(ctx, req.LeadID, history); err != nil {
			s.logger.Warn("failed to save scheduling preferences", "lead_id", req.LeadID, "error", err)
		}
	}

	// Enforce clinic-configured deposit amounts (override LLM amounts when a rule exists).
	if depositIntent != nil && s.clinicStore != nil && req.OrgID != "" {
		if cfg, err := s.clinicStore.Get(ctx, req.OrgID); err == nil && cfg != nil {
			if prefs, ok := extractPreferences(history); ok && prefs.ServiceInterest != "" {
				if amount := cfg.DepositAmountForService(prefs.ServiceInterest); amount > 0 {
					depositIntent.AmountCents = int32(amount)
				}
			}
		}
	}

	return &Response{
		ConversationID: req.ConversationID,
		Message:        reply,
		Timestamp:      time.Now().UTC(),
		DepositIntent:  depositIntent,
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
	msg = regexp.MustCompile(`\*([^\s*][^*]*[^\s*])\*`).ReplaceAllString(msg, "$1")
	// Remove markdown bullet points at start of lines: "- item" -> "item"
	msg = regexp.MustCompile(`(?m)^[\s]*[-•]\s+`).ReplaceAllString(msg, "")
	// Remove numbered list formatting: "1. item" -> "item"
	msg = regexp.MustCompile(`(?m)^[\s]*\d+\.\s+`).ReplaceAllString(msg, "")
	// Clean up any double spaces that might result
	msg = regexp.MustCompile(`\s{2,}`).ReplaceAllString(msg, " ")
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
	prefs, ok := extractPreferences(history)
	if !ok {
		return nil
	}
	prefs.Notes = fmt.Sprintf("Auto-extracted from conversation at %s", time.Now().Format(time.RFC3339))
	return s.leadsRepo.UpdateSchedulingPreferences(ctx, leadID, prefs)
}

func extractPreferences(history []ChatMessage) (leads.SchedulingPreferences, bool) {
	prefs := leads.SchedulingPreferences{}
	hasPreferences := false

	extractName := func(raw string) (string, string) {
		words := strings.Fields(strings.TrimSpace(raw))
		nameWords := make([]string, 0, 2)
		for _, word := range words {
			cleaned := strings.Trim(word, ".,!?")
			if cleaned == "" {
				continue
			}
			if len(cleaned) < 2 || len(cleaned) > 30 || !isCapitalized(cleaned) || isCommonWord(cleaned) {
				if len(nameWords) > 0 {
					break
				}
				continue
			}
			nameWords = append(nameWords, cleaned)
			if len(nameWords) == 2 {
				break
			}
		}
		if len(nameWords) >= 2 {
			return strings.Join(nameWords, " "), nameWords[0]
		}
		if len(nameWords) == 1 {
			return "", nameWords[0]
		}
		return "", ""
	}

	// Extract preferred days (only from user messages to avoid confusion with business hours).
	userMessages := ""
	userMessagesOriginal := "" // Keep original case for name extraction.
	for _, msg := range history {
		if msg.Role == ChatRoleUser {
			userMessages += strings.ToLower(msg.Content) + " "
			userMessagesOriginal += msg.Content + " "
		}
	}

	// Extract patient name from user messages.
	namePatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)my name is\s+([A-Z][a-zA-Z'-]+(?:\s+[A-Z][a-zA-Z'-]+){0,2})`),
		regexp.MustCompile(`(?i)i'?m\s+([A-Z][a-zA-Z'-]+(?:\s+[A-Z][a-zA-Z'-]+){0,2})(?:\s|,|\.|!|$)`),
		regexp.MustCompile(`(?i)this is\s+([A-Z][a-zA-Z'-]+(?:\s+[A-Z][a-zA-Z'-]+){0,2})`),
		regexp.MustCompile(`(?i)call me\s+([A-Z][a-zA-Z'-]+(?:\s+[A-Z][a-zA-Z'-]+){0,2})`),
		regexp.MustCompile(`(?i)it'?s\s+([A-Z][a-zA-Z'-]+(?:\s+[A-Z][a-zA-Z'-]+){0,2})(?:\s|,|\.|!|$)`),
		regexp.MustCompile(`(?i)name'?s\s+([A-Z][a-zA-Z'-]+(?:\s+[A-Z][a-zA-Z'-]+){0,2})`),
	}
	firstNameFallback := ""
	for _, pattern := range namePatterns {
		if matches := pattern.FindStringSubmatch(userMessagesOriginal); len(matches) > 1 {
			fullName, firstName := extractName(matches[1])
			if fullName != "" {
				prefs.Name = fullName
				hasPreferences = true
				break
			}
			if firstNameFallback == "" && firstName != "" {
				firstNameFallback = firstName
			}
		}
	}
	if prefs.Name == "" && firstNameFallback != "" {
		prefs.Name = firstNameFallback
		hasPreferences = true
	}

	// Standalone name response after an explicit name ask.
	if prefs.Name == "" {
		for i, msg := range history {
			if msg.Role != ChatRoleUser {
				continue
			}
			if i == 0 || history[i-1].Role != ChatRoleAssistant {
				continue
			}
			prev := strings.ToLower(history[i-1].Content)
			if !strings.Contains(prev, "name") || (!strings.Contains(prev, "may i") && !strings.Contains(prev, "what") && !strings.Contains(prev, "your")) {
				continue
			}
			content := strings.TrimSpace(msg.Content)
			words := strings.Fields(content)
			if len(words) < 1 || len(words) > 5 {
				continue
			}
			fullName, firstName := extractName(content)
			if fullName != "" {
				prefs.Name = fullName
				hasPreferences = true
				break
			}
			if firstName != "" && len(words) <= 2 {
				prefs.Name = firstName
				hasPreferences = true
				break
			}
		}
	}

	// Extract patient type.
	if strings.Contains(userMessages, "new patient") || strings.Contains(userMessages, "first time") || strings.Contains(userMessages, "i'm new") || strings.Contains(userMessages, "i am new") {
		prefs.PatientType = "new"
		hasPreferences = true
	} else if strings.Contains(userMessages, "returning") || strings.Contains(userMessages, "existing patient") || strings.Contains(userMessages, "i've been") || strings.Contains(userMessages, "i have been") {
		prefs.PatientType = "existing"
		hasPreferences = true
	}
	if prefs.PatientType == "" {
		if patientType := patientTypeFromShortReply(history); patientType != "" {
			prefs.PatientType = patientType
			hasPreferences = true
		}
	}

	// Extract service interest from user messages (users may answer with just a service name).
	services := []string{"botox", "filler", "dermal filler", "consultation", "laser", "facial", "peel", "microneedling"}
	for _, service := range services {
		if strings.Contains(userMessages, service) {
			prefs.ServiceInterest = service
			hasPreferences = true
			break
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

	return prefs, hasPreferences
}

var (
	priceInquiryRE     = regexp.MustCompile(`(?i)\b(?:how much|price|pricing|cost|rate|rates|charge)\b`)
	phiPrefaceRE       = regexp.MustCompile(`(?i)\b(?:diagnosed|diagnosis|my condition|my symptoms|i have|i've had|i am|i'm)\b`)
	medicalAdviceCueRE = regexp.MustCompile(`(?i)\b(?:should i|can i|is it safe|safe to|ok to|okay to|contraindications?|side effects?|dosage|dose|mg|milligram|interactions?|mix with|stop taking)\b`)
	medicalContextRE   = regexp.MustCompile(`(?i)\b(?:botox|filler|laser|microneedling|facial|peel|dermaplaning|prp|injectable|medication|medicine|meds|prescription|ibuprofen|tylenol|acetaminophen|antibiotics?|painkillers?|blood pressure|pregnan(?:t|cy)|breastfeed(?:ing)?|allerg(?:y|ic))\b`)
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
	// Minimal deterministic PHI keywords for deflection; expand as needed.
	for _, kw := range []string{
		"diabetes", "hiv", "aids", "cancer", "hepatitis", "pregnant", "pregnancy",
		"depression", "anxiety", "bipolar", "schizophrenia", "asthma", "hypertension",
		"blood pressure", "infection", "herpes", "std", "sti",
	} {
		if strings.Contains(message, kw) {
			return true
		}
	}
	return false
}

func detectMedicalAdvice(message string) []string {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" {
		return nil
	}
	if !medicalAdviceCueRE.MatchString(message) {
		return nil
	}
	if !medicalContextRE.MatchString(message) {
		return nil
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

func patientTypeFromShortReply(history []ChatMessage) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != ChatRoleUser {
			continue
		}
		reply := normalizePatientTypeReply(history[i].Content)
		if reply == "" {
			continue
		}
		if !assistantAskedPatientType(history, i) {
			continue
		}
		return reply
	}
	return ""
}

func normalizePatientTypeReply(message string) string {
	cleaned := strings.ToLower(strings.TrimSpace(message))
	cleaned = strings.Trim(cleaned, ".,!?")
	switch cleaned {
	case "new", "new patient", "new here", "first time", "first-time", "never been", "never been before", "i'm new", "im new", "i am new":
		return "new"
	case "existing", "returning", "existing patient", "returning patient", "been before", "i've been before", "i have been before", "not new":
		return "existing"
	default:
		return ""
	}
}

func assistantAskedPatientType(history []ChatMessage, userIndex int) bool {
	prev := previousAssistantMessage(history, userIndex)
	if prev == "" {
		return false
	}
	content := strings.ToLower(prev)
	if strings.Contains(content, "new patient") || strings.Contains(content, "existing patient") || strings.Contains(content, "returning patient") {
		return true
	}
	if strings.Contains(content, "visited") && strings.Contains(content, "before") {
		return true
	}
	if strings.Contains(content, "been") && strings.Contains(content, "before") {
		return true
	}
	if strings.Contains(content, "new") && (strings.Contains(content, "existing") || strings.Contains(content, "returning")) {
		return true
	}
	if strings.Contains(content, "are you new") && (strings.Contains(content, "patient") || strings.Contains(content, "here") || strings.Contains(content, "before")) {
		return true
	}
	return false
}

func previousAssistantMessage(history []ChatMessage, start int) string {
	for i := start - 1; i >= 0; i-- {
		if history[i].Role == ChatRoleSystem {
			continue
		}
		if history[i].Role != ChatRoleAssistant {
			return ""
		}
		return history[i].Content
	}
	return ""
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
