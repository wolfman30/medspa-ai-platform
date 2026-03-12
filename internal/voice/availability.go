package voice

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/emr/boulevard"
	moxieclient "github.com/wolfman30/medspa-ai-platform/internal/emr/moxie"
)

var (
	fetchMoxieAvailabilitySummaryFn     = fetchMoxieAvailabilitySummary
	fetchBoulevardAvailabilitySummaryFn = fetchBoulevardAvailabilitySummary
)

// FetchAvailabilitySummary pre-fetches available slots for the clinic's top services
// and returns a human-readable summary for injection into the voice AI system prompt.
func FetchAvailabilitySummary(l *slog.Logger, mc *moxieclient.Client, cs *clinic.Store, orgID string) string {
	if cs == nil || orgID == "" {
		l.Warn("voice-availability: no org ID, skipping pre-fetch")
		return ""
	}

	ctx := context.Background()
	cfg, err := cs.Get(ctx, orgID)
	if err != nil {
		l.Warn("voice-availability: could not load clinic config", "org_id", orgID, "error", err)
		return ""
	}

	if strings.EqualFold(cfg.BookingPlatform, "boulevard") {
		return fetchBoulevardAvailabilitySummaryFn(ctx, l, cfg)
	}

	if mc == nil {
		l.Warn("voice-availability: no moxie client, skipping pre-fetch", "org_id", orgID)
		return ""
	}

	return fetchMoxieAvailabilitySummaryFn(ctx, l, mc, cfg)
}

func fetchMoxieAvailabilitySummary(ctx context.Context, l *slog.Logger, mc *moxieclient.Client, cfg *clinic.Config) string {
	if cfg == nil {
		l.Warn("voice-availability: missing clinic config for moxie prefetch")
		return ""
	}
	if cfg.MoxieConfig == nil || cfg.MoxieConfig.MedspaID == "" {
		l.Warn("voice-availability: no medspa_id configured", "org_id", cfg.OrgID)
		return ""
	}

	// Get service menu items (service name → moxie menu item ID)
	if len(cfg.MoxieConfig.ServiceMenuItems) == 0 {
		l.Warn("voice-availability: no service menu items configured", "org_id", cfg.OrgID)
		return ""
	}

	loc := clinicLocation(cfg.Timezone)
	now := time.Now().In(loc)
	startDate := now.Format("2006-01-02")
	endDate := now.AddDate(0, 0, 14).Format("2006-01-02") // next 2 weeks

	// Fetch top 5 services
	var lines []string
	count := 0
	for svcName, menuItemID := range cfg.MoxieConfig.ServiceMenuItems {
		if count >= 5 {
			break
		}
		if menuItemID == "" {
			continue
		}

		result, err := mc.GetAvailableSlots(ctx, cfg.MoxieConfig.MedspaID, startDate, endDate, menuItemID, true)
		if err != nil {
			l.Warn("voice-availability: failed to fetch moxie slots", "service", svcName, "error", err)
			continue
		}
		if result == nil || len(result.Dates) == 0 {
			lines = append(lines, fmt.Sprintf("- %s: No openings in the next 2 weeks", svcName))
			count++
			continue
		}

		var slots []time.Time
		for _, ds := range result.Dates {
			for _, slot := range ds.Slots {
				t, err := parseSlotTime(slot.Start, loc)
				if err != nil {
					continue
				}
				slots = append(slots, t)
			}
		}
		lines = append(lines, summarizeServiceAvailability(svcName, slots, loc))
		count++
	}

	if len(lines) == 0 {
		return ""
	}

	l.Info("voice-availability: pre-fetched moxie availability", "org_id", cfg.OrgID, "services", len(lines))
	return strings.Join(lines, "\n")
}

