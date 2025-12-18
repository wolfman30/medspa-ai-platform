package aesthetic

import (
	"context"
	"testing"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/emr"
)

func TestSyncService_Start_SyncsImmediatelyAndOnTick(t *testing.T) {
	now := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	upstream := &fakeUpstream{
		slots: []emr.Slot{
			{
				ID:        "slot-1",
				StartTime: now.Add(2 * time.Hour),
				EndTime:   now.Add(3 * time.Hour),
				Status:    "free",
			},
		},
	}

	client, err := New(Config{
		ClinicID: "clinic-1",
		Upstream: upstream,
		Now: func() time.Time {
			return now
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tick := make(chan time.Time, 1)
	stopped := make(chan struct{})
	svc, err := NewSyncService(SyncServiceConfig{
		Client:       client,
		Targets:      []SyncTarget{{ClinicID: "clinic-1"}},
		WindowDays:   1,
		DurationMins: 30,
		Tick:         tick,
		Stop: func() {
			close(stopped)
		},
	})
	if err != nil {
		t.Fatalf("NewSyncService: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		svc.Start(ctx)
		close(done)
	}()

	waitFor(t, 250*time.Millisecond, func() bool { return upstream.calls >= 1 })

	tick <- now.Add(time.Minute)
	waitFor(t, 250*time.Millisecond, func() bool { return upstream.calls >= 2 })

	cancel()
	waitFor(t, 250*time.Millisecond, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	})

	waitFor(t, 250*time.Millisecond, func() bool {
		select {
		case <-stopped:
			return true
		default:
			return false
		}
	})
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}
