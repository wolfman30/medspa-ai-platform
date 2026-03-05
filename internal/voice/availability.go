package voice

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	moxieclient "github.com/wolfman30/medspa-ai-platform/internal/emr/moxie"
)

// FetchAvailabilitySummary pre-fetches available slots for the clinic's top services
// and returns a human-readable summary for injection into the voice AI system prompt.
func FetchAvailabilitySummary(l *slog.Logger, mc *moxieclient.Client, cs *clinic.Store, orgID string) string {
	if cs == nil || mc == nil || orgID == "" {
		l.Warn("voice-availability: no org ID, skipping pre-fetch")
		return ""
	}

	ctx := context.Background()
	cfg, err := cs.Get(ctx, orgID)
	if err != nil {
		l.Warn("voice-availability: could not load clinic config", "org_id", orgID, "error", err)
		return ""
	}

	if cfg.MoxieConfig == nil || cfg.MoxieConfig.MedspaID == "" {
		l.Warn("voice-availability: no medspa_id configured", "org_id", orgID)
		return ""
	}

	// Get service menu items (service name → moxie menu item ID)
	if len(cfg.MoxieConfig.ServiceMenuItems) == 0 {
		l.Warn("voice-availability: no service menu items configured", "org_id", orgID)
		return ""
	}

	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		loc = time.FixedZone("EST", -5*60*60)
	}
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
			l.Warn("voice-availability: failed to fetch slots", "service", svcName, "error", err)
			continue
		}
		if result == nil || len(result.Dates) == 0 {
			lines = append(lines, fmt.Sprintf("- %s: No openings in the next 2 weeks", svcName))
			count++
			continue
		}

		// Collect all slots, group by day
		type daySlots struct {
			Day   string
			Times []string
		}
		var dayOrder []string
		dayMap := make(map[string]*daySlots)

		for _, ds := range result.Dates {
			for _, slot := range ds.Slots {
				t, err := time.Parse(time.RFC3339, slot.Start)
				if err != nil {
					// Try without timezone
					t, err = time.ParseInLocation("2006-01-02T15:04:05", slot.Start, loc)
					if err != nil {
						continue
					}
				}
				t = t.In(loc)
				dayKey := t.Format("Monday, January 2")
				if _, ok := dayMap[dayKey]; !ok {
					dayMap[dayKey] = &daySlots{Day: dayKey}
					dayOrder = append(dayOrder, dayKey)
				}
				d := dayMap[dayKey]
				if len(d.Times) < 3 {
					d.Times = append(d.Times, t.Format("3:04 PM"))
				}
			}
		}

		var dayLines []string
		shown := 0
		for _, dk := range dayOrder {
			if shown >= 3 {
				break
			}
			d := dayMap[dk]
			dayLines = append(dayLines, fmt.Sprintf("  %s: %s", d.Day, strings.Join(d.Times, ", ")))
			shown++
		}
		moreText := ""
		if len(dayOrder) > 3 {
			moreText = fmt.Sprintf(" (and %d more days)", len(dayOrder)-3)
		}
		lines = append(lines, fmt.Sprintf("- %s:%s\n%s", svcName, moreText, strings.Join(dayLines, "\n")))
		count++
	}

	if len(lines) == 0 {
		return ""
	}

	l.Info("voice-availability: pre-fetched availability", "org_id", orgID, "services", len(lines))
	return strings.Join(lines, "\n")
}
