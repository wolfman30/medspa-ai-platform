package bootstrap

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/redis/go-redis/v9"

	"github.com/wolfman30/medspa-ai-platform/internal/browser"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/compliance"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/emr/aesthetic"
	"github.com/wolfman30/medspa-ai-platform/internal/emr/nextech"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	moxie "github.com/wolfman30/medspa-ai-platform/internal/moxie"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// BuildConversationService wires Redis-backed LLM conversation services from config.
func BuildConversationService(ctx context.Context, cfg *appconfig.Config, leadsRepo leads.Repository, paymentChecker conversation.PaymentStatusChecker, audit *compliance.AuditService, logger *logging.Logger) (conversation.Service, error) {
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

	redisOptions := &redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
	}
	if cfg.RedisTLS {
		redisOptions.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	redisClient := redis.NewClient(redisOptions)
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
		rag = conversation.NewHydratingRAGRetriever(ctx, knowledgeRepo, ragStore, logger)
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

	// Configure browser sidecar for booking page scraping (fallback when EMR is not available)
	if cfg.BrowserSidecarURL != "" {
		browserClient := browser.NewClient(cfg.BrowserSidecarURL, browser.WithLogger(logger))
		browserAdapter := conversation.NewBrowserAdapter(browserClient, logger)
		opts = append(opts, conversation.WithBrowserAdapter(browserAdapter))
		logger.Info("browser sidecar integration enabled", "url", cfg.BrowserSidecarURL)
	}

	// Configure direct Moxie GraphQL API client for fast availability queries
	moxieAPIClient := moxie.NewClient(logger)
	opts = append(opts, conversation.WithMoxieClient(moxieAPIClient))
	logger.Info("Moxie direct API client enabled for availability queries")

	// Wire in leads repository for preference capture
	if leadsRepo != nil {
		opts = append(opts, conversation.WithLeadsRepo(leadsRepo))
		logger.Info("leads repository wired into conversation service")
	}

	// Wire in clinic config store for business hours awareness
	clinicStore := clinic.NewStore(redisClient)
	opts = append(opts, conversation.WithClinicStore(clinicStore))
	logger.Info("clinic config store wired into conversation service")

	if audit != nil {
		opts = append(opts, conversation.WithAuditService(audit))
	}

	// Wire in payment checker for deposit status awareness
	if paymentChecker != nil {
		opts = append(opts, conversation.WithPaymentChecker(paymentChecker))
		logger.Info("payment checker wired into conversation service")
	}

	// Wire in public base URL for callback URL construction
	if cfg.PublicBaseURL != "" {
		opts = append(opts, conversation.WithAPIBaseURL(cfg.PublicBaseURL))
		logger.Info("API base URL wired for booking callbacks", "url", cfg.PublicBaseURL)
	}

	// Build primary LLM client based on provider configuration
	var primaryClient conversation.LLMClient
	var modelID string

	switch cfg.LLMProvider {
	case "gemini":
		if cfg.GeminiAPIKey == "" {
			return nil, fmt.Errorf("bootstrap: GEMINI_API_KEY required when LLM_PROVIDER=gemini")
		}
		geminiClient, err := conversation.NewGeminiLLMClient(ctx, cfg.GeminiAPIKey, cfg.GeminiModelID)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: create gemini client: %w", err)
		}
		primaryClient = geminiClient
		modelID = cfg.GeminiModelID
		logger.Info("using Gemini as primary LLM provider", "model", modelID)
	default: // "bedrock" or empty
		primaryClient = conversation.NewBedrockLLMClient(bedrockClient)
		modelID = cfg.BedrockModelID
		logger.Info("using Bedrock as primary LLM provider", "model", modelID)
	}

	// Build fallback client if enabled
	var llmClient conversation.LLMClient = primaryClient
	if cfg.LLMFallbackEnabled {
		var fallbackClient conversation.LLMClient
		switch cfg.LLMFallbackProvider {
		case "gemini":
			if cfg.GeminiAPIKey != "" {
				geminiClient, err := conversation.NewGeminiLLMClient(ctx, cfg.GeminiAPIKey, cfg.GeminiModelID)
				if err != nil {
					logger.Warn("failed to create gemini fallback client", "error", err)
				} else {
					fallbackClient = geminiClient
					logger.Info("Gemini fallback LLM enabled", "model", cfg.GeminiModelID)
				}
			} else {
				logger.Warn("LLM fallback enabled but GEMINI_API_KEY not set")
			}
		case "bedrock":
			fallbackClient = conversation.NewBedrockLLMClient(bedrockClient)
			logger.Info("Bedrock fallback LLM enabled", "model", cfg.BedrockModelID)
		default:
			logger.Warn("unknown fallback provider", "provider", cfg.LLMFallbackProvider)
		}

		if fallbackClient != nil {
			llmClient = conversation.NewFallbackLLMClient(primaryClient, fallbackClient, logger.Logger)
		}
	}

	logger.Info("using LLM conversation service", "model", modelID, "redis", cfg.RedisAddr, "fallback_enabled", cfg.LLMFallbackEnabled)
	return conversation.NewLLMService(
		llmClient,
		redisClient,
		rag,
		modelID,
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
		"We require a $50 refundable deposit to secure your appointment; the deposit applies toward your treatment cost.",
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
