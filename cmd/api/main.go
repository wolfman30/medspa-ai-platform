package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/wolfman30/medspa-ai-platform/internal/api/router"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func main() {
	// Load configuration
	cfg := appconfig.Load()

	// Initialize logger
	logger := logging.New(cfg.LogLevel)
	logger.Info("starting medspa-ai-platform API server",
		"env", cfg.Env,
		"port", cfg.Port,
	)

	// Initialize repositories and services
	leadsRepo := leads.NewInMemoryRepository()
	conversationEngine := conversation.NewStubService()

	awsCfg, err := loadAWSConfig(context.Background(), cfg)
	if err != nil {
		logger.Error("failed to load AWS config", "error", err)
		os.Exit(1)
	}

	sqsClient := sqs.NewFromConfig(awsCfg)
	conversationQueue := conversation.NewSQSQueue(sqsClient, cfg.ConversationQueueURL)
	conversationDispatcher := conversation.NewOrchestrator(conversationEngine, conversationQueue, logger)

	// Initialize handlers
	leadsHandler := leads.NewHandler(leadsRepo, logger)
	messagingHandler := messaging.NewHandler(cfg.TwilioWebhookSecret, logger)
	conversationHandler := conversation.NewHandler(conversationDispatcher, logger)

	// Setup router
	routerCfg := &router.Config{
		Logger:              logger,
		LeadsHandler:        leadsHandler,
		MessagingHandler:    messagingHandler,
		ConversationHandler: conversationHandler,
	}
	r := router.New(routerCfg)

	// Create HTTP server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		logger.Info("server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}

	if err := conversationDispatcher.Shutdown(ctx); err != nil {
		logger.Error("failed to shutdown conversation dispatcher", "error", err)
	}

	logger.Info("server stopped")
	fmt.Println("Server exited gracefully")
}

func loadAWSConfig(ctx context.Context, cfg *appconfig.Config) (aws.Config, error) {
	loaders := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.AWSRegion),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AWSAccessKeyID, cfg.AWSSecretAccessKey, ""),
		),
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, loaders...)
	if err != nil {
		return aws.Config{}, err
	}

	if endpoint := cfg.AWSEndpointOverride; endpoint != "" {
		awsCfg.EndpointResolverWithOptions = aws.EndpointResolverWithOptionsFunc(
			func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				if service == sqs.ServiceID {
					return aws.Endpoint{
						URL:           endpoint,
						PartitionID:   "aws",
						SigningRegion: cfg.AWSRegion,
					}, nil
				}
				return aws.Endpoint{}, &aws.EndpointNotFoundError{}
			},
		)
	}

	return awsCfg, nil
}
