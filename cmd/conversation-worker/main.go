package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	openai "github.com/sashabaranov/go-openai"
	"github.com/wolfman30/medspa-ai-platform/cmd/mainconfig"
	"github.com/wolfman30/medspa-ai-platform/internal/bookings"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/langchain"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func main() {
	cfg := appconfig.Load()
	logger := logging.New(cfg.LogLevel)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var dbPool *pgxpool.Pool
	if cfg.DatabaseURL != "" {
		pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
		if err != nil {
			logger.Error("worker failed to connect to postgres", "error", err)
			os.Exit(1)
		}
		dbPool = pool
		defer dbPool.Close()
	}

	awsConfig, err := mainconfig.LoadAWSConfig(context.Background(), cfg)
	if err != nil {
		logger.Error("failed to load AWS config", "error", err)
		os.Exit(1)
	}

	sqsClient := sqs.NewFromConfig(awsConfig)
	queue := conversation.NewSQSQueue(sqsClient, cfg.ConversationQueueURL)
	dynamoClient := dynamodb.NewFromConfig(awsConfig)
	jobStore := conversation.NewJobStore(dynamoClient, cfg.ConversationJobsTable, logger)

	processor := buildConversationService(cfg, logger)
	var messenger conversation.ReplyMessenger
	if cfg.TwilioAccountSID != "" && cfg.TwilioAuthToken != "" {
		messenger = messaging.NewTwilioSender(cfg.TwilioAccountSID, cfg.TwilioAuthToken, cfg.TwilioFromNumber, logger)
	} else {
		logger.Warn("twilio credentials missing; SMS replies disabled")
	}

	var bookingBridge conversationBookingAdapter
	if dbPool != nil {
		repo := bookings.NewRepository(dbPool)
		bookingBridge = conversationBookingAdapter{inner: bookings.NewService(repo, logger)}
	}

	worker := conversation.NewWorker(
		processor,
		queue,
		jobStore,
		messenger,
		bookingBridge,
		logger,
		conversation.WithWorkerCount(4),
	)

	worker.Start(ctx)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	logger.Info("shutting down conversation worker...")
	cancel()

	doneCtx, doneCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer doneCancel()

	waitCh := make(chan struct{})
	go func() {
		worker.Wait()
		close(waitCh)
	}()

	select {
	case <-waitCh:
		logger.Info("conversation worker stopped")
	case <-doneCtx.Done():
		logger.Error("conversation worker shutdown timed out", "error", doneCtx.Err())
	}
}

func buildConversationService(cfg *appconfig.Config, logger *logging.Logger) conversation.Service {
	if cfg.LangChainBaseURL == "" && cfg.OpenAIAPIKey == "" {
		logger.Warn("no conversation engine configured; using stub conversation service")
		return conversation.NewStubService()
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
	})
	knowledgeRepo := conversation.NewRedisKnowledgeRepository(redisClient)
	ctx := context.Background()
	if err := ensureDefaultKnowledge(ctx, knowledgeRepo); err != nil {
		logger.Warn("failed to seed default RAG context", "error", err)
	}

	if cfg.LangChainBaseURL != "" {
		client, err := langchain.NewClient(langchain.Config{
			BaseURL: cfg.LangChainBaseURL,
			APIKey:  cfg.LangChainAPIKey,
			Timeout: cfg.LangChainTimeout,
		})
		if err != nil {
			logger.Error("failed to configure langchain client", "error", err)
			os.Exit(1)
		}
		ingestor := conversation.NewLangChainIngestor(client)
		if err := hydrateRAGFromRedis(ctx, knowledgeRepo, ingestor, logger); err != nil {
			logger.Warn("failed to sync knowledge to langchain", "error", err)
		}
		logger.Info("using LangChain conversation service", "endpoint", cfg.LangChainBaseURL)
		return conversation.NewLangChainService(client, redisClient, logger)
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

	logger.Info("using GPT conversation service", "model", cfg.OpenAIModel, "redis", cfg.RedisAddr)
	return conversation.NewGPTService(openaiClient, redisClient, rag, cfg.OpenAIModel, logger)
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
	if rag == nil {
		return nil
	}
	docsByClinic, err := repo.LoadAll(ctx)
	if err != nil {
		return err
	}
	for clinicID, docs := range docsByClinic {
		if err := rag.AddDocuments(ctx, clinicID, docs); err != nil {
			logger.Error("failed to add documents to rag store", "clinic_id", clinicID, "error", err)
		}
	}
	return nil
}

type conversationBookingAdapter struct {
	inner *bookings.Service
}

func (a conversationBookingAdapter) ConfirmBooking(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID, scheduledFor *time.Time) error {
	if a.inner == nil {
		return nil
	}
	_, err := a.inner.ConfirmBooking(ctx, orgID, leadID, scheduledFor)
	return err
}
