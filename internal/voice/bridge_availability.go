package voice

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/emr/boulevard"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// newBoulevardClientForVoice creates a Boulevard adapter from clinic config.
func newBoulevardClientForVoice(cfg *clinic.Config, logger *slog.Logger) *boulevard.BoulevardAdapter {
	if cfg.BoulevardBusinessID == "" {
		return nil
	}
	l := logging.New("info")
	client := boulevard.NewBoulevardClient(cfg.BoulevardBusinessID, cfg.BoulevardLocationID, l)
	if client == nil {
		return nil
	}
	return boulevard.NewBoulevardAdapter(client, false, l) // Never dry-run for availability reads
}

// serviceConfirmationPattern matches Lauren confirming a service: "Great, Botox!" or "microneedling!"
var serviceConfirmationPattern = regexp.MustCompile(`(?i)\b(?:great|perfect|awesome|wonderful|sure),?\s+([A-Za-z][A-Za-z /&-]+?)(?:!|\.|\s*What)`)

// maybeFetchAvailability detects when Lauren confirms a service name and
// asynchronously fetches real Boulevard availability, then injects the
// slots into the Nova Sonic conversation as a system message.
func (b *Bridge) maybeFetchAvailability(ctx context.Context, text string) {
	b.mu.Lock()
	if b.availabilityFetched {
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()

	// Only process assistant text
	role, cleanText := parseTranscriptRoleAndText(text)
	if role != "assistant" {
		return
	}

	// Detect service confirmation pattern
	matches := serviceConfirmationPattern.FindStringSubmatch(cleanText)
	if len(matches) < 2 {
		return
	}
	serviceName := strings.TrimSpace(matches[1])
	if serviceName == "" {
		return
	}

	b.mu.Lock()
	if b.availabilityFetched {
		b.mu.Unlock()
		return
	}
	b.availabilityFetched = true
	b.mu.Unlock()

	b.logger.Info("bridge: detected service confirmation, fetching availability",
		"service", serviceName, "caller", b.callerPhone, "org_id", b.orgID)

	// Fetch async so we don't block audio
	go func() {
		if b.toolHandler == nil || b.toolHandler.deps == nil || b.toolHandler.deps.ClinicStore == nil {
			b.logger.Warn("bridge: no clinic store for availability fetch")
			return
		}

		cfg, err := b.toolHandler.deps.ClinicStore.Get(context.Background(), b.orgID)
		if err != nil || cfg == nil || !cfg.UsesBoulevardBooking() {
			b.logger.Warn("bridge: clinic not configured for Boulevard", "error", err)
			return
		}

		// Import boulevard adapter
		blvdClient := newBoulevardClientForVoice(cfg, b.logger)
		if blvdClient == nil {
			return
		}

		fetchCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		slots, _, blvdErr := blvdClient.ResolveAvailabilityWithCart(fetchCtx, serviceName, "", time.Now())
		if blvdErr != nil {
			b.logger.Error("bridge: Boulevard availability fetch failed", "error", blvdErr, "service", serviceName)
			return
		}

		if len(slots) == 0 {
			b.logger.Warn("bridge: no slots found for service", "service", serviceName)
			injectText := fmt.Sprintf("[SYSTEM: No available appointment times found for %s. Tell the patient: "+
				"\"I don't have any openings for %s right now. Let me have the team follow up with availability for you.\"]",
				serviceName, serviceName)
			if err := b.sidecar.InjectText(injectText); err != nil {
				b.logger.Error("bridge: failed to inject no-availability message", "error", err)
			}
			return
		}

		// Format slots (max 20, filter by business hours done upstream)
		var slotLines []string
		for i, s := range slots {
			if i >= 20 {
				break
			}
			slotLines = append(slotLines, fmt.Sprintf("- %s", s.StartAt.Format("Mon Jan 2 at 3:04 PM")))
		}

		injectText := fmt.Sprintf("[SYSTEM: Real available appointment times for %s just loaded from the booking system. "+
			"THESE ARE THE ONLY TIMES THAT EXIST:\n%s\n"+
			"When the patient tells you their day/time preferences, pick the best 2-3 matches from this list. "+
			"NEVER offer a time not on this list. Always say day of week + date + time.]",
			serviceName, strings.Join(slotLines, "\n"))

		b.logger.Info("bridge: injecting real availability into conversation",
			"service", serviceName, "slot_count", len(slots), "org_id", b.orgID)

		if err := b.sidecar.InjectText(injectText); err != nil {
			b.logger.Error("bridge: failed to inject availability", "error", err)
		}
	}()
}
