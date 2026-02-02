package conversation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
			result := DetectTimeSelection(tt.message, slots)

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
	result := DetectTimeSelection("1", []PresentedSlot{})
	assert.Nil(t, result)

	result = DetectTimeSelection("1", nil)
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