func fetchBoulevardAvailabilitySummary(ctx context.Context, l *slog.Logger, cfg *clinic.Config) string {
	if cfg == nil {
		l.Warn("voice-availability: missing clinic config for boulevard prefetch")
		return ""
	}
	if cfg.BoulevardBusinessID == "" || cfg.BoulevardLocationID == "" {
		l.Warn("voice-availability: boulevard missing business/location config", "org_id", cfg.OrgID)
		return ""
	}

	blvdClient := boulevard.NewBoulevardClient(cfg.BoulevardBusinessID, cfg.BoulevardLocationID, nil)
	services, err := boulevardServicesToPrefetch(ctx, blvdClient, cfg)
	if err != nil {
		l.Warn("voice-availability: failed loading boulevard services", "org_id", cfg.OrgID, "error", err)
		return ""
	}
	if len(services) == 0 {
		l.Warn("voice-availability: no boulevard services configured", "org_id", cfg.OrgID)
		return ""
	}

	loc := clinicLocation(cfg.Timezone)
	var lines []string
	for _, svcName := range services {
		slots, _, err := blvdClient.GetAvailableSlots(ctx, svcName, "", cfg.Timezone)
		if err != nil {
			l.Warn("voice-availability: failed to fetch boulevard slots", "service", svcName, "error", err)
			continue
		}

		times := make([]time.Time, 0, len(slots))
		for _, slot := range slots {
			times = append(times, slot.StartAt)
		}
		lines = append(lines, summarizeServiceAvailability(svcName, times, loc))
	}

	if len(lines) == 0 {
		return ""
	}

	l.Info("voice-availability: pre-fetched boulevard availability", "org_id", cfg.OrgID, "services", len(lines))
	return strings.Join(lines, "\n")
}

func boulevardServicesToPrefetch(ctx context.Context, client *boulevard.BoulevardClient, cfg *clinic.Config) ([]string, error) {
	if len(cfg.Services) > 0 {
		services := make([]string, 0, 5)
		seen := make(map[string]struct{}, len(cfg.Services))
		for _, svc := range cfg.Services {
			trimmed := strings.TrimSpace(svc)
			if trimmed == "" {
				continue
			}
			key := strings.ToLower(trimmed)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			services = append(services, trimmed)
			if len(services) >= 5 {
				break
			}
		}
		return services, nil
	}

	_, catalog, err := client.CreateCart(ctx)
	if err != nil {
		return nil, err
	}

	services := make([]string, 0, 5)
	for _, svc := range catalog {
		name := strings.TrimSpace(svc.Name)
		if name == "" {
			continue
		}
		services = append(services, name)
		if len(services) >= 5 {
			break
		}
	}
	return services, nil
}

func summarizeServiceAvailability(service string, slots []time.Time, loc *time.Location) string {
	if len(slots) == 0 {
		return fmt.Sprintf("- %s: No openings in the next 2 weeks", service)
	}

	type daySlots struct {
		day   string
		times []string
	}
	dayMap := make(map[string]*daySlots)
	var dayOrder []string

	for _, t := range slots {
		t = t.In(loc)
		dayKey := t.Format("Monday, January 2")
		d, ok := dayMap[dayKey]
		if !ok {
			d = &daySlots{day: dayKey}
			dayMap[dayKey] = d
			dayOrder = append(dayOrder, dayKey)
		}
		if len(d.times) < 3 {
			d.times = append(d.times, t.Format("3:04 PM"))
		}
	}

	if len(dayOrder) == 0 {
		return fmt.Sprintf("- %s: No openings in the next 2 weeks", service)
	}

	var dayLines []string
	for i, dayKey := range dayOrder {
		if i >= 3 {
			break
		}
		d := dayMap[dayKey]
		dayLines = append(dayLines, fmt.Sprintf("  %s: %s", d.day, strings.Join(d.times, ", ")))
	}

	moreText := ""
	if len(dayOrder) > 3 {
		moreText = fmt.Sprintf(" (and %d more days)", len(dayOrder)-3)
	}
	return fmt.Sprintf("- %s:%s\n%s", service, moreText, strings.Join(dayLines, "\n"))
}

func parseSlotTime(raw string, loc *time.Location) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, raw)
	if err == nil {
		return t.In(loc), nil
	}
	return time.ParseInLocation("2006-01-02T15:04:05", raw, loc)
}

func clinicLocation(tz string) *time.Location {
	if tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.FixedZone("EST", -5*60*60)
}
