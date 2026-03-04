package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/wolfman30/medspa-ai-platform/cmd/mainconfig"
	appbootstrap "github.com/wolfman30/medspa-ai-platform/internal/app/bootstrap"
	"github.com/wolfman30/medspa-ai-platform/internal/briefs"
	auditcompliance "github.com/wolfman30/medspa-ai-platform/internal/compliance"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/http/handlers"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/telnyxclient"
	observemetrics "github.com/wolfman30/medspa-ai-platform/internal/observability/metrics"
	"github.com/wolfman30/medspa-ai-platform/internal/prospects"
	"github.com/wolfman30/medspa-ai-platform/internal/stories"
	"github.com/wolfman30/medspa-ai-platform/migrations"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"

	"github.com/golang-migrate/migrate/v4"
	pgmigrate "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// DatabaseServices holds the outputs from database bootstrapping.
type DatabaseServices struct {
	Pool              *pgxpool.Pool
	SQLDB             *sql.DB
	ConversationStore *conversation.ConversationStore
	AuditSvc          *auditcompliance.AuditService
	LeadsRepo         leads.Repository
	MsgStore          *messaging.Store
}

// BootstrapDB connects to postgres, runs migrations, and initializes core DB-dependent services.
func BootstrapDB(ctx context.Context, cfg *appconfig.Config, logger *logging.Logger) DatabaseServices {
	pool := ConnectPostgresPool(ctx, cfg.DatabaseURL, logger)
	sqlDB := connectSQLDB(pool, logger)
	if sqlDB != nil {
		runAutoMigrate(sqlDB, logger)
	}
	convStore := appbootstrap.BuildConversationStore(sqlDB, cfg, logger, true)
	var audit *auditcompliance.AuditService
	if sqlDB != nil {
		audit = auditcompliance.NewAuditService(sqlDB)
	}
	return DatabaseServices{
		Pool:              pool,
		SQLDB:             sqlDB,
		ConversationStore: convStore,
		AuditSvc:          audit,
		LeadsRepo:         initializeLeadsRepository(pool),
		MsgStore:          messaging.NewStore(pool),
	}
}

func SetupMessagingMetrics() (http.Handler, *observemetrics.MessagingMetrics) {
	registry := prometheus.NewRegistry()
	messagingMetrics := observemetrics.NewMessagingMetrics(registry)
	conversation.RegisterMetrics(registry)
	metricsHandler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	return metricsHandler, messagingMetrics
}

