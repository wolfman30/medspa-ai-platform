package main

import (
	"context"
	"github.com/wolfman30/medspa-ai-platform/internal/bootstrap"
	"testing"

	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func mustNotPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()
	fn()
}

func TestBootstrapPayments_DoesNotPanicWithZeroValueDeps(t *testing.T) {
	logger := logging.New("debug")
	cfg := &appconfig.Config{}

	mustNotPanic(t, func() {
		_ = bootstrap.BootstrapPayments(bootstrap.PaymentsDeps{
			AppCtx: context.Background(),
			Cfg:    cfg,
			Logger: logger,
		})
	})
}

func TestBootstrapVoice_DoesNotPanicWithZeroValueDeps(t *testing.T) {
	logger := logging.New("debug")
	cfg := &appconfig.Config{}

	mustNotPanic(t, func() {
		_ = bootstrap.BootstrapVoice(bootstrap.VoiceDeps{
			Cfg:    cfg,
			Logger: logger,
		})
	})
}
