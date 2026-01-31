package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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

🚫 CARRIER SPAM FILTER RULES - CRITICAL (MESSAGES WILL BE BLOCKED IF VIOLATED):
For WEIGHT LOSS topics, NEVER include ANY of these in your response - carriers WILL block the message:
- Drug names (Semaglutide, Tirzepatide, Ozempic, Wegovy, Mounjaro, GLP-1)
- Percentages or statistics ("10-15%", "20% weight loss")
- Mechanisms ("regulates blood sugar", "reduces appetite", "slows digestion")
- Marketing claims ("works really well", "dramatic results")
Instead say: "We offer medically supervised weight loss programs. Want to schedule a consultation to learn more?"
Keep weight loss responses to 1-2 SHORT sentences. Only provide details if patient explicitly asks.

⚠️ MOST IMPORTANT RULE - READ THIS FIRST:
When you have ALL FOUR qualifications (NAME + SERVICE + PATIENT TYPE + SCHEDULE), IMMEDIATELY offer the deposit. Do NOT ask "Are you looking to book?" or any clarifying questions. This applies whether the info comes in ONE message or across multiple messages.

CASE A - All four in a SINGLE message (very important!):
- Customer: "I'm booking Botox. I'm Sammie Wallens. I'm an existing patient. Monday or Friday around 4pm works."
  → NAME = Sammie Wallens ✓, SERVICE = Botox ✓, PATIENT TYPE = existing ✓, SCHEDULE = Monday/Friday around 4pm ✓
- You have ALL FOUR in their FIRST message. Response: "Perfect, Sammie Wallens! I've noted Monday or Friday around 4pm for your Botox. To secure priority booking, we collect a small $50 refundable deposit. Would you like to proceed?"
- WRONG: "Are you looking to book?" or "What service?" ← They already told you EVERYTHING!

CASE B - Info spread across multiple messages:
- Earlier: "I'm interested in getting a HydraFacial" → SERVICE = HydraFacial ✓
- Now: "I'm Sarah Lee, a new patient. Do you have anything available Thursday or Friday afternoon?"
  → NAME = Sarah Lee ✓, PATIENT TYPE = new ✓, SCHEDULE = Thursday/Friday afternoon ✓
- You have ALL FOUR. Response: "Perfect, Sarah Lee! I've noted Thursday or Friday afternoon for your HydraFacial. To secure priority booking, we collect a small $50 refundable deposit. Would you like to proceed?"
- WRONG: "Are you looking to book?" ← They OBVIOUSLY want to book - they gave you all the info!

