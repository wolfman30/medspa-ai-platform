package bootstrap

import (
	"context"
	"fmt"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// BuildSupervisor wires an optional LLM supervisor for outbound SMS replies.
func BuildSupervisor(ctx context.Context, cfg *appconfig.Config, logger *logging.Logger) (conversation.Supervisor, error) {
	if cfg == nil {
		return nil, fmt.Errorf("bootstrap: config is required")
	}
	if !cfg.SupervisorEnabled {
		return nil, nil
	}
	if logger == nil {
		logger = logging.Default()
	}
	if ctx == nil {
		ctx = context.Background()
	}

	model := strings.TrimSpace(cfg.SupervisorModelID)
	if model == "" {
		logger.Warn("supervisor enabled but model id empty; disabling")
		return nil, nil
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.AWSRegion))
	if err != nil {
		return nil, fmt.Errorf("bootstrap: load aws config: %w", err)
	}
	bedrockClient := bedrockruntime.NewFromConfig(awsCfg)

	supervisor := conversation.NewLLMSupervisor(
		conversation.NewBedrockLLMClient(bedrockClient),
		conversation.SupervisorConfig{
			Model:        model,
			Timeout:      cfg.SupervisorMaxLatency,
			SystemPrompt: cfg.SupervisorSystemPrompt,
		},
		logger,
	)
	logger.Info("supervisor enabled", "model", model, "mode", cfg.SupervisorMode)
	return supervisor, nil
}
