package rebooking

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// MessageTemplate generates a personalized rebooking outreach message.
func MessageTemplate(r *Reminder, clinicName string) string {
	name := r.PatientName
	if name == "" {
		name = "there"
	}

	duration := humanDuration(r.BookedAt, r.RebookAfter)
	service := r.Service

	td, found := LookupDuration(service)
	if found && td.IsSeries {
		return seriesTemplate(name, service, duration, clinicName)
	}

	switch normalizeService(service) {
	case "botox", "tox":
		return fmt.Sprintf(
			"Hi %s! 💉 It's been about %s since your Botox at %s. Ready to keep those results fresh? Reply YES and I'll find available times for you!",
			name, duration, clinicName,
		)
	case "dermal filler", "filler":
		return fmt.Sprintf(
			"Hi %s! It's been %s since your filler appointment at %s. Thinking about a touch-up? Reply YES and I'll check availability for you 😊",
			name, duration, clinicName,
		)
	case "lip filler":
		return fmt.Sprintf(
			"Hi %s! 💋 It's been %s since your lip filler at %s. Ready for a refresh? Reply YES and I'll find times that work for you!",
			name, duration, clinicName,
		)
	case "weight loss":
		return fmt.Sprintf(
			"Hi %s! It's time for your monthly weight loss follow-up at %s. Reply YES to schedule your next appointment!",
			name, clinicName,
		)
	default:
		return fmt.Sprintf(
			"Hi %s! It's been %s since your %s at %s. Ready to schedule your next session? Reply YES and I'll find available times for you!",
			name, duration, service, clinicName,
		)
	}
}

func seriesTemplate(name, service, duration, clinicName string) string {
	return fmt.Sprintf(
		"Hi %s! It's been %s since your last %s session at %s. Ready to continue your series? Reply YES to book your next appointment!",
		name, duration, strings.ToLower(service), clinicName,
	)
}

// OptOutResponse returns the message sent when a patient opts out.
func OptOutResponse(clinicName string) string {
	return fmt.Sprintf(
		"No problem at all! We've removed this reminder. If you'd like to book in the future, just text us anytime. — %s",
		clinicName,
	)
}

// RebookConfirmation returns the message when a patient wants to rebook.
func RebookConfirmation(r *Reminder, clinicName string) string {
	name := r.PatientName
	if name == "" {
		name = "there"
	}
	return fmt.Sprintf(
		"Great, %s! Let me find available times for your %s at %s. One moment... 🗓️",
		name, r.Service, clinicName,
	)
}

func humanDuration(from, to time.Time) string {
	diff := to.Sub(from)
	weeks := int(math.Round(diff.Hours() / (24 * 7)))
	if weeks <= 0 {
		weeks = 1
	}
	if weeks >= 52 {
		months := weeks / 4
		return fmt.Sprintf("%d months", months)
	}
	if weeks > 12 {
		months := weeks / 4
		return fmt.Sprintf("%d months", months)
	}
	return fmt.Sprintf("%d weeks", weeks)
}
