package main

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
	"github.com/wolfman30/medspa-ai-platform/internal/payments"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// adminClinicDataDeps holds inputs for building the admin clinic data handler.
type adminClinicDataDeps struct {
	appCtx      context.Context
	cfg         *appconfig.Config
	logger      *logging.Logger
	dbPool      *pgxpool.Pool
	redisClient *redis.Client
}

// buildAdminClinicDataHandler constructs the clinic data admin handler with
// S3 archiver and training archiver if configured.
func buildAdminClinicDataHandler(deps adminClinicDataDeps) *handlers.AdminClinicDataHandler {
	if deps.cfg.Env == "production" || deps.dbPool == nil {
		return nil
	}

	adminCfg := handlers.AdminClinicDataConfig{
		DB:     deps.dbPool,
		Redis:  deps.redisClient,
		Logger: deps.logger,
	}

	if deps.cfg.S3ArchiveBucket != "" {
		adminCfg.Archiver = buildS3Archiver(deps)
	}
	if deps.cfg.S3TrainingBucket != "" {
		adminCfg.TrainingArchiver = buildTrainingArchiver(deps)
	}

	return handlers.NewAdminClinicDataHandler(adminCfg)
}

// buildS3Archiver creates the S3-backed conversation archiver.
func buildS3Archiver(deps adminClinicDataDeps) *clinicdata.Archiver {
	awsCfg, err := mainconfig.LoadAWSConfig(deps.appCtx, deps.cfg)
	if err != nil {
		deps.logger.Warn("failed to load AWS config for archiver, archiving disabled", "error", err)
		return nil
	}
	s3Client := s3.NewFromConfig(awsCfg)
	deps.logger.Info("S3 archiver enabled", "bucket", deps.cfg.S3ArchiveBucket)
	return clinicdata.NewArchiver(clinicdata.ArchiverConfig{
		DB:       deps.dbPool,
		S3:       s3Client,
		Bucket:   deps.cfg.S3ArchiveBucket,
		KMSKeyID: deps.cfg.S3ArchiveKMSKey,
		Logger:   deps.logger,
	})
}

// buildTrainingArchiver creates the training data archiver with LLM classifier.
func buildTrainingArchiver(deps adminClinicDataDeps) *archive.TrainingArchiver {
	awsCfg, err := mainconfig.LoadAWSConfig(deps.appCtx, deps.cfg)
	if err != nil {
		deps.logger.Warn("failed to load AWS config for training archiver", "error", err)
		return nil
	}
	trainingS3 := s3.NewFromConfig(awsCfg)
	brClient := bedrockruntime.NewFromConfig(awsCfg)
	trainingStore := archive.NewStore(trainingS3, deps.cfg.S3TrainingBucket, deps.logger.Logger)
	classifier := archive.NewClassifier(brClient, deps.cfg.ClassifierModelID, deps.logger.Logger)
	deps.logger.Info("training archiver enabled", "bucket", deps.cfg.S3TrainingBucket)
	return archive.NewTrainingArchiver(trainingStore, classifier, deps.logger.Logger)
}

// buildClinicHandlers creates the clinic config, stats, and dashboard handlers.
func buildClinicHandlers(logger *logging.Logger, clinicStore *clinic.Store, dbPool *pgxpool.Pool) (*clinic.Handler, *clinic.StatsHandler, *clinic.DashboardHandler) {
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

// buildEvidenceS3 creates the S3 client for evidence uploads.
func buildEvidenceS3(appCtx context.Context, cfg *appconfig.Config, logger *logging.Logger) handlers.S3Uploader {
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

// buildPaymentRedirect creates the short-URL payment redirect handler.
func buildPaymentRedirect(paymentsRepo *payments.Repository, logger *logging.Logger) *payments.RedirectHandler {
	return payments.NewRedirectHandler(paymentsRepo, logger)
}
