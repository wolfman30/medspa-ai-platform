package conversation

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wolfman30/medspa-ai-platform/internal/browser"
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

// dateAwareMock returns different responses per date string.
type dateAwareMock struct {
	responses map[string]*browser.AvailabilityResponse
	callCount atomic.Int32
}

func (m *dateAwareMock) GetAvailability(_ context.Context, req browser.AvailabilityRequest) (*browser.AvailabilityResponse, error) {
	m.callCount.Add(1)
	resp, ok := m.responses[req.Date]
	if !ok {
		return &browser.AvailabilityResponse{Success: true, Slots: nil}, nil
	}
	return resp, nil
}

func (m *dateAwareMock) IsReady(_ context.Context) bool { return true }

// makeSlotResponse builds an AvailabilityResponse with the given times.
func makeSlotResponse(times ...string) *browser.AvailabilityResponse {
	slots := make([]browser.TimeSlot, len(times))
	for i, t := range times {
		slots[i] = browser.TimeSlot{Time: t, Available: true}
	}
	return &browser.AvailabilityResponse{Success: true, Slots: slots}
}

func TestFetchAvailableTimes_Parallel(t *testing.T) {
	// Use a date-aware mock that returns slots for two different dates.
	// Today's date varies, so we compute the qualifying dates dynamically.
	now := time.Now()
	day0 := now.Format("2006-01-02")
	day1 := now.AddDate(0, 0, 1).Format("2006-01-02")

	mock := &dateAwareMock{
		responses: map[string]*browser.AvailabilityResponse{
			day0: makeSlotResponse("10:00 AM", "11:00 AM"),
			day1: makeSlotResponse("2:00 PM"),
		},
	}
	adapter := NewBrowserAdapter(mock, nil)

	slots, err := FetchAvailableTimes(context.Background(), adapter, "https://example.com/book", "Botox", TimePreferences{})
	require.NoError(t, err)

	// Should get slots from both days (3 total)
	assert.Len(t, slots, 3)
	// Slots should be in chronological order
	assert.True(t, slots[0].DateTime.Before(slots[1].DateTime), "slot 0 should be before slot 1")
	assert.True(t, slots[1].DateTime.Before(slots[2].DateTime), "slot 1 should be before slot 2")
	// Indexes should be 1-based sequential
	assert.Equal(t, 1, slots[0].Index)
	assert.Equal(t, 2, slots[1].Index)
	assert.Equal(t, 3, slots[2].Index)

	// All 7 dates should have been fetched (parallel)
	assert.Equal(t, int32(daysToSearch), mock.callCount.Load())
}

func TestFetchAvailableTimes_DayOfWeekFilter(t *testing.T) {
	// Set up mock with slots on every day
	now := time.Now()
	responses := make(map[string]*browser.AvailabilityResponse)
	for i := 0; i < daysToSearch; i++ {
		d := now.AddDate(0, 0, i).Format("2006-01-02")
		responses[d] = makeSlotResponse("9:00 AM")
	}
	mock := &dateAwareMock{responses: responses}
	adapter := NewBrowserAdapter(mock, nil)

	// Only fetch Mondays (weekday 1)
	prefs := TimePreferences{DaysOfWeek: []int{1}} // Monday
	slots, err := FetchAvailableTimes(context.Background(), adapter, "https://example.com/book", "Botox", prefs)
	require.NoError(t, err)

	// Every returned slot should be on a Monday
	for _, s := range slots {
		assert.Equal(t, time.Monday, s.DateTime.Weekday(), "slot %s should be Monday", s.TimeStr)
	}
}