func ConnectPostgresPool(ctx context.Context, dbURL string, logger *logging.Logger) *pgxpool.Pool {
	if dbURL == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		logger.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	if err := pool.Ping(ctx); err != nil {
		logger.Error("failed to ping postgres", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to postgres")
	return pool
}

func runAutoMigrate(db *sql.DB, logger *logging.Logger) {
	srcDriver, err := iofs.New(migrations.FS, ".")
	if err != nil {
		logger.Error("auto-migrate: failed to open migrations source", "error", err)
		return
	}
	dbDriver, err := pgmigrate.WithInstance(db, &pgmigrate.Config{})
	if err != nil {
		logger.Error("auto-migrate: failed to create db driver", "error", err)
		return
	}
	m, err := migrate.NewWithInstance("iofs", srcDriver, "postgres", dbDriver)
	if err != nil {
		logger.Error("auto-migrate: failed to create migrator", "error", err)
		return
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		logger.Error("auto-migrate: migration failed", "error", err)
		return
	}
	logger.Info("auto-migrate: database migrations applied")
}

func connectSQLDB(pool *pgxpool.Pool, logger *logging.Logger) *sql.DB {
	if pool == nil {
		return nil
	}
	db := stdlib.OpenDBFromPool(pool)
	if logger != nil {
		logger.Info("sql db wrapper initialized")
	}
	return db
}

func SetupTelnyxClient(cfg *appconfig.Config, logger *logging.Logger) *telnyxclient.Client {
	if cfg.TelnyxAPIKey == "" {
		logger.Debug("telnyx client not created: API key empty")
		return nil
	}
	client, err := telnyxclient.New(telnyxclient.Config{
		APIKey:        cfg.TelnyxAPIKey,
		WebhookSecret: cfg.TelnyxWebhookSecret,
		Timeout:       10 * time.Second,
		Logger:        logger.Logger,
	})
	if err != nil {
		logger.Error("failed to configure telnyx client", "error", err)
		os.Exit(1)
	}
	return client
}

func initializeLeadsRepository(dbPool *pgxpool.Pool) leads.Repository {
	if dbPool != nil {
		return leads.NewPostgresRepository(dbPool)
	}
	return leads.NewInMemoryRepository()
}

type ConversationSetupDeps struct {
	Ctx    context.Context
	Cfg    *appconfig.Config
	DBPool *pgxpool.Pool
	Logger *logging.Logger
}

func SetupConversation(deps ConversationSetupDeps) (*conversation.Publisher, conversation.JobRecorder, conversation.JobUpdater, *conversation.MemoryQueue) {
	ctx := deps.Ctx
	cfg := deps.Cfg
	dbPool := deps.DBPool
	logger := deps.Logger
	if cfg.UseMemoryQueue {
		if dbPool == nil {
			logger.Error("USE_MEMORY_QUEUE requires DATABASE_URL for job persistence")
			os.Exit(1)
		}
		memoryQueue := conversation.NewMemoryQueue(1024)
		pgStore := conversation.NewPGJobStore(dbPool)
		publisher := conversation.NewPublisher(memoryQueue, pgStore, logger)
		return publisher, pgStore, pgStore, memoryQueue
	}

	awsCfg, err := mainconfig.LoadAWSConfig(ctx, cfg)
	if err != nil {
		logger.Error("failed to load AWS config", "error", err)
		os.Exit(1)
	}
	sqsClient := sqs.NewFromConfig(awsCfg)
	sqsQueue := conversation.NewSQSQueue(sqsClient, cfg.ConversationQueueURL)
	dynamoClient := dynamodb.NewFromConfig(awsCfg)
	store := conversation.NewJobStore(dynamoClient, cfg.ConversationJobsTable, logger)
	publisher := conversation.NewPublisher(sqsQueue, store, logger)
	return publisher, store, store, nil
}

func WaitForInlineWorker(inlineWorker *conversation.Worker, logger *logging.Logger) {
	if inlineWorker == nil {
		return
	}
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer waitCancel()

	done := make(chan struct{})
	go func() {
		inlineWorker.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("inline conversation workers stopped")
	case <-waitCtx.Done():
		logger.Warn("inline conversation workers shutdown timed out", "error", waitCtx.Err())
	}
}

func NewBriefsHandler(pool *pgxpool.Pool, logger *logging.Logger) *handlers.AdminBriefsHandler {
	abs, err := filepath.Abs("research")
	if err != nil {
		abs = ""
	} else if info, statErr := os.Stat(abs); statErr != nil || !info.IsDir() {
		abs = ""
	}
	h := handlers.NewAdminBriefsHandler(abs, logger)
	if pool != nil {
		h.SetRepository(briefs.NewPostgresBriefsRepository(pool))
	}
	return h
}

func NewProspectsHandler(sqlDB *sql.DB) *prospects.Handler {
	h := prospects.NewHandler(prospects.NewRepository(sqlDB))
	if abs, err := filepath.Abs("research"); err == nil {
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			h.SetResearchDir(abs)
		}
	}
	return h
}

func NewStoriesHandler(sqlDB *sql.DB) *stories.Handler {
	return stories.NewHandler(stories.NewRepository(sqlDB))
}

func NewFinanceHandler(logger *logging.Logger) *handlers.AdminFinanceHandler {
	abs, err := filepath.Abs(filepath.Join("data", "budget.json"))
	if err != nil {
		abs = filepath.Join("data", "budget.json")
	}

	env := strings.ToLower(strings.TrimSpace(os.Getenv("PLAID_ENV")))
	baseURL := "https://production.plaid.com"
	if env == "sandbox" {
		baseURL = "https://sandbox.plaid.com"
	} else if env == "development" {
		baseURL = "https://development.plaid.com"
	}

	return handlers.NewAdminFinanceHandler(logger, abs, handlers.PlaidConfig{
		BaseURL:     baseURL,
		ClientID:    strings.TrimSpace(os.Getenv("PLAID_CLIENT_ID")),
		Secret:      strings.TrimSpace(os.Getenv("PLAID_SECRET")),
		AccessToken: strings.TrimSpace(os.Getenv("PLAID_ACCESS_TOKEN")),
	})
}