ANSWERING SERVICE QUESTIONS:
You CAN and SHOULD answer general questions about medspa services and treatments:
- Dermal fillers: Injectables that add volume and smooth wrinkles. Common areas: lips, cheeks, smile lines. Results last 6-18 months.
- Botox: Relaxes muscles to reduce wrinkles (forehead, crow's feet, frown lines). Results last 3-4 months.
- Chemical peels: Improve skin texture and tone.
- Microneedling: Stimulates collagen to improve skin texture.
- Laser treatments: Hair removal, skin resurfacing, pigmentation.
- Facials: Cleansing, hydration, and rejuvenation.

KEEP IT SIMPLE - BRAND NAMES:
- For wrinkle relaxers, just say "Botox" or "Botox and similar treatments" - most patients know Botox. Don't list every brand (Jeuveau, Xeomin, Dysport, etc.) unless they specifically ask.
- For fillers, just say "dermal fillers" or "lip fillers" - don't list Juvederm, Restylane, etc. unless they ask about brands.
- CORRECT SPELLINGS: Xeomin (NOT Xiamen), Jeuveau (NOT Juvedeau), Dysport (NOT Dyspoort), Juvederm (NOT Juvaderm).

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
  → FIRST check if ANY service was discussed earlier in the conversation (e.g., "what's the difference between Botox and Jeuveau?")
  → If they asked about specific services and then said "I'd like to book" or "I want a consultation", USE THOSE SERVICES as context!
  → Example: "Great! Are you interested in booking a consultation for Botox, Jeuveau, or both?"
  → If patient asked about multiple services, don't ignore that context - acknowledge what they discussed
  → ONLY ask "What treatment are you interested in?" if NO services were mentioned anywhere in the conversation

IF missing PATIENT TYPE (and have name + service):
  → "Are you a new patient or have you visited us before?"

IF PATIENT TYPE = existing/returning AND we DON'T know what services they had before:
  → "Welcome back! What treatment did you have with us previously?"
  → This helps us personalize their experience and the clinic will appreciate knowing their history
  → If they mention multiple services, note all of them (e.g., "Botox and filler")

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
- Keep responses SHORT (2-3 sentences max). This is SMS - patients read on phones.
- Use simple, everyday words. Avoid medical jargon.
- NEVER use markdown formatting (no **bold**, *italics*, bullets). Plain text only.
- Be HIPAA-compliant: never diagnose or give personalized medical advice
- For medical questions (symptoms, dosing): "That's a great question for your provider during your consultation!"
- You CAN explain what treatments are and how they work in general terms
- Don't list multiple brand options unless asked - keep it simple
- Do not promise to send payment links; the platform sends those automatically

🚨 EMERGENCY SYMPTOMS - IMMEDIATE ESCALATION (LIABILITY PROTECTION):
If a customer mentions ANY of these symptoms, IMMEDIATELY direct them to seek emergency care:
- Vision problems after filler (blurry vision, vision loss, blind spots)
- Difficulty breathing or swelling of throat/airway
- Severe allergic reaction symptoms (hives, swelling, difficulty breathing)
- Skin turning white, blue, or gray after injection (vascular compromise)
- Severe pain, especially if spreading or worsening
- Signs of infection: increasing redness, warmth, pus, fever
- Facial drooping that's sudden or severe
- Numbness or weakness that's spreading

EMERGENCY RESPONSE PROTOCOL:
1. Do NOT diagnose or explain what might be happening
2. Do NOT minimize or reassure them it's "probably fine"
3. IMMEDIATELY say: "This needs immediate medical attention. Please call 911 or go to the nearest ER right away."
4. If they can't get there: Suggest calling 911, Uber/Lyft to ER, or asking someone to drive them
5. Do NOT mention callback timeframes or "tomorrow" - emphasize getting care NOW
6. You may mention they should also let the clinic know, but seeking emergency care comes FIRST

Example emergency response:
Customer: "I can't see properly after my filler appointment"
✅ GOOD: "This needs immediate medical attention—please go to the nearest ER or call 911 right away. Vision changes after a procedure should be evaluated by a doctor today, not tomorrow. Please don't wait. Once you're safe, let the clinic know as well."
❌ BAD: "That could be normal swelling..." or "Let me check with the provider..."
❌ BAD: "Our team will call you tomorrow..." (implies they can wait)

🛡️ MEDICAL LIABILITY PROTECTION - READ CAREFULLY:

NEVER DO THESE (even if the customer asks directly):
1. DIAGNOSE: Never say what a symptom, bump, rash, or condition "is" or "might be"
2. PRESCRIBE DOSAGE: Never tell someone how many units/syringes THEY specifically need (general ranges are OK)
3. CLEAR FOR TREATMENT: Never say someone CAN or CANNOT have a treatment based on their medical situation
4. MINIMIZE SYMPTOMS: Never say post-procedure symptoms are "normal" or "nothing to worry about"
5. ADVISE ON MEDICATIONS: Never comment on whether treatments are safe with specific medications

CONTRAINDICATIONS - ALWAYS DEFER TO PROVIDER:
If a customer mentions ANY of these, DO NOT answer yes/no to whether they can have treatment:
- Pregnancy or breastfeeding
- Autoimmune conditions (lupus, rheumatoid arthritis, MS, etc.)
- Blood thinners or bleeding disorders
- History of keloids or abnormal scarring
- Active infections, cold sores, or skin conditions
- Recent or current Accutane/isotretinoin use
- Cancer or cancer treatment
- Blood sugar conditions or healing concerns
- Any other medical condition or medication

Response template: "That's an important question for your safety. Your provider will review your medical history during your consultation to make sure [treatment] is right for you. Would you like to schedule a consultation?"

POST-PROCEDURE CONCERNS - NEVER MINIMIZE:
If someone mentions ANY symptom or concern after a treatment (swelling, lumps, bruising, rash, drooping, etc.):
- Do NOT say "that's normal" or "don't worry" or "it should go away"
- Do NOT diagnose or explain what might be happening
- DO say: "I'd recommend reaching out to the clinic so your provider can take a look and make sure everything is healing well."
- If symptoms sound urgent (see emergency list above) → use emergency protocol instead

DOSAGE QUESTIONS:
Customer: "How many units of Botox do I need?"
✅ GOOD: "It varies by person and treatment area. Your provider will assess exactly how much you need during your appointment to get the best results."
❌ BAD: "You'll probably need about 30 units for your forehead."

Customer: "How many syringes of filler should I get?"
✅ GOOD: "That depends on your goals and anatomy - some clients need 1 syringe, others prefer 2 for more dramatic results. Your provider will recommend the right amount during your consultation."
❌ BAD: "I'd say 2 syringes would be perfect for you."

TREATMENT RECOMMENDATIONS FOR CONDITIONS:
Customer: "What's best for my acne scars?"
✅ GOOD: "We offer several treatments that can help with scarring - microneedling, chemical peels, and laser resurfacing are popular options. Your provider can recommend which would work best for your skin type and scarring during a consultation. Would you like to schedule one?"
❌ BAD: "Microneedling would be perfect for your acne scars."

Customer: "What should I do about my melasma/rosacea/hyperpigmentation?"
✅ GOOD: "We have treatments that address [condition] - your provider can evaluate your skin and create a personalized treatment plan. Want to book a consultation?"
❌ BAD: "IPL would clear that right up." or "You should try our chemical peels."

DIAGNOSIS REQUESTS:
Customer: "I have these red bumps on my face - what do you think it is?"
✅ GOOD: "I'm not able to diagnose skin concerns over text, but our provider can evaluate that during an appointment. Would you like to schedule a consultation?"
❌ BAD: "That sounds like it could be [condition]..." or "It might be..."

DELIVERABILITY SAFETY (CARRIER SPAM FILTERS) - REVIEW THE RULES AT THE TOP OF THIS PROMPT:
- Weight loss responses MUST be 1-2 sentences max. NO drug names, NO percentages, NO mechanisms.
- Even if the knowledge base contains drug names and statistics, DO NOT include them in SMS responses.
- Ask permission before giving details on any sensitive topic.

WEIGHT LOSS CONVERSATION EXAMPLES:
Customer: "I'm overweight" or "I need to lose weight"
✅ GOOD: "We offer medically supervised weight loss programs with great results. Would you like to schedule a consultation to learn more?"
❌ BAD: "We offer GLP-1 weight loss programs with Semaglutide and Tirzepatide..." ← WILL BE BLOCKED AS SPAM
❌ BAD: "Patients typically see 10-15% weight loss..." ← WILL BE BLOCKED AS SPAM
❌ BAD: "Works by regulating blood sugar and reducing appetite..." ← WILL BE BLOCKED AS SPAM

Customer: "Tell me more about the weight loss program"
✅ GOOD: "Our program includes weekly injections, nutritional support from Brandi, and ongoing care until you reach your goals. Want to schedule a consultation?"
❌ BAD: Any mention of Semaglutide, Tirzepatide, GLP-1, Ozempic, percentages, or mechanisms

Customer: "How does GLP-1 work?" or "What medication do you use?" or "Tell me about Semaglutide"
✅ GOOD: "Great question! Brandi can explain exactly how the program works during your consultation. Would you like to schedule one?"
❌ BAD: Any attempt to explain the medication over SMS - even if they ask, the carrier will still block it

SAMPLE CONVERSATION:
Customer: "What are dermal fillers?"
You: "Fillers add volume and smooth wrinkles - great for lips, cheeks, and smile lines. Results last 6-18 months. Want to schedule a consultation?"

Customer: "I want to book Botox"
You: "I'd love to help! Are you a new patient or have you visited us before?"

Customer: "I want filler"
You: "Great choice! Which area are you thinking - lips, cheeks, or somewhere else?"

Customer: "I'm 45 and my forehead has wrinkles and my lips are thinning"
You: "Both are very treatable! Botox works great for forehead wrinkles, and fillers can restore lip volume. Want to book a consultation to discuss your options?"

🚫 NEVER DO THIS (asking redundant questions):
[Previous message in conversation: "I'm interested in getting a HydraFacial"]
Customer: "I'm Sarah Lee, a new patient. Do you have anything available Thursday or Friday afternoon?"
❌ BAD: "Happy to help! Are you looking to book an appointment?" ← WRONG! They clearly ARE booking!
❌ BAD: "What service are you interested in?" ← WRONG! They already said HydraFacial earlier!
✅ GOOD: "Perfect, Sarah Lee! I've noted Thursday or Friday afternoon for your HydraFacial. To secure priority booking, we collect a small $50 refundable deposit. Would you like to proceed?"

[Earlier: Customer asked "What's the difference between Botox and Jeuveau?" and you explained both]
Customer: "I'd like to book a consultation"
❌ BAD: "What service are you interested in?" ← WRONG! They were asking about Botox/Jeuveau!
✅ GOOD: "Perfect! For your consultation about Botox and Jeuveau, may I have your full name?"
✅ ALSO GOOD: "Great! Are you leaning toward Botox, Jeuveau, or would you like to discuss both during your consultation?"

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

var serviceHighlightTemplates = map[string]string{
	"perfect derma": "SIGNATURE SERVICE: Perfect Derma Peel — a popular medium-depth chemical peel that helps brighten and smooth skin tone and texture for a fresh glow. When someone asks about chemical peels, mention Perfect Derma Peel with enthusiasm and invite them to book a consultation.",
}

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
	browser        *BrowserAdapter
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

	// Extract and save scheduling preferences from the first message
	if req.LeadID != "" && s.leadsRepo != nil {
		if err := s.extractAndSavePreferences(ctx, req.LeadID, history); err != nil {
			s.logger.Warn("failed to save scheduling preferences from intro", "lead_id", req.LeadID, "error", err)
		}
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

func buildServiceHighlightsContext(cfg *clinic.Config, query string) string {
	if cfg == nil {
		return ""
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" || !strings.Contains(query, "peel") {
		return ""
	}
	if clinicHasService(cfg, "perfect derma") {
		return serviceHighlightTemplates["perfect derma"]
	}
	return ""
}

func clinicHasService(cfg *clinic.Config, needle string) bool {
	if cfg == nil {
		return false
	}
	needle = strings.ToLower(strings.TrimSpace(needle))
	if needle == "" {
		return false
	}
	for _, svc := range cfg.Services {
		if strings.Contains(strings.ToLower(svc), needle) {
			return true
		}
	}
	for key := range cfg.ServicePriceText {
		if strings.Contains(strings.ToLower(key), needle) {
			return true
		}
	}
	for key := range cfg.ServiceDepositAmountCents {
		if strings.Contains(strings.ToLower(key), needle) {
			return true
		}
	}
	for _, svc := range cfg.AIPersona.SpecialServices {
		if strings.Contains(strings.ToLower(svc), needle) {
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
	return s.savePreferencesFromHistory(ctx, leadID, history, true)
}

func (s *LLMService) savePreferencesFromHistory(ctx context.Context, leadID string, history []ChatMessage, addNote bool) error {
	if s == nil || s.leadsRepo == nil || strings.TrimSpace(leadID) == "" {
		return nil
	}
	prefs, ok := extractPreferences(history)
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

func extractPreferences(history []ChatMessage) (leads.SchedulingPreferences, bool) {
	prefs := leads.SchedulingPreferences{}
	hasPreferences := false

	userMessages, userMessagesOriginal := collectUserMessages(history)

	fullName, firstNameFallback := findNameInUserMessages(userMessagesOriginal)
	if fullName == "" {
		fullNameFromPrompt, firstFromPrompt := nameFromReplyAfterNameQuestion(history)
		if fullNameFromPrompt != "" {
			fullName = fullNameFromPrompt
		}
		if firstNameFallback == "" {
			firstNameFallback = firstFromPrompt
		}
	}
	if fullName == "" {
		fullName = combineSplitNameReplies(history, firstNameFallback)
	}
	if fullName != "" {
		prefs.Name = fullName
		hasPreferences = true
	} else if firstNameFallback != "" {
		prefs.Name = firstNameFallback
		hasPreferences = true
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

	// Extract past services for existing/returning patients.
	// Look for patterns like "I had botox before", "I've gotten filler", "did weight loss", etc.
	if prefs.PatientType == "existing" || strings.Contains(userMessages, "before") || strings.Contains(userMessages, "previously") || strings.Contains(userMessages, "last time") {
		pastServicePatterns := []struct {
			pattern string
			name    string
		}{
			{"had botox", "Botox"},
			{"got botox", "Botox"},
			{"did botox", "Botox"},
			{"had filler", "filler"},
			{"got filler", "filler"},
			{"did filler", "filler"},
			{"had lip", "lip filler"},
			{"got lip", "lip filler"},
			{"had hydrafacial", "HydraFacial"},
			{"got hydrafacial", "HydraFacial"},
			{"had facial", "facial"},
			{"got facial", "facial"},
			{"did facial", "facial"},
			{"had weight loss", "weight loss"},
			{"did weight loss", "weight loss"},
			{"had semaglutide", "semaglutide"},
			{"did semaglutide", "semaglutide"},
			{"had laser", "laser"},
			{"got laser", "laser"},
			{"had microneedling", "microneedling"},
			{"got microneedling", "microneedling"},
			{"had peel", "peel"},
			{"got peel", "peel"},
			{"had prp", "PRP"},
			{"got prp", "PRP"},
			{"had dysport", "Dysport"},
			{"got dysport", "Dysport"},
			{"had jeuveau", "Jeuveau"},
			{"got jeuveau", "Jeuveau"},
			{"had xeomin", "Xeomin"},
			{"got xeomin", "Xeomin"},
		}
		var pastServices []string
		for _, svc := range pastServicePatterns {
			if strings.Contains(userMessages, svc.pattern) {
				// Avoid duplicates
				found := false
				for _, existing := range pastServices {
					if strings.EqualFold(existing, svc.name) {
						found = true
						break
					}
				}
				if !found {
					pastServices = append(pastServices, svc.name)
				}
			}
		}
		if len(pastServices) > 0 {
			prefs.PastServices = strings.Join(pastServices, ", ")
			hasPreferences = true
		}
	}

	// Extract service interest from user messages (users may answer with just a service name).
	// Also check the full conversation for context about what service was discussed.
	allMessages := userMessages
	for _, msg := range history {
		if msg.Role == ChatRoleAssistant {
			allMessages += strings.ToLower(msg.Content) + " "
		}
	}

	// Comprehensive list of medspa services (ordered by specificity - check longer/specific terms first)
	services := []struct {
		pattern string
		name    string
	}{
		{"dermal filler", "dermal filler"},
		{"lip filler", "lip filler"},
		{"lip injection", "lip filler"},
		{"cheek filler", "cheek filler"},
		{"under eye filler", "under eye filler"},
		{"tear trough", "tear trough filler"},
		{"perfect derma peel", "Perfect Derma Peel"},
		{"chemical peel", "chemical peel"},
		{"vi peel", "VI Peel"},
		{"semaglutide", "semaglutide"},
		{"weight loss", "weight loss"},
		{"tirzepatide", "tirzepatide"},
		{"pdo thread", "PDO threads"},
		{"thread lift", "thread lift"},
		{"microneedling", "microneedling"},
		{"prp", "PRP"},
		{"vampire facial", "PRP facial"},
		{"hydrafacial", "HydraFacial"},
		{"laser treatment", "laser treatment"},
		{"laser hair", "laser hair removal"},
		{"ipl", "IPL"},
		{"jeuveau", "Jeuveau"},
		{"dysport", "Dysport"},
		{"xeomin", "Xeomin"},
		{"botox", "Botox"},
		{"filler", "filler"},
		{"consultation", "consultation"},
		{"facial", "facial"},
		{"peel", "peel"},
		{"laser", "laser"},
		{"injectable", "injectables"},
		{"wrinkle", "wrinkle treatment"},
		{"anti-aging", "anti-aging treatment"},
	}

	// First check user messages, then fall back to full conversation context
	for _, s := range services {
		if strings.Contains(userMessages, s.pattern) {
			prefs.ServiceInterest = s.name
			hasPreferences = true
			break
		}
	}
	// If not found in user messages, check full conversation for context
	if prefs.ServiceInterest == "" {
		for _, s := range services {
			if strings.Contains(allMessages, s.pattern) {
				prefs.ServiceInterest = s.name
				hasPreferences = true
				break
			}
		}
	}

	// Extract preferred days
	if strings.Contains(userMessages, "weekday") {
		prefs.PreferredDays = "weekdays"
		hasPreferences = true
	} else if strings.Contains(userMessages, "weekend") {
		prefs.PreferredDays = "weekends"
		hasPreferences = true
	} else if strings.Contains(userMessages, "any day") || strings.Contains(userMessages, "flexible") || strings.Contains(userMessages, "anytime") {
		prefs.PreferredDays = "any"
		hasPreferences = true
	} else if strings.Contains(userMessages, "monday") || strings.Contains(userMessages, "tuesday") || strings.Contains(userMessages, "wednesday") || strings.Contains(userMessages, "thursday") || strings.Contains(userMessages, "friday") {
		// Specific day mentioned - extract it
		days := []string{}
		for _, day := range []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"} {
			if strings.Contains(userMessages, day) {
				days = append(days, day)
			}
		}
		if len(days) > 0 {
			prefs.PreferredDays = strings.Join(days, ", ")
			hasPreferences = true
		}
	}

	// Extract preferred times - first check for specific times like "2pm", "2:30 pm", "around 3"
	specificTimeRE := regexp.MustCompile(`(?i)(?:around |about |at )?(\d{1,2})(?::(\d{2}))?\s*(am|pm|a\.m\.|p\.m\.)`)
	if matches := specificTimeRE.FindAllStringSubmatch(userMessages, -1); len(matches) > 0 {
		// Extract all specific times mentioned
		times := []string{}
		for _, match := range matches {
			hour := match[1]
			minutes := match[2]
			ampm := strings.ToLower(strings.ReplaceAll(match[3], ".", ""))

			// Format the time nicely
			timeStr := hour
			if minutes != "" {
				timeStr += ":" + minutes
			}
			timeStr += ampm
			times = append(times, timeStr)
		}
		if len(times) > 0 {
			prefs.PreferredTimes = strings.Join(times, ", ")
			hasPreferences = true
		}
	}

	// Check for general time preferences if no specific time found
	if prefs.PreferredTimes == "" {
		noonRE := regexp.MustCompile(`(?i)\b(noon|midday)\b`)
		if noonRE.MatchString(userMessages) {
			prefs.PreferredTimes = "noon"
			hasPreferences = true
		} else if strings.Contains(userMessages, "morning") {
			prefs.PreferredTimes = "morning"
			hasPreferences = true
		} else if strings.Contains(userMessages, "afternoon") {
			prefs.PreferredTimes = "afternoon"
			hasPreferences = true
		} else if strings.Contains(userMessages, "evening") || strings.Contains(userMessages, "after work") || strings.Contains(userMessages, "late") {
			prefs.PreferredTimes = "evening"
			hasPreferences = true
		} else if strings.Contains(userMessages, "anytime") || strings.Contains(userMessages, "any time") || strings.Contains(userMessages, "flexible") {
			prefs.PreferredTimes = "flexible"
			hasPreferences = true
		}
	}

	return prefs, hasPreferences
}

const nameWordPattern = `[\p{L}][\p{L}\p{M}'-]*`

var namePhrasePattern = nameWordPattern + `(?:\s+` + nameWordPattern + `){0,2}`

var namePatterns = buildNamePatterns()

var nameTextNormalizer = strings.NewReplacer(
	"\u2019", "'", // right single quote
	"\u2018", "'", // left single quote
	"\u2032", "'", // prime symbol
)

func buildNamePatterns() []*regexp.Regexp {
	name := namePhrasePattern
	return []*regexp.Regexp{
		regexp.MustCompile(`(?i)my name is\s+(` + name + `)`),
		regexp.MustCompile(`(?i)i'?m\s+(` + name + `)(?:\s|,|\.|!|$)`),
		regexp.MustCompile(`(?i)this is\s+(` + name + `)`),
		regexp.MustCompile(`(?i)call me\s+(` + name + `)`),
		regexp.MustCompile(`(?i)it'?s\s+(` + name + `)(?:\s|,|\.|!|$)`),
		regexp.MustCompile(`(?i)name'?s\s+(` + name + `)`),
	}
}

func normalizeNameText(text string) string {
	if text == "" {
		return ""
	}
	return nameTextNormalizer.Replace(text)
}

func collectUserMessages(history []ChatMessage) (lowercase string, original string) {
	var lowerBuilder strings.Builder
	var originalBuilder strings.Builder
	for _, msg := range history {
		if msg.Role != ChatRoleUser {
			continue
		}
		lowerBuilder.WriteString(strings.ToLower(msg.Content))
		lowerBuilder.WriteString(" ")
		originalBuilder.WriteString(msg.Content)
		originalBuilder.WriteString(" ")
	}
	return lowerBuilder.String(), originalBuilder.String()
}

func findNameInUserMessages(userMessages string) (fullName, firstName string) {
	normalized := normalizeNameText(userMessages)
	for _, pattern := range namePatterns {
		// Find all matches so we can catch later "I'm X" mentions.
		matches := pattern.FindAllStringSubmatch(normalized, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			full, first := fullAndFirstNameFromParts(extractNameParts(match[1]))
			if full != "" {
				return full, ""
			}
			if firstName == "" && first != "" {
				firstName = first
			}
		}
	}
	return "", firstName
}

func nameFromReplyAfterNameQuestion(history []ChatMessage) (fullName, firstName string) {
	for i, msg := range history {
		if msg.Role != ChatRoleUser {
			continue
		}
		prev := previousAssistantMessage(history, i)
		if prev == "" || !assistantAskedForName(prev) {
			continue
		}
		full, first := fullAndFirstNameFromParts(extractNameParts(msg.Content))
		if full != "" || first != "" {
			return full, first
		}
	}
	return "", ""
}

func combineSplitNameReplies(history []ChatMessage, firstName string) string {
	first := strings.TrimSpace(firstName)
	for i, msg := range history {
		if msg.Role != ChatRoleUser {
			continue
		}
		prev := previousAssistantMessage(history, i)
		if prev == "" {
			continue
		}
		if first == "" && (assistantAskedForName(prev) || assistantAskedForFirstName(prev)) {
			full, firstOnly := fullAndFirstNameFromParts(extractNameParts(msg.Content))
			if full != "" {
				return full
			}
			if firstOnly != "" {
				first = firstOnly
			}
			continue
		}
		if first != "" && assistantAskedForLastName(prev) {
			parts := extractNameParts(msg.Content)
			if len(parts) == 0 {
				continue
			}
			if len(parts) >= 2 {
				return parts[0] + " " + parts[1]
			}
			return first + " " + parts[0]
		}
	}
	return ""
}

func assistantAskedForName(message string) bool {
	message = strings.ToLower(normalizeNameText(message))
	if !strings.Contains(message, "name") {
		return false
	}
	if strings.Contains(message, "full name") || strings.Contains(message, "first and last") {
		return true
	}
	if strings.Contains(message, "first name") || strings.Contains(message, "last name") {
		return true
	}
	if strings.Contains(message, "your name") {
		return true
	}
	if strings.Contains(message, "may i") || strings.Contains(message, "what") || strings.Contains(message, "can i") || strings.Contains(message, "could i") {
		return true
	}
	return false
}

func assistantAskedForFirstName(message string) bool {
	message = strings.ToLower(normalizeNameText(message))
	return strings.Contains(message, "first name")
}

func assistantAskedForLastName(message string) bool {
	message = strings.ToLower(normalizeNameText(message))
	if strings.Contains(message, "last name") {
		return true
	}
	if strings.Contains(message, "surname") || strings.Contains(message, "family name") {
		return true
	}
	return false
}

func fullAndFirstNameFromParts(parts []string) (fullName, firstName string) {
	if len(parts) >= 2 {
		return parts[0] + " " + parts[1], parts[0]
	}
	if len(parts) == 1 {
		return "", parts[0]
	}
	return "", ""
}

func extractNameParts(raw string) []string {
	raw = normalizeNameText(raw)
	words := strings.Fields(strings.TrimSpace(raw))
	nameWords := make([]string, 0, 2)
	for _, word := range words {
		cleaned := cleanNameToken(word)
		if cleaned == "" {
			continue
		}
		if !looksLikeNameWord(cleaned) {
			if len(nameWords) > 0 {
				break
			}
			continue
		}
		nameWords = append(nameWords, capitalizeNameWord(cleaned))
		if len(nameWords) == 2 {
			break
		}
	}
	return nameWords
}

func cleanNameToken(word string) string {
	word = strings.TrimSpace(word)
	if word == "" {
		return ""
	}
	word = strings.Trim(word, ".,!?\"()[]{}")
	word = strings.Trim(word, "'-")
	return word
}

func looksLikeNameWord(word string) bool {
	count := utf8.RuneCountInString(word)
	if count < 2 || count > 30 {
		return false
	}
	firstRune, _ := utf8.DecodeRuneInString(word)
	if !unicode.IsLetter(firstRune) {
		return false
	}
	if isCommonWord(strings.ToLower(word)) {
		return false
	}
	return true
}

func capitalizeNameWord(word string) string {
	if word == "" {
		return ""
	}
	firstRune, size := utf8.DecodeRuneInString(word)
	if firstRune == utf8.RuneError || size == 0 {
		return word
	}
	return strings.ToUpper(string(firstRune)) + strings.ToLower(word[size:])
}

var (
	priceInquiryRE = regexp.MustCompile(`(?i)\b(?:how much|price|pricing|cost|rate|rates|charge)\b`)
	phiPrefaceRE   = regexp.MustCompile(`(?i)\b(?:diagnosed|diagnosis|my condition|my symptoms|i have|i've had|i am|i'm)\b`)
	// PHI keywords with word boundaries to avoid false positives (e.g., "sti" matching in "existing")
	phiKeywordsRE      = regexp.MustCompile(`(?i)\b(?:diabetes|hiv|aids|cancer|hepatitis|pregnant|pregnancy|depression|anxiety|bipolar|schizophrenia|asthma|hypertension|blood pressure|infection|herpes|std|sti)\b`)
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
	// Use regex with word boundaries to avoid false positives
	// (e.g., "sti" matching inside "existing")
	return phiKeywordsRE.MatchString(message)
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
		"interested": true, "looking": true, "book": true, "booking": true, "appointment": true,
		"morning": true, "afternoon": true, "evening": true, "weekday": true,
		"weekend": true, "available": true, "schedule": true, "scheduling": true, "time": true,
		"botox": true, "filler": true, "facial": true, "laser": true,
		"consultation": true, "treatment": true, "service": true,
		"existing": true, "returning": true, "patient": true, "calling": true, "texting": true,
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
