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

		// Store slots on bridge for later filtering
		b.mu.Lock()
		b.fetchedSlots = slots
		b.mu.Unlock()

		b.logger.Info("bridge: availability fetched and stored for filtering",
			"service", serviceName, "slot_count", len(slots), "org_id", b.orgID)

		// Inject a brief system message — full filtered list comes after time preferences
		injectText := fmt.Sprintf("[SYSTEM: %d appointment slots loaded for %s. "+
			"Ask the patient for their preferred days and times. "+
			"Do NOT offer any specific times yet — wait until they tell you their preferences.]",
			len(slots), serviceName)

		if err := b.sidecar.InjectText(injectText); err != nil {
			b.logger.Error("bridge: failed to inject availability notice", "error", err)
		}
	}()
}

// timePreferencePattern matches user saying time preferences
var timePreferencePattern = regexp.MustCompile(`(?i)(?:after|before|around|morning|afternoon|evening|weekday|monday|tuesday|wednesday|thursday|friday|saturday|sunday)`)

// maybeFilterAndInjectSlots listens for user time preferences and injects only matching slots.
func (b *Bridge) maybeFilterAndInjectSlots(ctx context.Context, text string) {
	b.mu.Lock()
	if b.timePreferenceInjected || len(b.fetchedSlots) == 0 {
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()

	role, cleanText := parseTranscriptRoleAndText(text)
	if role != "user" {
		return
	}

	// Check if user is expressing time preferences
	if !timePreferencePattern.MatchString(cleanText) {
		return
	}

	b.mu.Lock()
	if b.timePreferenceInjected {
		b.mu.Unlock()
		return
	}
	b.timePreferenceInjected = true
	slots := b.fetchedSlots
	b.mu.Unlock()

	b.logger.Info("bridge: detected time preference, filtering slots",
		"preference", cleanText, "total_slots", len(slots))

	// Parse preferences using the existing SMS preference parser
	prefs := parseVoiceTimePreferences(cleanText)

	// Filter slots
	var matched []boulevard.TimeSlot
	for _, s := range slots {
		if matchesVoicePreferences(s.StartAt, prefs) {
			matched = append(matched, s)
		}
	}

	// If no matches, fall back to all slots
	if len(matched) == 0 {
		b.logger.Warn("bridge: no slots match preferences, using all slots", "preference", cleanText)
		matched = slots
	}

	// Limit to 3 for voice
	if len(matched) > 3 {
		matched = matched[:3]
	}

	var slotLines []string
	for _, s := range matched {
		slotLines = append(slotLines, fmt.Sprintf("- %s", s.StartAt.Format("Monday, January 2 at 3:04 PM")))
	}

	injectText := fmt.Sprintf("[SYSTEM: Here are the ONLY appointment times that match the patient's preferences:\n%s\n"+
		"Offer EXACTLY these times. Say each one clearly with day of week, date, and time. "+
		"Do NOT offer any other times. Do NOT modify these times. Read them EXACTLY as written.]",
		strings.Join(slotLines, "\n"))

	b.logger.Info("bridge: injecting filtered slots",
		"matched", len(matched), "preference", cleanText)

	if err := b.sidecar.InjectText(injectText); err != nil {
		b.logger.Error("bridge: failed to inject filtered slots", "error", err)
	}
}

// voiceTimePrefs holds parsed time preferences for voice filtering.
type voiceTimePrefs struct {
	daysOfWeek []time.Weekday
	afterHour  int // -1 if not set
	beforeHour int // -1 if not set
}

func parseVoiceTimePreferences(text string) voiceTimePrefs {
	p := voiceTimePrefs{afterHour: -1, beforeHour: -1}
	lower := strings.ToLower(text)

	// Parse days
	dayMap := map[string]time.Weekday{
		"monday": time.Monday, "tuesday": time.Tuesday, "wednesday": time.Wednesday,
		"thursday": time.Thursday, "friday": time.Friday, "saturday": time.Saturday, "sunday": time.Sunday,
	}
	for name, day := range dayMap {
		if strings.Contains(lower, name) {
			p.daysOfWeek = append(p.daysOfWeek, day)
		}
	}
	// "weekday" means Mon-Fri
	if strings.Contains(lower, "weekday") {
		p.daysOfWeek = []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
	}

	// Parse time preferences — handle both digits and spoken words
	wordToNum := map[string]int{
		"one": 1, "two": 2, "three": 3, "four": 4, "five": 5,
		"six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10,
		"eleven": 11, "twelve": 12,
	}
	afterPattern := regexp.MustCompile(`(?i)after\s+(\w+)(?::(\d{2}))?\s*(?:o'clock\s*)?(?:p\.?m\.?)?`)
	if m := afterPattern.FindStringSubmatch(lower); len(m) >= 2 {
		hour := 0
		if n, ok := wordToNum[m[1]]; ok {
			hour = n
		} else {
			fmt.Sscanf(m[1], "%d", &hour)
		}
		if hour > 0 && hour < 12 {
			hour += 12 // assume PM for voice
		}
		p.afterHour = hour
	}

	// "morning" = before noon, "afternoon" = after noon, "evening" = after 5
	if strings.Contains(lower, "morning") {
		p.beforeHour = 12
	}
	if strings.Contains(lower, "afternoon") {
		p.afterHour = 12
	}
	if strings.Contains(lower, "evening") {
		p.afterHour = 17
	}

	return p
}

func matchesVoicePreferences(slotTime time.Time, prefs voiceTimePrefs) bool {
	// Check day of week
	if len(prefs.daysOfWeek) > 0 {
		match := false
		for _, d := range prefs.daysOfWeek {
			if slotTime.Weekday() == d {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}

	// Check after hour
	if prefs.afterHour >= 0 && slotTime.Hour() < prefs.afterHour {
		return false
	}

	// Check before hour
	if prefs.beforeHour >= 0 && slotTime.Hour() >= prefs.beforeHour {
		return false
	}

	return true
}
