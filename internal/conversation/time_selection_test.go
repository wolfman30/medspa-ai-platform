package conversation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
)

func TestDetectTimeSelection(t *testing.T) {
	// Create sample presented slots
	// Feb 9, 2026 is a Monday, Feb 12, 2026 is a Thursday
	baseTime := time.Date(2026, 2, 9, 10, 0, 0, 0, time.Local)
	slots := []PresentedSlot{
		{Index: 1, DateTime: baseTime, TimeStr: "Mon Feb 9 at 10:00 AM"},
		{Index: 2, DateTime: baseTime.Add(90 * time.Minute), TimeStr: "Mon Feb 9 at 11:30 AM"},
		{Index: 3, DateTime: time.Date(2026, 2, 12, 14, 0, 0, 0, time.Local), TimeStr: "Thu Feb 12 at 2:00 PM"},
		{Index: 4, DateTime: time.Date(2026, 2, 12, 15, 30, 0, 0, time.Local), TimeStr: "Thu Feb 12 at 3:30 PM"},
	}

	tests := []struct {
		name          string
		message       string
		expectedIndex int // 0 means no selection detected
	}{
		// Simple number selections (most important for MVP)
		{name: "just number 1", message: "1", expectedIndex: 1},
		{name: "just number 2", message: "2", expectedIndex: 2},
		{name: "just number 3", message: "3", expectedIndex: 3},
		{name: "number out of range", message: "5", expectedIndex: 0},
		{name: "zero", message: "0", expectedIndex: 0},

		// Option N format
		{name: "option 1", message: "option 1", expectedIndex: 1},
		{name: "Option 2", message: "Option 2", expectedIndex: 2},
		{name: "number 3", message: "number 3", expectedIndex: 3},
		{name: "choice 4", message: "choice 4", expectedIndex: 4},
		{name: "#1", message: "#1", expectedIndex: 1},
		{name: "#2", message: "#2", expectedIndex: 2},

		// Ordinal words
		{name: "first one", message: "the first one", expectedIndex: 1},
		{name: "second", message: "second", expectedIndex: 2},
		{name: "third one please", message: "third one please", expectedIndex: 3},
		{name: "1st", message: "1st", expectedIndex: 1},
		{name: "2nd option", message: "2nd option", expectedIndex: 2},

		// Time-based selection (10am matches slot 1 at 10:00 AM)
		{name: "10:00 AM", message: "10:00 AM", expectedIndex: 1},
		{name: "10am", message: "10am", expectedIndex: 1},
		{name: "11:30 am", message: "11:30 am", expectedIndex: 2},

		// No selection
		{name: "random text", message: "what are your hours?", expectedIndex: 0},
		{name: "empty", message: "", expectedIndex: 0},
		{name: "question about options", message: "can you show me more options?", expectedIndex: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectTimeSelection(tt.message, slots, TimePreferences{})

			if tt.expectedIndex == 0 {
				assert.Nil(t, result, "expected no selection for message: %s", tt.message)
			} else {
				assert.NotNil(t, result, "expected selection for message: %s", tt.message)
				if result != nil {
					assert.Equal(t, tt.expectedIndex, result.Index, "wrong slot selected for message: %s", tt.message)
				}
			}
		})
	}
}

func TestDetectTimeSelection_EmptySlots(t *testing.T) {
	result := DetectTimeSelection("1", []PresentedSlot{}, TimePreferences{})
	assert.Nil(t, result)

	result = DetectTimeSelection("1", nil, TimePreferences{})
	assert.Nil(t, result)
}

func TestFormatTimeSlotsForSMS(t *testing.T) {
	slots := []PresentedSlot{
		{Index: 1, TimeStr: "Mon Feb 10 at 10:00 AM"},
		{Index: 2, TimeStr: "Mon Feb 10 at 11:30 AM"},
		{Index: 3, TimeStr: "Thu Feb 13 at 2:00 PM"},
	}

	t.Run("exact match", func(t *testing.T) {
		result := FormatTimeSlotsForSMS(slots, "Botox", true)

		assert.Contains(t, result, "Botox")
		assert.Contains(t, result, "1. Mon Feb 10 at 10:00 AM")
		assert.Contains(t, result, "2. Mon Feb 10 at 11:30 AM")
		assert.Contains(t, result, "3. Thu Feb 13 at 2:00 PM")
		assert.Contains(t, result, "Reply with the number")
		assert.NotContains(t, result, "couldn't find exact matches")
	})

	t.Run("no exact match", func(t *testing.T) {
		result := FormatTimeSlotsForSMS(slots, "Botox", false)

		assert.Contains(t, result, "couldn't find exact matches")
		assert.Contains(t, result, "Botox")
		assert.Contains(t, result, "1. Mon Feb 10 at 10:00 AM")
	})

	t.Run("empty slots", func(t *testing.T) {
		result := FormatTimeSlotsForSMS([]PresentedSlot{}, "Botox", true)

		assert.Contains(t, result, "couldn't find any available times")
		assert.Contains(t, result, "Botox")
	})
}

