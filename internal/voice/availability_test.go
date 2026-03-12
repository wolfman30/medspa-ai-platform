package voice

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	moxieclient "github.com/wolfman30/medspa-ai-platform/internal/emr/moxie"
)

func TestSummarizeServiceAvailability_LimitsDaysAndTimes(t *testing.T) {
	loc := time.UTC
	slots := []time.Time{
		time.Date(2026, 3, 16, 9, 0, 0, 0, loc),
		time.Date(2026, 3, 16, 10, 0, 0, 0, loc),
		time.Date(2026, 3, 16, 11, 0, 0, 0, loc),
		time.Date(2026, 3, 16, 12, 0, 0, 0, loc), // ignored (max 3/day)
		time.Date(2026, 3, 17, 9, 30, 0, 0, loc),
		time.Date(2026, 3, 18, 14, 0, 0, 0, loc),
		time.Date(2026, 3, 19, 15, 0, 0, 0, loc), // 4th day, should roll into moreText
	}

	summary := summarizeServiceAvailability("Botox", slots, loc)

	if !strings.Contains(summary, "- Botox: (and 1 more days)") {
		t.Fatalf("expected more-days suffix, got: %s", summary)
	}
	if strings.Contains(summary, "Monday, March 16: 9:00 AM, 10:00 AM, 11:00 AM, 12:00 PM") {
		t.Fatalf("expected only 3 times per day, got: %s", summary)
	}
	if strings.Count(summary, "\n  ") != 3 {
		t.Fatalf("expected exactly 3 day lines, got: %s", summary)
	}
}

func TestFetchAvailabilitySummary_RoutesToBoulevard(t *testing.T) {
	orgID := "org-boulevard-route"
	cfg := clinic.DefaultConfig(orgID)
	cfg.BookingPlatform = "boulevard"
	cfg.BoulevardBusinessID = "biz_123"
	cfg.BoulevardLocationID = "loc_456"
	store := setupClinicStore(t, cfg)

	origMoxie := fetchMoxieAvailabilitySummaryFn
	origBlvd := fetchBoulevardAvailabilitySummaryFn
	t.Cleanup(func() {
		fetchMoxieAvailabilitySummaryFn = origMoxie
		fetchBoulevardAvailabilitySummaryFn = origBlvd
	})

	moxieCalled := false
	boulevardCalled := false
	fetchMoxieAvailabilitySummaryFn = func(_ context.Context, _ *slog.Logger, _ *moxieclient.Client, _ *clinic.Config) string {
		moxieCalled = true
		return ""
	}
	fetchBoulevardAvailabilitySummaryFn = func(_ context.Context, _ *slog.Logger, _ *clinic.Config) string {
		boulevardCalled = true
		return "- Botox: No openings in the next 2 weeks"
	}

	got := FetchAvailabilitySummary(slog.Default(), nil, store, orgID)
	if !boulevardCalled {
		t.Fatal("expected boulevard prefetch to be called")
	}
	if moxieCalled {
		t.Fatal("did not expect moxie prefetch for boulevard clinic")
	}
	if !strings.Contains(got, "Botox") {
		t.Fatalf("unexpected summary: %q", got)
	}
}
