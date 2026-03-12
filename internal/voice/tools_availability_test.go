package voice

import (
	"testing"
	"time"
)

func TestParseAfterHour(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{name: "after four bare", in: "after 4", want: 16},
		{name: "after four pm", in: "after 4pm", want: 16},
		{name: "after three colon pm", in: "after 3:00 PM", want: 15},
		{name: "past five", in: "past 5", want: 17},
		{name: "morning explicit am", in: "after 10am", want: 10},
		{name: "no pattern", in: "morning only", want: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseAfterHour(tt.in); got != tt.want {
				t.Fatalf("parseAfterHour(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestFilterSlotByTime_AfterHourIsStrictlyExclusive(t *testing.T) {
	loc := time.UTC
	slotAtFour := time.Date(2026, 3, 20, 16, 0, 0, 0, loc)
	slotAtFourThirty := time.Date(2026, 3, 20, 16, 30, 0, 0, loc)
	slotAtFive := time.Date(2026, 3, 20, 17, 0, 0, 0, loc)

	if filterSlotByTime(slotAtFour, 16, "") {
		t.Fatal("expected 4:00 PM to be excluded for 'after 4'")
	}
	if !filterSlotByTime(slotAtFourThirty, 16, "") {
		t.Fatal("expected 4:30 PM to be included for 'after 4'")
	}
	if !filterSlotByTime(slotAtFive, 16, "") {
		t.Fatal("expected 5:00 PM to be included for 'after 4'")
	}
}
