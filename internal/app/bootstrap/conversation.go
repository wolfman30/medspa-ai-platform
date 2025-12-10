package bootstrap

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	openai "github.com/sashabaranov/go-openai"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/emr/nextech"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// BuildConversationService wires Redis-backed GPT conversation services from config.
func BuildConversationService(ctx context.Context, cfg *appconfig.Config, leadsRepo leads.Repository, paymentChecker conversation.PaymentStatusChecker, logger *logging.Logger) (conversation.Service, error) {
	if cfg == nil {
		return nil, fmt.Errorf("bootstrap: config is required")
	}
	if logger == nil {
		logger = logging.Default()
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if cfg.OpenAIAPIKey == "" {
		logger.Warn("no OpenAI API key configured; using stub conversation service")
		return conversation.NewStubService(), nil
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
	})
	knowledgeRepo := conversation.NewRedisKnowledgeRepository(redisClient)
	if err := ensureDefaultKnowledge(ctx, knowledgeRepo); err != nil {
		logger.Warn("failed to seed default knowledge", "error", err)
	}

	openaiCfg := openai.DefaultConfig(cfg.OpenAIAPIKey)
	if cfg.OpenAIBaseURL != "" {
		openaiCfg.BaseURL = cfg.OpenAIBaseURL
	}
	openaiClient := openai.NewClientWithConfig(openaiCfg)

	var rag conversation.RAGRetriever
	if cfg.OpenAIEmbeddingModel != "" {
		ragStore := conversation.NewMemoryRAGStore(openaiClient, cfg.OpenAIEmbeddingModel, logger)
		if err := hydrateRAGFromRedis(ctx, knowledgeRepo, ragStore, logger); err != nil {
			logger.Warn("failed to hydrate RAG store", "error", err)
		}
		rag = ragStore
	}

	// Build GPT service options
	opts := []conversation.GPTOption{
		conversation.WithDepositConfig(conversation.DepositConfig{
			DefaultAmountCents: int32(cfg.DepositAmountCents),
			SuccessURL:         cfg.SquareSuccessURL,
			CancelURL:          cfg.SquareCancelURL,
		}),
	}

	// Configure EMR integration if credentials are provided
	emrAdapter := buildEMRAdapter(cfg, logger)
	if emrAdapter != nil {
		opts = append(opts, conversation.WithEMR(emrAdapter))
		logger.Info("EMR integration enabled", "provider", "nextech")
	}

	// Wire in leads repository for preference capture
	if leadsRepo != nil {
		opts = append(opts, conversation.WithLeadsRepo(leadsRepo))
		logger.Info("leads repository wired into conversation service")
	}

	// Wire in clinic config store for business hours awareness
	clinicStore := clinic.NewStore(redisClient)
	opts = append(opts, conversation.WithClinicStore(clinicStore))
	logger.Info("clinic config store wired into conversation service")

	// Wire in payment checker for deposit status awareness
	if paymentChecker != nil {
		opts = append(opts, conversation.WithPaymentChecker(paymentChecker))
		logger.Info("payment checker wired into conversation service")
	}

	logger.Info("using GPT conversation service", "model", cfg.OpenAIModel, "redis", cfg.RedisAddr)
	return conversation.NewGPTService(
		openaiClient,
		redisClient,
		rag,
		cfg.OpenAIModel,
		logger,
		opts...,
	), nil
}

// buildEMRAdapter creates an EMR adapter if Nextech credentials are configured.
func buildEMRAdapter(cfg *appconfig.Config, logger *logging.Logger) *conversation.EMRAdapter {
	if cfg.NextechBaseURL == "" || cfg.NextechClientID == "" || cfg.NextechClientSecret == "" {
		logger.Info("nextech EMR not configured; skipping EMR integration")
		return nil
	}

	client, err := nextech.New(nextech.Config{
		BaseURL:      cfg.NextechBaseURL,
		ClientID:     cfg.NextechClientID,
		ClientSecret: cfg.NextechClientSecret,
		Timeout:      30 * time.Second,
	})
	if err != nil {
		logger.Error("failed to create nextech client", "error", err)
		return nil
	}

	// Use empty clinicID as default; can be overridden per-org in the future
	return conversation.NewEMRAdapter(client, "")
}

func ensureDefaultKnowledge(ctx context.Context, repo *conversation.RedisKnowledgeRepository) error {
	existing, err := repo.GetDocuments(ctx, "")
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	docs := []string{
		"Dermaplaning candidates should avoid treatment if they have active acne breakouts, open wounds, or have used Accutane within the last 6 months.",
		"All MedSpa AI clinics require a deposit between $50-$100 to secure an injectable appointment; deposits apply toward treatment.",
		"New clients should be advised to arrive 10 minutes early to complete intake forms and mention any recent chemical peels or microneedling.",
	}
	return repo.AppendDocuments(ctx, "", docs)
}

func hydrateRAGFromRedis(ctx context.Context, repo conversation.KnowledgeRepository, rag conversation.RAGIngestor, logger *logging.Logger) error {
	if rag == nil || repo == nil {
		return nil
	}
	docsByClinic, err := repo.LoadAll(ctx)
	if err != nil {
		return err
	}
	if len(docsByClinic) == 0 {
		if redisRepo, ok := repo.(*conversation.RedisKnowledgeRepository); ok {
			if err := ensureDefaultKnowledge(ctx, redisRepo); err != nil && logger != nil {
				logger.Warn("failed to seed default knowledge before rag hydration", "error", err)
			}
			docsByClinic, err = repo.LoadAll(ctx)
			if err != nil {
				return err
			}
		}
	}
	for clinicID, docs := range docsByClinic {
		if err := rag.AddDocuments(ctx, clinicID, docs); err != nil {
			logger.Error("failed to add documents to rag store", "clinic_id", clinicID, "error", err)
		}
	}
	return nil
}
