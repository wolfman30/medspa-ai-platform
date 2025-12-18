package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/redis/go-redis/v9"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/emr/aesthetic"
	"github.com/wolfman30/medspa-ai-platform/internal/emr/nextech"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// BuildConversationService wires Redis-backed LLM conversation services from config.
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

	if cfg.BedrockModelID == "" {
		logger.Warn("no Bedrock model configured; using stub conversation service")
		return conversation.NewStubService(), nil
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.AWSRegion))
	if err != nil {
		return nil, fmt.Errorf("bootstrap: load aws config: %w", err)
	}
	bedrockClient := bedrockruntime.NewFromConfig(awsCfg)

	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
	})
	knowledgeRepo := conversation.NewRedisKnowledgeRepository(redisClient)
	if err := ensureDefaultKnowledge(ctx, knowledgeRepo); err != nil {
		logger.Warn("failed to seed default knowledge", "error", err)
	}

	var rag conversation.RAGRetriever
	if cfg.BedrockEmbeddingModelID != "" {
		embedder := conversation.NewBedrockEmbeddingClient(bedrockClient)
		ragStore := conversation.NewMemoryRAGStore(embedder, cfg.BedrockEmbeddingModelID, logger)
		if err := hydrateRAGFromRedis(ctx, knowledgeRepo, ragStore, logger); err != nil {
			logger.Warn("failed to hydrate RAG store", "error", err)
		}
		rag = ragStore
	}

	// Build LLM service options
	opts := []conversation.LLMOption{
		conversation.WithDepositConfig(conversation.DepositConfig{
			DefaultAmountCents: int32(cfg.DepositAmountCents),
			SuccessURL:         cfg.SquareSuccessURL,
			CancelURL:          cfg.SquareCancelURL,
		}),
	}

	// Configure EMR integration if credentials are provided
	emrAdapter := buildEMRAdapter(ctx, cfg, logger)
	if emrAdapter != nil {
		opts = append(opts, conversation.WithEMR(emrAdapter))
		logger.Info("EMR integration enabled")
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

	logger.Info("using LLM conversation service", "model", cfg.BedrockModelID, "redis", cfg.RedisAddr)
	return conversation.NewLLMService(
		conversation.NewBedrockLLMClient(bedrockClient),
		redisClient,
		rag,
		cfg.BedrockModelID,
		logger,
		opts...,
	), nil
}

// buildEMRAdapter creates an EMR adapter based on configured provider credentials.
func buildEMRAdapter(ctx context.Context, cfg *appconfig.Config, logger *logging.Logger) *conversation.EMRAdapter {
	if cfg.NextechBaseURL == "" || cfg.NextechClientID == "" || cfg.NextechClientSecret == "" {
		logger.Info("nextech EMR not configured; skipping EMR integration")
	} else {
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

		logger.Info("EMR integration enabled", "provider", "nextech")
		return conversation.NewEMRAdapter(client, "")
	}

	if strings.TrimSpace(cfg.AestheticRecordClinicID) == "" {
		logger.Info("aesthetic record shadow scheduler not configured; skipping EMR integration")
		return nil
	}

	var upstream aesthetic.AvailabilitySource
	if strings.TrimSpace(cfg.AestheticRecordSelectBaseURL) != "" {
		selectClient, err := aesthetic.NewSelectAPIClient(aesthetic.SelectAPIConfig{
			BaseURL:     cfg.AestheticRecordSelectBaseURL,
			BearerToken: cfg.AestheticRecordSelectBearerToken,
		})
		if err != nil {
			logger.Error("failed to create aesthetic record select api client", "error", err)
			return nil
		}
		upstream = selectClient
	} else {
		logger.Warn("aesthetic record upstream not configured; shadow schedule requires manual slot seeding until upstream is available")
	}

	shadowClient, err := aesthetic.New(aesthetic.Config{
		ClinicID: cfg.AestheticRecordClinicID,
		Upstream: upstream,
	})
	if err != nil {
		logger.Error("failed to create aesthetic record shadow scheduler client", "error", err)
		return nil
	}

	if cfg.AestheticRecordShadowSyncEnabled {
		if upstream == nil {
			logger.Warn("aesthetic record shadow sync enabled but upstream is nil; sync will not run")
		} else {
			targets := []aesthetic.SyncTarget{{
				ClinicID:    cfg.AestheticRecordClinicID,
				ProviderID:  strings.TrimSpace(cfg.AestheticRecordProviderID),
				ServiceType: "",
			}}
			svc, err := aesthetic.NewSyncService(aesthetic.SyncServiceConfig{
				Client:       shadowClient,
				Targets:      targets,
				Interval:     cfg.AestheticRecordSyncInterval,
				WindowDays:   cfg.AestheticRecordSyncWindowDays,
				DurationMins: cfg.AestheticRecordSyncDurationMins,
			})
			if err != nil {
				logger.Error("failed to create aesthetic record shadow sync service", "error", err)
			} else {
				go svc.Start(ctx)
				logger.Info("aesthetic record shadow scheduler sync started",
					"clinic_id", cfg.AestheticRecordClinicID,
					"provider_id", cfg.AestheticRecordProviderID,
					"interval", cfg.AestheticRecordSyncInterval.String(),
				)
			}
		}
	}

	logger.Info("EMR integration enabled", "provider", "aesthetic_record_shadow")
	return conversation.NewEMRAdapter(shadowClient, cfg.AestheticRecordClinicID)
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
