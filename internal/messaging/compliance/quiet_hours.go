package compliance

import (
	"fmt"
	"time"
)

// Purpose distinguishes transactional vs marketing messages.
type Purpose string

const (
	PurposeTransactional Purpose = "transactional"
	PurposeMarketing     Purpose = "marketing"
)

// QuietHours represents a daily window (local time) when marketing sends are suppressed.
type QuietHours struct {
	StartMinutes int
	EndMinutes   int
	location     *time.Location
	enabled      bool
}

// ParseQuietHours returns a quiet-hours window from HH:MM strings.
func ParseQuietHours(start, end, tz string) (QuietHours, error) {
	loc := time.UTC
	if tz != "" {
		var err error
		loc, err = time.LoadLocation(tz)
		if err != nil {
			return QuietHours{}, fmt.Errorf("compliance: load quiet hours tz: %w", err)
		}
	}
	startMin, err := parseClock(start)
	if err != nil {
		return QuietHours{}, fmt.Errorf("compliance: parse quiet hours start: %w", err)
	}
	endMin, err := parseClock(end)
	if err != nil {
		return QuietHours{}, fmt.Errorf("compliance: parse quiet hours end: %w", err)
	}
	return QuietHours{
		StartMinutes: startMin,
		EndMinutes:   endMin,
		location:     loc,
		enabled:      true,
	}, nil
}

func parseClock(v string) (int, error) {
	if v == "" {
		return 0, fmt.Errorf("empty clock")
	}
	t, err := time.Parse("15:04", v)
	if err != nil {
		return 0, err
	}
	return t.Hour()*60 + t.Minute(), nil
}

// Suppress returns true when the given moment falls inside the quiet-hours window for marketing sends.
func (q QuietHours) Suppress(now time.Time, purpose Purpose) bool {
	if !q.enabled || purpose != PurposeMarketing {
		return false
	}
	local := now.In(q.location)
	minutes := local.Hour()*60 + local.Minute()
	if q.StartMinutes == q.EndMinutes {
		return false
	}
	if q.StartMinutes < q.EndMinutes {
		return minutes >= q.StartMinutes && minutes < q.EndMinutes
	}
	// Window crosses midnight.
	return minutes >= q.StartMinutes || minutes < q.EndMinutes
}
