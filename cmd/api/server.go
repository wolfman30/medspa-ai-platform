package main

import (
	"context"
	"fmt"
	"github.com/wolfman30/medspa-ai-platform/internal/bootstrap"
	"net/http"
	"os"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// runServer starts the HTTP server, waits for context cancellation,
// then gracefully drains connections and stops the inline worker.
func runServer(ctx context.Context, handler http.Handler, port string, logger *logging.Logger, inlineWorker *conversation.Worker) {
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}

	bootstrap.WaitForInlineWorker(inlineWorker, logger)

	logger.Info("server stopped")
	fmt.Println("Server exited gracefully")
}
