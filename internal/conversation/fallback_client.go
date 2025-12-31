package conversation

import (
	"context"
	"log/slog"
)

// FallbackLLMClient wraps a primary LLM client with a fallback provider.
// If the primary fails, it automatically retries with the fallback.
type FallbackLLMClient struct {
	primary  LLMClient
	fallback LLMClient
	logger   *slog.Logger
}

// NewFallbackLLMClient creates a new fallback-enabled LLM client.
// If fallback is nil, the client will only use the primary provider.
func NewFallbackLLMClient(primary, fallback LLMClient, logger *slog.Logger) *FallbackLLMClient {
	if logger == nil {
		logger = slog.Default()
	}
	return &FallbackLLMClient{
		primary:  primary,
		fallback: fallback,
		logger:   logger,
	}
}

// Complete sends a completion request to the primary LLM.
// If it fails and a fallback is configured, retries with the fallback.
func (c *FallbackLLMClient) Complete(ctx context.Context, req LLMRequest) (LLMResponse, error) {
	resp, err := c.primary.Complete(ctx, req)
	if err == nil {
		return resp, nil
	}

	// Log the primary failure
	c.logger.Warn("primary LLM failed, attempting fallback",
		"error", err.Error(),
		"fallback_available", c.fallback != nil,
	)

	// If no fallback configured, return the original error
	if c.fallback == nil {
		return LLMResponse{}, err
	}

	// Try the fallback provider
	fallbackResp, fallbackErr := c.fallback.Complete(ctx, req)
	if fallbackErr != nil {
		c.logger.Error("fallback LLM also failed",
			"primary_error", err.Error(),
			"fallback_error", fallbackErr.Error(),
		)
		// Return the fallback error since that was the last attempt
		return LLMResponse{}, fallbackErr
	}

	c.logger.Info("fallback LLM succeeded after primary failure")
	return fallbackResp, nil
}
