package compliance

import (
	"testing"
	"time"
)

func TestQuietHoursSuppressDaytimeWindow(t *testing.T) {
	q, err := ParseQuietHours("21:00", "07:30", "UTC")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tests := []struct {
		ts      string
		want    bool
		purpose Purpose
	}{
		{"2024-10-05T22:00:00Z", true, PurposeMarketing},
		{"2024-10-05T06:59:00Z", true, PurposeMarketing},
		{"2024-10-05T08:00:00Z", false, PurposeMarketing},
		{"2024-10-05T22:00:00Z", false, PurposeTransactional},
	}
	for _, tc := range tests {
		ts, _ := time.Parse(time.RFC3339, tc.ts)
		if got := q.Suppress(ts, tc.purpose); got != tc.want {
			t.Fatalf("Suppress(%s,%s)=%v want %v", tc.ts, tc.purpose, got, tc.want)
		}
	}
}

func TestQuietHoursSuppressSimpleWindow(t *testing.T) {
	q, err := ParseQuietHours("22:00", "23:00", "UTC")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ts, _ := time.Parse(time.RFC3339, "2024-10-05T22:30:00Z")
	if !q.Suppress(ts, PurposeMarketing) {
		t.Fatalf("expected suppression")
	}
	ts, _ = time.Parse(time.RFC3339, "2024-10-05T21:30:00Z")
	if q.Suppress(ts, PurposeMarketing) {
		t.Fatalf("expected no suppression")
	}
}

func TestParseQuietHoursValidationErrors(t *testing.T) {
	if _, err := ParseQuietHours("", "07:00", "UTC"); err == nil {
		t.Fatalf("expected error for empty start clock")
	}
	if _, err := ParseQuietHours("07:00", "08:00", "Mars/Phobos"); err == nil {
		t.Fatalf("expected error for invalid timezone")
	}
	if _, err := ParseQuietHours("bad", "08:00", "UTC"); err == nil {
		t.Fatalf("expected error for malformed start time")
	}
}

func TestQuietHoursSuppressDisabledOrNonMarketing(t *testing.T) {
	var q QuietHours
	now := time.Now()
	if q.Suppress(now, PurposeMarketing) {
		t.Fatalf("zero quiet hours should be disabled")
	}
	parsed, err := ParseQuietHours("00:00", "23:59", "UTC")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Suppress(now, PurposeTransactional) {
		t.Fatalf("transactional sends should bypass quiet hours")
	}
}
