// Package rebooking provides proactive rebooking reminders for medspa treatments.
// It tracks treatment durations, schedules follow-up outreach when treatments wear off,
// and handles the patient response flow (YES → rebook, STOP → dismiss).
package rebooking

import (
	"strings"
	"time"
)

// TreatmentDuration defines when a patient should be reminded to rebook
// after receiving a treatment. MinWeeks is the earliest outreach, MaxWeeks
// the latest reasonable window.
type TreatmentDuration struct {
	Service  string
	MinWeeks int
	MaxWeeks int
	// IsSeries indicates treatments done in a series (e.g., microneedling 4-6 sessions).
	IsSeries bool
}

// DefaultTreatmentDurations returns the standard rebooking intervals for common medspa services.
func DefaultTreatmentDurations() []TreatmentDuration {
	return []TreatmentDuration{
		{Service: "botox", MinWeeks: 10, MaxWeeks: 14, IsSeries: false},
		{Service: "tox", MinWeeks: 10, MaxWeeks: 14, IsSeries: false},
		{Service: "dermal filler", MinWeeks: 26, MaxWeeks: 52, IsSeries: false},
		{Service: "lip filler", MinWeeks: 26, MaxWeeks: 52, IsSeries: false},
		{Service: "filler", MinWeeks: 26, MaxWeeks: 52, IsSeries: false},
		{Service: "microneedling", MinWeeks: 4, MaxWeeks: 6, IsSeries: true},
		{Service: "chemical peel", MinWeeks: 4, MaxWeeks: 6, IsSeries: true},
		{Service: "laser hair removal", MinWeeks: 4, MaxWeeks: 6, IsSeries: true},
		{Service: "weight loss", MinWeeks: 4, MaxWeeks: 5, IsSeries: false},
	}
}

// treatmentDurations is the lookup map built from defaults. Keys are normalized.
var treatmentDurations map[string]TreatmentDuration

func init() {
	treatmentDurations = make(map[string]TreatmentDuration)
	for _, td := range DefaultTreatmentDurations() {
		treatmentDurations[td.Service] = td
	}
}

// normalizeService lowercases and trims a service name for lookup.
func normalizeService(service string) string {
	return strings.ToLower(strings.TrimSpace(service))
}

// LookupDuration finds the treatment duration config for a service.
// It checks exact match first, then substring containment.
// Returns the duration and true if found, zero value and false otherwise.
func LookupDuration(service string) (TreatmentDuration, bool) {
	key := normalizeService(service)
	if td, ok := treatmentDurations[key]; ok {
		return td, true
	}
	// Fuzzy: check if key contains a known service or vice versa.
	var best TreatmentDuration
	bestLen := 0
	for svcKey, td := range treatmentDurations {
		if strings.Contains(key, svcKey) || strings.Contains(svcKey, key) {
			if len(svcKey) > bestLen {
				best = td
				bestLen = len(svcKey)
			}
		}
	}
	if bestLen > 0 {
		return best, true
	}
	return TreatmentDuration{}, false
}

// RebookAfter calculates the rebooking reminder date using the minimum weeks interval.
func RebookAfter(service string, bookedAt time.Time) (time.Time, bool) {
	td, ok := LookupDuration(service)
	if !ok {
		return time.Time{}, false
	}
	return bookedAt.Add(time.Duration(td.MinWeeks) * 7 * 24 * time.Hour), true
}
