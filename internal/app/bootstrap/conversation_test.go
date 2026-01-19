package bootstrap

import (
	"context"
	"testing"

	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestBuildConversationServiceRequiresConfig(t *testing.T) {
	if _, err := BuildConversationService(context.Background(), nil, nil, nil, nil, nil); err == nil {
		t.Fatalf("expected error for nil config")
	}
}

func TestBuildConversationServiceNoModelReturnsStub(t *testing.T) {
	cfg := &appconfig.Config{}

	svc, err := BuildConversationService(nil, cfg, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc == nil {
		t.Fatalf("expected service")
	}
	if _, ok := svc.(*conversation.StubService); !ok {
		t.Fatalf("expected StubService, got %T", svc)
	}
}

func TestBuildEMRAdapterMissingConfigReturnsNil(t *testing.T) {
	cfg := &appconfig.Config{}
	logger := logging.New("error")

	adapter := buildEMRAdapter(context.Background(), cfg, logger)
	if adapter != nil {
		t.Fatalf("expected nil adapter when EMR is not configured")
	}
}
