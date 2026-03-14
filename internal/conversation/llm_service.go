package conversation

import (
	"strings"

	"github.com/redis/go-redis/v9"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/compliance"
	blvdclient "github.com/wolfman30/medspa-ai-platform/internal/emr/boulevard"
	moxieclient "github.com/wolfman30/medspa-ai-platform/internal/emr/moxie"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

type contextKey string

const ctxKeyVoiceModel contextKey = "voiceModel"

const (
	maxHistoryMessages      = 40
	maxVoiceHistoryMessages = 20
	maxConversationMessages = 50

	defaultLLMModel           = "anthropic.claude-3-haiku-20240307-v1:0"
	defaultDepositAmountCents = 5000
	defaultDepositDescription = "Appointment deposit"

	llmMaxTokens   = 450
	llmTemperature = 0.2
)

const phiDeflectionReply = "Thanks for sharing. I can help with booking and general questions, but I can't provide medical advice over text. Please call the clinic for medical guidance or discuss this with your provider during your consultation."

const medicalAdviceDeflectionReply = "I can help with booking and general questions, but I can't provide medical advice over text. Please call the clinic for medical guidance or discuss this with your provider during your consultation."

// LLMService produces conversation responses using a configured LLM and stores context in Redis.
type LLMService struct {
	client           LLMClient
	rag              RAGRetriever
	emr              *EMRAdapter
	moxieClient      *moxieclient.Client
	boulevardAdapter *blvdclient.BoulevardAdapter
	model            string
	voiceModel       string
	logger           *logging.Logger
	history          *historyStore
	deposit          depositConfig
	leadsRepo        leads.Repository
	clinicStore      *clinic.Store
	audit            *compliance.AuditService
	paymentChecker   PaymentStatusChecker
	faqClassifier    *FAQClassifier
	variantResolver  *VariantResolver
	apiBaseURL       string // Public API base URL for callback URLs
	events           *EventLogger
	prefetcher       *AvailabilityPrefetcher
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
		model = defaultLLMModel
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
		service.deposit.DefaultAmountCents = defaultDepositAmountCents
	}
	if strings.TrimSpace(service.deposit.Description) == "" {
		service.deposit.Description = defaultDepositDescription
	}

	return service
}
