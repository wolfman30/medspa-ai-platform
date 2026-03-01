package bootstrap

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"github.com/wolfman30/medspa-ai-platform/cmd/mainconfig"
	"github.com/wolfman30/medspa-ai-platform/internal/archive"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/clinicdata"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/http/handlers"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// AdminClinicDataDeps holds inputs for building the admin clinic data handler.
type AdminClinicDataDeps struct {
	AppCtx      context.Context
	Cfg         *appconfig.Config
	Logger      *logging.Logger
	DBPool      *pgxpool.Pool
	RedisClient *redis.Client
}

// AdminAssembler groups shared dependencies for admin handler assembly.
type AdminAssembler struct {
	appCtx      context.Context
	cfg         *appconfig.Config
	logger      *logging.Logger
	dbPool      *pgxpool.Pool
	redisClient *redis.Client
}

func NewAdminAssembler(deps AdminClinicDataDeps) *AdminAssembler {
	return &AdminAssembler{
		appCtx:      deps.AppCtx,
		cfg:         deps.Cfg,
		logger:      deps.Logger,
		dbPool:      deps.DBPool,
		redisClient: deps.RedisClient,
	}
}

// BuildAdminClinicDataHandler constructs the clinic data admin handler with
// S3 archiver and training archiver if configured.
func BuildAdminClinicDataHandler(deps AdminClinicDataDeps) *handlers.AdminClinicDataHandler {
	return NewAdminAssembler(deps).buildAdminClinicDataHandler()
}

func (a *AdminAssembler) buildAdminClinicDataHandler() *handlers.AdminClinicDataHandler {
	if a.cfg.Env == "production" || a.dbPool == nil {
		return nil
	}

	adminCfg := handlers.AdminClinicDataConfig{
		DB:     a.dbPool,
		Redis:  a.redisClient,
		Logger: a.logger,
	}

	if a.cfg.S3ArchiveBucket != "" {
		adminCfg.Archiver = a.buildS3Archiver()
	}
	if a.cfg.S3TrainingBucket != "" {
		adminCfg.TrainingArchiver = a.buildTrainingArchiver()
	}

	return handlers.NewAdminClinicDataHandler(adminCfg)
}

// buildS3Archiver creates the S3-backed conversation archiver.
func (a *AdminAssembler) buildS3Archiver() *clinicdata.Archiver {
	awsCfg, err := mainconfig.LoadAWSConfig(a.appCtx, a.cfg)
	if err != nil {
		a.logger.Warn("failed to load AWS config for archiver, archiving disabled", "error", err)
		return nil
	}
	s3Client := s3.NewFromConfig(awsCfg)
	a.logger.Info("S3 archiver enabled", "bucket", a.cfg.S3ArchiveBucket)
	return clinicdata.NewArchiver(clinicdata.ArchiverConfig{
		DB:       a.dbPool,
		S3:       s3Client,
		Bucket:   a.cfg.S3ArchiveBucket,
		KMSKeyID: a.cfg.S3ArchiveKMSKey,
		Logger:   a.logger,
	})
}

// buildTrainingArchiver creates the training data archiver with LLM classifier.
func (a *AdminAssembler) buildTrainingArchiver() *archive.TrainingArchiver {
	awsCfg, err := mainconfig.LoadAWSConfig(a.appCtx, a.cfg)
	if err != nil {
		a.logger.Warn("failed to load AWS config for training archiver", "error", err)
		return nil
	}
	trainingS3 := s3.NewFromConfig(awsCfg)
	brClient := bedrockruntime.NewFromConfig(awsCfg)
	trainingStore := archive.NewStore(trainingS3, a.cfg.S3TrainingBucket, a.logger.Logger)
	classifier := archive.NewClassifier(brClient, a.cfg.ClassifierModelID, a.logger.Logger)
	a.logger.Info("training archiver enabled", "bucket", a.cfg.S3TrainingBucket)
	return archive.NewTrainingArchiver(trainingStore, classifier, a.logger.Logger)
}

// BuildClinicHandlers creates the clinic config, stats, and dashboard handlers.
func BuildClinicHandlers(logger *logging.Logger, clinicStore *clinic.Store, dbPool *pgxpool.Pool) (*clinic.Handler, *clinic.StatsHandler, *clinic.DashboardHandler) {
	var ch *clinic.Handler
	var stats *clinic.StatsHandler
	var dashboard *clinic.DashboardHandler

	if clinicStore != nil {
		ch = clinic.NewHandler(clinicStore, logger)
	}
	if dbPool != nil {
		stats = clinic.NewStatsHandler(clinic.NewStatsRepository(dbPool), logger)
		dashboard = clinic.NewDashboardHandler(clinic.NewDashboardRepository(dbPool), prometheus.DefaultGatherer, logger)
	}
	return ch, stats, dashboard
}

// BuildEvidenceS3 creates the S3 client for evidence uploads.
func BuildEvidenceS3(appCtx context.Context, cfg *appconfig.Config, logger *logging.Logger) handlers.S3Uploader {
	if cfg.S3TrainingBucket == "" {
		return nil
	}
	awsCfg, err := mainconfig.LoadAWSConfig(appCtx, cfg)
	if err != nil {
		return nil
	}
	logger.Info("evidence upload S3 enabled", "bucket", cfg.S3TrainingBucket)
	return s3.NewFromConfig(awsCfg)
}
