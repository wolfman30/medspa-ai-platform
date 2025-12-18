package aesthetic

import (
	"context"
	"errors"
	"time"
)

type SyncTarget struct {
	ClinicID    string
	ProviderID  string
	ServiceType string
}

type SyncService struct {
	client       *Client
	targets      []SyncTarget
	windowDays   int
	durationMins int

	tick <-chan time.Time
	stop func()
}

type SyncServiceConfig struct {
	Client *Client

	Targets      []SyncTarget
	Interval     time.Duration
	WindowDays   int
	DurationMins int

	Tick <-chan time.Time
	Stop func()
}

func NewSyncService(cfg SyncServiceConfig) (*SyncService, error) {
	if cfg.Client == nil {
		return nil, errors.New("aesthetic: sync service requires client")
	}

	windowDays := cfg.WindowDays
	if windowDays <= 0 {
		windowDays = 7
	}
	if windowDays > 60 {
		windowDays = 60
	}

	durationMins := cfg.DurationMins
	if durationMins <= 0 {
		durationMins = 30
	}

	tick := cfg.Tick
	stop := cfg.Stop
	if tick == nil {
		interval := cfg.Interval
		if interval <= 0 {
			interval = 30 * time.Minute
		}
		ticker := time.NewTicker(interval)
		tick = ticker.C
		stop = ticker.Stop
	}

	targets := cfg.Targets
	if len(targets) == 0 {
		targets = []SyncTarget{{ClinicID: cfg.Client.clinicID}}
	}

	return &SyncService{
		client:       cfg.Client,
		targets:      targets,
		windowDays:   windowDays,
		durationMins: durationMins,
		tick:         tick,
		stop:         stop,
	}, nil
}

func (s *SyncService) Start(ctx context.Context) {
	if s == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	defer func() {
		if s.stop != nil {
			s.stop()
		}
	}()

	_ = s.SyncOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.tick:
			_ = s.SyncOnce(ctx)
		}
	}
}

func (s *SyncService) SyncOnce(ctx context.Context) error {
	if s == nil || s.client == nil {
		return errors.New("aesthetic: sync service not initialized")
	}

	var firstErr error
	for _, target := range s.targets {
		if err := s.client.SyncAvailability(ctx, SyncAvailabilityOptions{
			ClinicID:     target.ClinicID,
			ProviderID:   target.ProviderID,
			ServiceType:  target.ServiceType,
			WindowDays:   s.windowDays,
			DurationMins: s.durationMins,
		}); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