func TestFetchAvailableTimesWithFallback_ExactMatch(t *testing.T) {
	now := time.Now()
	day0 := now.Format("2006-01-02")
	mock := &dateAwareMock{
		responses: map[string]*browser.AvailabilityResponse{
			day0: makeSlotResponse("3:00 PM", "4:00 PM"),
		},
	}
	adapter := NewBrowserAdapter(mock, nil)
	prefs := TimePreferences{AfterTime: "15:00"} // after 3pm

	result, err := FetchAvailableTimesWithFallback(context.Background(), adapter, "https://example.com/book", "Botox", prefs)
	require.NoError(t, err)

	assert.True(t, result.ExactMatch)
	assert.Equal(t, daysToSearch, result.SearchedDays)
	assert.Len(t, result.Slots, 2)
	assert.Empty(t, result.Message)
}

func TestFetchAvailableTimesWithFallback_RelaxedDayOfWeek(t *testing.T) {
	// Exact prefs: only Sundays after 3pm — no slots.
	// Relaxed: drop day filter, keep time filter — finds slots.
	now := time.Now()

	// Put an afternoon slot on day 0 (which is "today", probably not Sunday)
	day0 := now.Format("2006-01-02")
	mock := &dateAwareMock{
		responses: map[string]*browser.AvailabilityResponse{
			day0: makeSlotResponse("4:00 PM"),
		},
	}
	adapter := NewBrowserAdapter(mock, nil)

	// Ask for Sundays only (weekday 0) after 3pm
	prefs := TimePreferences{DaysOfWeek: []int{0}, AfterTime: "15:00"}

	// Only works if today is not Sunday
	if now.Weekday() == time.Sunday {
		t.Skip("today is Sunday, test not applicable")
	}

	result, err := FetchAvailableTimesWithFallback(context.Background(), adapter, "https://example.com/book", "Botox", prefs)
	require.NoError(t, err)

	// Should find slots via relaxed day-of-week (step 2)
	assert.False(t, result.ExactMatch)
	assert.Len(t, result.Slots, 1)
	assert.Empty(t, result.Message)
}

func TestFetchAvailableTimesWithFallback_ExtendedSearch(t *testing.T) {
	// No slots in days 0-6, but slots on day 14
	now := time.Now()
	day14 := now.AddDate(0, 0, 14).Format("2006-01-02")

	mock := &dateAwareMock{
		responses: map[string]*browser.AvailabilityResponse{
			day14: makeSlotResponse("10:00 AM"),
		},
	}
	adapter := NewBrowserAdapter(mock, nil)
	prefs := TimePreferences{} // no day filter, so step 2 is skipped

	result, err := FetchAvailableTimesWithFallback(context.Background(), adapter, "https://example.com/book", "Botox", prefs)
	require.NoError(t, err)

	// Should find via extended search (step 3)
	assert.True(t, result.ExactMatch)
	assert.Equal(t, extendedDaysToSearch, result.SearchedDays)
	assert.Len(t, result.Slots, 1)
	assert.Empty(t, result.Message)
}

func TestFetchAvailableTimesWithFallback_NothingFound(t *testing.T) {
	// Empty mock — no slots at all
	mock := &dateAwareMock{responses: map[string]*browser.AvailabilityResponse{}}
	adapter := NewBrowserAdapter(mock, nil)
	prefs := TimePreferences{AfterTime: "15:00"}

	result, err := FetchAvailableTimesWithFallback(context.Background(), adapter, "https://example.com/book", "Botox", prefs)
	require.NoError(t, err)

	assert.Nil(t, result.Slots)
	assert.False(t, result.ExactMatch)
	assert.Equal(t, extendedDaysToSearch, result.SearchedDays)
	assert.Contains(t, result.Message, "Botox")
	assert.Contains(t, result.Message, "4 weeks")
}

func TestFetchAvailableTimes_NilAdapter(t *testing.T) {
	_, err := FetchAvailableTimes(context.Background(), nil, "https://example.com/book", "Botox", TimePreferences{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestFetchAvailableTimes_EmptyBookingURL(t *testing.T) {
	mock := &dateAwareMock{responses: map[string]*browser.AvailabilityResponse{}}
	adapter := NewBrowserAdapter(mock, nil)
	_, err := FetchAvailableTimes(context.Background(), adapter, "", "Botox", TimePreferences{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "booking URL")
}
