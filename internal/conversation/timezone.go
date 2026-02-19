package conversation

import (
	"fmt"
	"time"
)

// ParseSlotTime parses a time string from a booking platform API (e.g., Moxie)
// into a time.Time in the clinic's local timezone. Handles:
//   - RFC3339 with offset: "2006-01-02T15:04:05-05:00"
//   - RFC3339 UTC: "2006-01-02T15:04:05Z"
//   - Naive datetime (no timezone): "2006-01-02T15:04:05" — treated as clinic local
func ParseSlotTime(raw string, clinicTimezone string) (time.Time, error) {
	loc, err := time.LoadLocation(clinicTimezone)
	if err != nil {
		loc = time.UTC
	}

	// Try RFC3339 first (has timezone info)
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.In(loc), nil
	}

	// Try with explicit offset
	if t, err := time.Parse("2006-01-02T15:04:05-07:00", raw); err == nil {
		return t.In(loc), nil
	}

	// Naive datetime — assume clinic local time
	if t, err := time.ParseInLocation("2006-01-02T15:04:05", raw, loc); err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("cannot parse slot time %q", raw)
}

// ClinicLocation returns the *time.Location for a clinic timezone string.
// Falls back to UTC if the timezone is invalid or empty.
func ClinicLocation(timezone string) *time.Location {
	if timezone == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// FormatAppointmentConfirmation builds a standardized booking confirmation message.
func FormatAppointmentConfirmation(service string, appointmentTime time.Time, clinicName string) string {
	dateStr := appointmentTime.Format("Monday, January 2")
	tzAbbrev := appointmentTime.Format("MST")
	timeStr := appointmentTime.Format("3:04 PM") + " " + tzAbbrev

	return fmt.Sprintf(
		"Payment received and your appointment is booked! 🎉\n\n"+
			"📋 %s\n"+
			"📅 %s at %s\n"+
			"📍 %s\n\n"+
			"Reminder: There is a 24-hour cancellation policy. Cancellations made less than 24 hours before your appointment are non-refundable.",
		service, dateStr, timeStr, clinicName)
}