func TestFormatSlotForDisplay(t *testing.T) {
	tests := []struct {
		name     string
		time     time.Time
		expected string
	}{
		{
			name:     "morning time",
			time:     time.Date(2026, 2, 9, 10, 0, 0, 0, time.Local), // Feb 9, 2026 = Monday
			expected: "Mon Feb 9 at 10:00 AM",
		},
		{
			name:     "afternoon time",
			time:     time.Date(2026, 2, 13, 14, 30, 0, 0, time.Local), // Feb 13, 2026 = Friday
			expected: "Fri Feb 13 at 2:30 PM",
		},
		{
			name:     "noon",
			time:     time.Date(2026, 2, 15, 12, 0, 0, 0, time.Local), // Feb 15, 2026 = Sunday
			expected: "Sun Feb 15 at 12:00 PM",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatSlotForDisplay(tt.time)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMatchesTimePreferences(t *testing.T) {
	tests := []struct {
		name     string
		slotTime time.Time
		prefs    TimePreferences
		expected bool
	}{
		{
			name:     "no preferences - match",
			slotTime: time.Date(2026, 2, 10, 10, 0, 0, 0, time.Local),
			prefs:    TimePreferences{},
			expected: true,
		},
		{
			name:     "after 4pm - slot at 5pm - match",
			slotTime: time.Date(2026, 2, 10, 17, 0, 0, 0, time.Local),
			prefs:    TimePreferences{AfterTime: "16:00"},
			expected: true,
		},
		{
			name:     "after 4pm - slot at 3pm - no match",
			slotTime: time.Date(2026, 2, 10, 15, 0, 0, 0, time.Local),
			prefs:    TimePreferences{AfterTime: "16:00"},
			expected: false,
		},
		{
			name:     "before noon - slot at 10am - match",
			slotTime: time.Date(2026, 2, 10, 10, 0, 0, 0, time.Local),
			prefs:    TimePreferences{BeforeTime: "12:00"},
			expected: true,
		},
		{
			name:     "before noon - slot at 2pm - no match",
			slotTime: time.Date(2026, 2, 10, 14, 0, 0, 0, time.Local),
			prefs:    TimePreferences{BeforeTime: "12:00"},
			expected: false,
		},
		{
			name:     "between 9am and 5pm - slot at 2pm - match",
			slotTime: time.Date(2026, 2, 10, 14, 0, 0, 0, time.Local),
			prefs:    TimePreferences{AfterTime: "09:00", BeforeTime: "17:00"},
			expected: true,
		},
		{
			name:     "between 9am and 5pm - slot at 6pm - no match",
			slotTime: time.Date(2026, 2, 10, 18, 0, 0, 0, time.Local),
			prefs:    TimePreferences{AfterTime: "09:00", BeforeTime: "17:00"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesTimePreferences(tt.slotTime, tt.prefs)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseTimeToMinutes(t *testing.T) {
	tests := []struct {
		timeStr  string
		expected int
	}{
		{"09:00", 540},  // 9am
		{"12:00", 720},  // noon
		{"16:00", 960},  // 4pm
		{"17:30", 1050}, // 5:30pm
		{"00:00", 0},    // midnight
		{"23:59", 1439}, // just before midnight
		{"invalid", 0},  // invalid format
		{"", 0},         // empty
	}

	for _, tt := range tests {
		t.Run(tt.timeStr, func(t *testing.T) {
			result := parseTimeToMinutes(tt.timeStr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatTimeSelectionConfirmation(t *testing.T) {
	// Feb 9, 2026 is a Monday
	selectedTime := time.Date(2026, 2, 9, 10, 0, 0, 0, time.Local)
	result := FormatTimeSelectionConfirmation(selectedTime, "Botox", 5000)

	assert.Contains(t, result, "Monday, February 9 at 10:00 AM")
	assert.Contains(t, result, "Botox")
	assert.Contains(t, result, "$50")
	assert.Contains(t, result, "refundable deposit")
}

func TestFormatSlotNoLongerAvailableMessage(t *testing.T) {
	selectedTime := time.Date(2026, 2, 10, 10, 0, 0, 0, time.Local)

	t.Run("with remaining slots", func(t *testing.T) {
		remaining := []PresentedSlot{
			{Index: 1, TimeStr: "Mon Feb 10 at 11:30 AM"},
			{Index: 2, TimeStr: "Thu Feb 13 at 2:00 PM"},
		}
		result := FormatSlotNoLongerAvailableMessage(selectedTime, remaining)

		assert.Contains(t, result, "10:00 AM slot was just booked")
		assert.Contains(t, result, "1. Mon Feb 10 at 11:30 AM")
		assert.Contains(t, result, "2. Thu Feb 13 at 2:00 PM")
	})

	t.Run("no remaining slots", func(t *testing.T) {
		result := FormatSlotNoLongerAvailableMessage(selectedTime, []PresentedSlot{})

		assert.Contains(t, result, "10:00 AM slot was just booked")
		assert.Contains(t, result, "check for other available times")
	})
}

// dateAwareMock returns different responses per date string.
// makeSlotResponse builds an AvailabilityResponse with the given times.
// === Progressive Search + Adjacent Proposal Tests ===

func TestHumanizeDays(t *testing.T) {
	tests := []struct {
		days     int
		expected string
	}{
		{7, "week"},
		{14, "2 weeks"},
		{21, "3 weeks"},
		{28, "month"},
		{56, "2 months"},
		{84, "3 months"},
		{90, "3 months"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, humanizeDays(tt.days))
		})
	}
}

// === Progress Callback Tests ===

// === Disambiguation Tests (TDD: these should FAIL until implementation is added) ===

func TestDetectTimeSelection_BareHour_SingleMatch(t *testing.T) {
	// Patient says "6" and only 6:00 PM exists among presented slots → select it
	slots := []PresentedSlot{
		{Index: 1, DateTime: time.Date(2026, 2, 9, 10, 0, 0, 0, time.Local), TimeStr: "Mon Feb 9 at 10:00 AM"},
		{Index: 2, DateTime: time.Date(2026, 2, 9, 14, 0, 0, 0, time.Local), TimeStr: "Mon Feb 9 at 2:00 PM"},
		{Index: 3, DateTime: time.Date(2026, 2, 12, 18, 0, 0, 0, time.Local), TimeStr: "Thu Feb 12 at 6:00 PM"},
	}

	result := DetectTimeSelection("6", slots, TimePreferences{})
	require.NotNil(t, result, "bare hour '6' should match the 6:00 PM slot")
	assert.Equal(t, 3, result.Index)
}

func TestDetectTimeSelection_BareHour_AmbiguousWithPrefs(t *testing.T) {
	// Patient says "6" and BOTH 6:00 AM and 6:00 PM exist,
	// but patient previously said "after 3pm" → should pick 6:00 PM
	slots := []PresentedSlot{
		{Index: 1, DateTime: time.Date(2026, 2, 9, 6, 0, 0, 0, time.Local), TimeStr: "Mon Feb 9 at 6:00 AM"},
		{Index: 2, DateTime: time.Date(2026, 2, 9, 10, 0, 0, 0, time.Local), TimeStr: "Mon Feb 9 at 10:00 AM"},
		{Index: 3, DateTime: time.Date(2026, 2, 12, 18, 0, 0, 0, time.Local), TimeStr: "Thu Feb 12 at 6:00 PM"},
	}
	prefs := TimePreferences{AfterTime: "15:00"} // after 3pm

	result := DetectTimeSelection("6", slots, prefs)
	require.NotNil(t, result, "bare hour '6' with 'after 3pm' prefs should select 6:00 PM")
	assert.Equal(t, 3, result.Index)
}

func TestDetectTimeSelection_BareHour_AmbiguousNoPrefs(t *testing.T) {
	// Patient says "6" and BOTH 6:00 AM and 6:00 PM exist,
	// NO time preference → return nil (LLM will ask for clarification)
	slots := []PresentedSlot{
		{Index: 1, DateTime: time.Date(2026, 2, 9, 6, 0, 0, 0, time.Local), TimeStr: "Mon Feb 9 at 6:00 AM"},
		{Index: 2, DateTime: time.Date(2026, 2, 9, 10, 0, 0, 0, time.Local), TimeStr: "Mon Feb 9 at 10:00 AM"},
		{Index: 3, DateTime: time.Date(2026, 2, 12, 18, 0, 0, 0, time.Local), TimeStr: "Thu Feb 12 at 6:00 PM"},
	}

	result := DetectTimeSelection("6", slots, TimePreferences{})
	// Should NOT match because 6 is ambiguous (6am vs 6pm) and there are only 3 slots (so 6 is out of index range)
	// But the system should also not match it as an index since 6 > len(slots)
	// This should return nil — LLM asks for clarification
	assert.Nil(t, result, "bare hour '6' with 6am AND 6pm and no prefs should return nil for clarification")
}

func TestDetectTimeSelection_BareHour_PrefersMorning(t *testing.T) {
	// Patient says "9" and both 9:00 AM exists,
	// patient has "before noon" preference → pick 9:00 AM
	slots := []PresentedSlot{
		{Index: 1, DateTime: time.Date(2026, 2, 9, 9, 0, 0, 0, time.Local), TimeStr: "Mon Feb 9 at 9:00 AM"},
		{Index: 2, DateTime: time.Date(2026, 2, 9, 14, 0, 0, 0, time.Local), TimeStr: "Mon Feb 9 at 2:00 PM"},
		{Index: 3, DateTime: time.Date(2026, 2, 12, 15, 0, 0, 0, time.Local), TimeStr: "Thu Feb 12 at 3:00 PM"},
	}
	prefs := TimePreferences{BeforeTime: "12:00"} // morning

	result := DetectTimeSelection("9", slots, prefs)
	require.NotNil(t, result, "bare hour '9' with morning prefs should select 9:00 AM")
	assert.Equal(t, 1, result.Index)
}

func TestDetectTimeSelection_NaturalPhrase_IllTakeThe2pm(t *testing.T) {
	// Patient says "I'll take the 2pm" → should match the 2:00 PM slot
	slots := []PresentedSlot{
		{Index: 1, DateTime: time.Date(2026, 2, 9, 10, 0, 0, 0, time.Local), TimeStr: "Mon Feb 9 at 10:00 AM"},
		{Index: 2, DateTime: time.Date(2026, 2, 9, 11, 30, 0, 0, time.Local), TimeStr: "Mon Feb 9 at 11:30 AM"},
		{Index: 3, DateTime: time.Date(2026, 2, 12, 14, 0, 0, 0, time.Local), TimeStr: "Thu Feb 12 at 2:00 PM"},
	}

	result := DetectTimeSelection("I'll take the 2pm", slots, TimePreferences{})
	require.NotNil(t, result, "'I'll take the 2pm' should match the 2:00 PM slot")
	assert.Equal(t, 3, result.Index)
}

func TestDetectTimeSelection_NaturalPhrase_IWant6(t *testing.T) {
	// Patient says "I want 6" with afterTime preference → should pick 6:00 PM
	slots := []PresentedSlot{
		{Index: 1, DateTime: time.Date(2026, 2, 9, 6, 0, 0, 0, time.Local), TimeStr: "Mon Feb 9 at 6:00 AM"},
		{Index: 2, DateTime: time.Date(2026, 2, 9, 14, 0, 0, 0, time.Local), TimeStr: "Mon Feb 9 at 2:00 PM"},
		{Index: 3, DateTime: time.Date(2026, 2, 12, 18, 0, 0, 0, time.Local), TimeStr: "Thu Feb 12 at 6:00 PM"},
	}
	prefs := TimePreferences{AfterTime: "15:00"} // after 3pm

	result := DetectTimeSelection("I want 6", slots, prefs)
	require.NotNil(t, result, "'I want 6' with 'after 3pm' prefs should select 6:00 PM")
	assert.Equal(t, 3, result.Index)
}

func TestDetectTimeSelection_BareHour_IndexTakesPriority(t *testing.T) {
	// Patient says "3" and there are 4 slots. Slot 3 is at 2:00 PM.
	// No slot is at 3:00 hour. So "3" should be treated as slot index 3.
	slots := []PresentedSlot{
		{Index: 1, DateTime: time.Date(2026, 2, 9, 10, 0, 0, 0, time.Local), TimeStr: "Mon Feb 9 at 10:00 AM"},
		{Index: 2, DateTime: time.Date(2026, 2, 9, 11, 30, 0, 0, time.Local), TimeStr: "Mon Feb 9 at 11:30 AM"},
		{Index: 3, DateTime: time.Date(2026, 2, 12, 14, 0, 0, 0, time.Local), TimeStr: "Thu Feb 12 at 2:00 PM"},
		{Index: 4, DateTime: time.Date(2026, 2, 12, 15, 30, 0, 0, time.Local), TimeStr: "Thu Feb 12 at 3:30 PM"},
	}

	result := DetectTimeSelection("3", slots, TimePreferences{})
	require.NotNil(t, result, "'3' as a bare number should match slot index 3")
	assert.Equal(t, 3, result.Index)
}

func TestDetectTimeSelection_MoreTimesRequest(t *testing.T) {
	slots := []PresentedSlot{
		{Index: 1, DateTime: time.Date(2026, 2, 13, 20, 30, 0, 0, time.Local), TimeStr: "Fri Feb 13 at 8:30 PM"},
		{Index: 2, DateTime: time.Date(2026, 2, 19, 18, 45, 0, 0, time.Local), TimeStr: "Thu Feb 19 at 6:45 PM"},
		{Index: 3, DateTime: time.Date(2026, 2, 24, 18, 0, 0, 0, time.Local), TimeStr: "Tue Feb 24 at 6:00 PM"},
		{Index: 4, DateTime: time.Date(2026, 2, 26, 20, 15, 0, 0, time.Local), TimeStr: "Thu Feb 26 at 8:15 PM"},
		{Index: 5, DateTime: time.Date(2026, 3, 2, 15, 0, 0, 0, time.Local), TimeStr: "Mon Mar 2 at 3:00 PM"},
		{Index: 6, DateTime: time.Date(2026, 3, 4, 15, 0, 0, 0, time.Local), TimeStr: "Wed Mar 4 at 3:00 PM"},
	}
	prefs := TimePreferences{}

	// "Any later times on Mar 2 and 4th?" should NOT select slot 4 — it's asking for more times
	result := DetectTimeSelection("Any later times on Mar 2 and 4th?", slots, prefs)
	assert.Nil(t, result, "should not match — patient is asking for more times, not selecting slot 4")

	// "more times on Monday" should also not match
	result = DetectTimeSelection("Do you have more times on Monday?", slots, prefs)
	assert.Nil(t, result, "should not match — asking for more times")

	// "any other options?" should not match
	result = DetectTimeSelection("any other options?", slots, prefs)
	assert.Nil(t, result, "should not match — asking for other options")

	// But "4" by itself SHOULD still select slot 4
	result = DetectTimeSelection("4", slots, prefs)
	require.NotNil(t, result, "'4' should select slot 4")
	assert.Equal(t, 4, result.Index)

	// And "the 4th one" without date context should still work
	result = DetectTimeSelection("the 4th one", slots, prefs)
	require.NotNil(t, result, "'the 4th one' should select slot 4")
	assert.Equal(t, 4, result.Index)
}

func TestBuildRefinedTimePreferences_LaterOnSpecificDates(t *testing.T) {
	// Use fixed far-future dates to avoid test flakiness as calendar advances.
	// Jun 1, 2028 = Thursday (day 4), Jun 7, 2028 = Wednesday (day 3)
	slots := []PresentedSlot{
		{Index: 1, DateTime: time.Date(2028, 5, 20, 20, 30, 0, 0, time.Local), TimeStr: "Sat May 20 at 8:30 PM"},
		{Index: 4, DateTime: time.Date(2028, 6, 1, 15, 0, 0, 0, time.Local), TimeStr: "Thu Jun 1 at 3:00 PM"},
		{Index: 5, DateTime: time.Date(2028, 6, 7, 15, 0, 0, 0, time.Local), TimeStr: "Wed Jun 7 at 3:00 PM"},
	}

	prefs := leads.SchedulingPreferences{
		PreferredDays:  "thursdays wednesdays",
		PreferredTimes: "after 3pm",
	}

	refined := buildRefinedTimePreferences("Any later times on Jun 1 and 7th?", prefs, slots)

	// Should have shifted AfterTime past 3:00 PM (15:00 → 15:01)
	assert.Equal(t, "15:01", refined.AfterTime, "should shift after-time past the latest shown slot on those dates")

	// Should have days of week for Thu (4) and Wed (3) from the specific dates
	assert.Contains(t, refined.DaysOfWeek, 4, "should include Thursday")
	assert.Contains(t, refined.DaysOfWeek, 3, "should include Wednesday")
}

func TestExtractSpecificDates(t *testing.T) {
	dates := extractSpecificDates("any later times on jun 1 and 7th?")
	require.Len(t, dates, 2, "should extract 2 dates")
	assert.Equal(t, time.June, dates[0].Month())
	assert.Equal(t, 1, dates[0].Day())
	assert.Equal(t, time.June, dates[1].Month())
	assert.Equal(t, 7, dates[1].Day())
}

func TestFilterOutPreviousSlots(t *testing.T) {
	prev := []PresentedSlot{
		{DateTime: time.Date(2026, 3, 2, 15, 0, 0, 0, time.UTC)},
	}
	newSlots := []PresentedSlot{
		{DateTime: time.Date(2026, 3, 2, 15, 0, 0, 0, time.UTC)},
		{DateTime: time.Date(2026, 3, 2, 17, 0, 0, 0, time.UTC)},
	}
	filtered := filterOutPreviousSlots(newSlots, prev)
	assert.Len(t, filtered, 1, "should filter out the duplicate")
	assert.Equal(t, 17, filtered[0].DateTime.Hour())
}

func TestDetectTimeSelection_ShorthandTime_3p(t *testing.T) {
	// Patient says "3p" → should match 3:00 PM or 3:30 PM slot
	slots := []PresentedSlot{
		{Index: 1, DateTime: time.Date(2026, 2, 9, 10, 0, 0, 0, time.Local), TimeStr: "Mon Feb 9 at 10:00 AM"},
		{Index: 2, DateTime: time.Date(2026, 2, 12, 15, 0, 0, 0, time.Local), TimeStr: "Thu Feb 12 at 3:00 PM"},
		{Index: 3, DateTime: time.Date(2026, 2, 12, 16, 0, 0, 0, time.Local), TimeStr: "Thu Feb 12 at 4:00 PM"},
	}

	result := DetectTimeSelection("3p", slots, TimePreferences{})
	require.NotNil(t, result, "'3p' should match the 3:00 PM slot")
	assert.Equal(t, 2, result.Index)
}

func TestDetectTimeSelection_DateBased(t *testing.T) {
	slots := []PresentedSlot{
		{Index: 1, DateTime: time.Date(2026, 2, 24, 10, 0, 0, 0, time.Local), TimeStr: "Mon Feb 24 at 10:00 AM"},
		{Index: 2, DateTime: time.Date(2026, 2, 25, 14, 0, 0, 0, time.Local), TimeStr: "Tue Feb 25 at 2:00 PM"},
		{Index: 3, DateTime: time.Date(2026, 2, 26, 11, 0, 0, 0, time.Local), TimeStr: "Wed Feb 26 at 11:00 AM"},
		{Index: 4, DateTime: time.Date(2026, 2, 27, 15, 0, 0, 0, time.Local), TimeStr: "Thu Feb 27 at 3:00 PM"},
		{Index: 5, DateTime: time.Date(2026, 2, 28, 9, 0, 0, 0, time.Local), TimeStr: "Fri Feb 28 at 9:00 AM"},
	}
	noPrefs := TimePreferences{}

	tests := []struct {
		name          string
		message       string
		expectedIndex int
	}{
		// Month + day (the critical fix for Andrew's "Feb 28" bug)
		{name: "Feb 28", message: "Feb 28", expectedIndex: 5},
		{name: "feb 28", message: "feb 28", expectedIndex: 5},
		{name: "February 28", message: "February 28", expectedIndex: 5},
		{name: "feb 28th", message: "feb 28th", expectedIndex: 5},
		{name: "Feb 24", message: "Feb 24", expectedIndex: 1},
		{name: "let's do feb 26", message: "let's do feb 26", expectedIndex: 3},
		// Numeric date
		{name: "2/28", message: "2/28", expectedIndex: 5},
		{name: "2/24", message: "2/24", expectedIndex: 1},
		// Ordinal day
		{name: "the 28th", message: "the 28th", expectedIndex: 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := DetectTimeSelection(tc.message, slots, noPrefs)
			if tc.expectedIndex == 0 {
				if result != nil {
					t.Errorf("expected nil, got slot %d", result.Index)
				}
			} else {
				if result == nil {
					t.Errorf("expected slot %d, got nil", tc.expectedIndex)
				} else if result.Index != tc.expectedIndex {
					t.Errorf("expected slot %d, got slot %d", tc.expectedIndex, result.Index)
				}
			}
		})
	}
}
