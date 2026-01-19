package bootstrap

import (
	"context"
	"testing"

	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestBuildSupervisorRequiresConfig(t *testing.T) {
	if _, err := BuildSupervisor(context.Background(), nil, logging.New("error")); err == nil {
		t.Fatalf("expected error for nil config")
	}
}

func TestBuildSupervisorDisabledReturnsNil(t *testing.T) {
	cfg := &appconfig.Config{SupervisorEnabled: false}

	supervisor, err := BuildSupervisor(context.Background(), cfg, logging.New("error"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if supervisor != nil {
		t.Fatalf("expected nil supervisor when disabled")
	}
}

func TestBuildSupervisorNoModelReturnsNil(t *testing.T) {
	cfg := &appconfig.Config{
		SupervisorEnabled: true,
		SupervisorModelID: "",
	}

	supervisor, err := BuildSupervisor(context.Background(), cfg, logging.New("error"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if supervisor != nil {
		t.Fatalf("expected nil supervisor when model is empty")
	}
}
