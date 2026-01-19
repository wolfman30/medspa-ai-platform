package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	conversationworker "github.com/wolfman30/medspa-ai-platform/internal/worker/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func main() {
	cfg := appconfig.Load()
	logger := logging.New(cfg.LogLevel)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		logger.Info("shutting down conversation worker...")
		cancel()
	}()
	if err := conversationworker.Run(ctx, cfg, logger); err != nil {
		logger.Error("conversation worker failed", "error", err)
		os.Exit(1)
	}
}
