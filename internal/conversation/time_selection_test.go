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

func (m *dateAwareMock) GetBatchAvailability(_ context.Context, req browser.BatchAvailabilityRequest) (*browser.BatchAvailabilityResponse, error) {
	var results []browser.AvailabilityResponse
	for _, date := range req.Dates {
		m.callCount.Add(1)
		resp, ok := m.responses[date]
		if !ok {
			results = append(results, browser.AvailabilityResponse{Success: true, Date: date, Slots: nil})
		} else {
			r := *resp
			r.Date = date
			results = append(results, r)
		}
	}
	return &browser.BatchAvailabilityResponse{Success: true, Results: results}, nil
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

	result, err := FetchAvailableTimesWithFallback(context.Background(), adapter, "https://example.com/book", "Botox", prefs, nil)
	require.NoError(t, err)

	assert.True(t, result.ExactMatch)
	assert.Equal(t, maxCalendarDays, result.SearchedDays) // all qualifying dates searched at once
	assert.Len(t, result.Slots, 2)
	assert.Empty(t, result.Message)
}

func TestFetchAvailableTimesWithFallback_RelaxedDayOfWeek(t *testing.T) {
	// Exact prefs: only Sundays after 3pm — no slots in 90 days.
	// Phase 2 relaxed: drop day filter, keep time filter — finds slots on day 0.
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

	result, err := FetchAvailableTimesWithFallback(context.Background(), adapter, "https://example.com/book", "Botox", prefs, nil)
	require.NoError(t, err)

	// Should find slots via Phase 2 adjacent proposals (same time, different days)
	assert.False(t, result.ExactMatch)
	assert.Len(t, result.Slots, 1)
	assert.Empty(t, result.Message)
}

func TestFetchAvailableTimesWithFallback_ExtendedSearch(t *testing.T) {
	// Slot on day 14 — all dates searched in batches of 31
	now := time.Now()
	day14 := now.AddDate(0, 0, 14).Format("2006-01-02")

	mock := &dateAwareMock{
		responses: map[string]*browser.AvailabilityResponse{
			day14: makeSlotResponse("10:00 AM"),
		},
	}
	adapter := NewBrowserAdapter(mock, nil)
	prefs := TimePreferences{} // no day filter

	result, err := FetchAvailableTimesWithFallback(context.Background(), adapter, "https://example.com/book", "Botox", prefs, nil)
	require.NoError(t, err)

	assert.True(t, result.ExactMatch)
	assert.Equal(t, maxCalendarDays, result.SearchedDays)
	assert.Len(t, result.Slots, 1)
	assert.Empty(t, result.Message)
}

func TestFetchAvailableTimesWithFallback_NothingFound(t *testing.T) {
	// Empty mock — no slots at all. Only time prefs (no day filter), so no adjacent proposals.
	mock := &dateAwareMock{responses: map[string]*browser.AvailabilityResponse{}}
	adapter := NewBrowserAdapter(mock, nil)
	prefs := TimePreferences{AfterTime: "15:00"}

	result, err := FetchAvailableTimesWithFallback(context.Background(), adapter, "https://example.com/book", "Botox", prefs, nil)
	require.NoError(t, err)

	assert.Nil(t, result.Slots)
	assert.False(t, result.ExactMatch)
	assert.Equal(t, maxCalendarDays, result.SearchedDays) // searched all 90 days
	assert.Contains(t, result.Message, "Botox")
	assert.Contains(t, result.Message, "3 months")
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

// === Progressive Search + Adjacent Proposal Tests ===

func TestProgressiveBatch_FirstBatch(t *testing.T) {
	// Slot on day 3 → found in first batch of up to 31 dates
	now := time.Now()
	day3 := now.AddDate(0, 0, 3).Format("2006-01-02")

	mock := &dateAwareMock{
		responses: map[string]*browser.AvailabilityResponse{
			day3: makeSlotResponse("10:00 AM"),
		},
	}
	adapter := NewBrowserAdapter(mock, nil)

	result, err := FetchAvailableTimesWithFallback(context.Background(), adapter, "https://example.com/book", "Botox", TimePreferences{}, nil)
	require.NoError(t, err)

	assert.True(t, result.ExactMatch)
	assert.Equal(t, maxCalendarDays, result.SearchedDays)
	assert.Len(t, result.Slots, 1)
	assert.Empty(t, result.Message)
}

func TestProgressiveBatch_ThirdBatch(t *testing.T) {
	// Slot on day 35 → found in second batch (dates 31-61)
	now := time.Now()
	day35 := now.AddDate(0, 0, 35).Format("2006-01-02")

	mock := &dateAwareMock{
		responses: map[string]*browser.AvailabilityResponse{
			day35: makeSlotResponse("2:00 PM"),
		},
	}
	adapter := NewBrowserAdapter(mock, nil)

	result, err := FetchAvailableTimesWithFallback(context.Background(), adapter, "https://example.com/book", "Botox", TimePreferences{}, nil)
	require.NoError(t, err)

	assert.True(t, result.ExactMatch)
	assert.Equal(t, maxCalendarDays, result.SearchedDays)
	assert.Len(t, result.Slots, 1)
}

func TestProgressiveBatch_LastBatch(t *testing.T) {
	// Slot on day 85 → found in final batch [84,90)
	now := time.Now()
	day85 := now.AddDate(0, 0, 85).Format("2006-01-02")

	mock := &dateAwareMock{
		responses: map[string]*browser.AvailabilityResponse{
			day85: makeSlotResponse("11:00 AM"),
		},
	}
	adapter := NewBrowserAdapter(mock, nil)

	result, err := FetchAvailableTimesWithFallback(context.Background(), adapter, "https://example.com/book", "Botox", TimePreferences{}, nil)
	require.NoError(t, err)

	assert.True(t, result.ExactMatch)
	assert.Equal(t, maxCalendarDays, result.SearchedDays) // 90
	assert.Len(t, result.Slots, 1)
}

func TestNothingFound_90Days(t *testing.T) {
	// No slots anywhere in 90 days, only time prefs (no day filter → no adjacent proposals)
	mock := &dateAwareMock{responses: map[string]*browser.AvailabilityResponse{}}
	adapter := NewBrowserAdapter(mock, nil)

	result, err := FetchAvailableTimesWithFallback(context.Background(), adapter, "https://example.com/book", "Botox", TimePreferences{AfterTime: "20:00"}, nil)
	require.NoError(t, err)

	assert.Nil(t, result.Slots)
	assert.False(t, result.ExactMatch)
	assert.Equal(t, maxCalendarDays, result.SearchedDays)
	assert.Contains(t, result.Message, "3 months")
	assert.Contains(t, result.Message, "Botox")
}

func TestAdjacentProposal_BothAlternatives(t *testing.T) {
	// Patient wants Mondays after 3pm. Nothing matches in 90 days.
	// But Tuesday at 4pm exists (same time, different day)
	// And Monday at 10am exists (same day, different time)
	// → Message should ask patient to choose.
	now := time.Now()

	// Find a nearby Monday and Tuesday
	daysUntilMon := (int(time.Monday) - int(now.Weekday()) + 7) % 7
	if daysUntilMon == 0 {
		daysUntilMon = 7
	}
	monday := now.AddDate(0, 0, daysUntilMon)
	tuesday := monday.AddDate(0, 0, 1)

	monStr := monday.Format("2006-01-02")
	tueStr := tuesday.Format("2006-01-02")

	mock := &dateAwareMock{
		responses: map[string]*browser.AvailabilityResponse{
			monStr: makeSlotResponse("10:00 AM"),           // Monday morning (wrong time)
			tueStr: makeSlotResponse("4:00 PM", "5:00 PM"), // Tuesday afternoon (wrong day)
		},
	}
	adapter := NewBrowserAdapter(mock, nil)

	prefs := TimePreferences{DaysOfWeek: []int{1}, AfterTime: "15:00"} // Monday after 3pm

	result, err := FetchAvailableTimesWithFallback(context.Background(), adapter, "https://example.com/book", "Botox", prefs, nil)
	require.NoError(t, err)

	// Should propose adjacent alternatives
	assert.Nil(t, result.Slots, "should not have exact-match slots")
	assert.False(t, result.ExactMatch)
	assert.Contains(t, result.Message, "adjustment")
	assert.Contains(t, result.Message, "different days")
	assert.Contains(t, result.Message, "Different times")
}

func TestAdjacentProposal_OnlyDiffDays(t *testing.T) {
	// Patient wants Mondays after 3pm. Nothing matches exactly.
	// Tuesday at 4pm exists (same time, different day) — but no Monday morning slots.
	// → Should present the different-day slots directly.
	now := time.Now()

	// Find a nearby Tuesday
	daysUntilTue := (int(time.Tuesday) - int(now.Weekday()) + 7) % 7
	if daysUntilTue == 0 {
		daysUntilTue = 7
	}
	tuesday := now.AddDate(0, 0, daysUntilTue)
	tueStr := tuesday.Format("2006-01-02")

	mock := &dateAwareMock{
		responses: map[string]*browser.AvailabilityResponse{
			tueStr: makeSlotResponse("4:00 PM"),
		},
	}
	adapter := NewBrowserAdapter(mock, nil)

	prefs := TimePreferences{DaysOfWeek: []int{1}, AfterTime: "15:00"} // Monday after 3pm

	result, err := FetchAvailableTimesWithFallback(context.Background(), adapter, "https://example.com/book", "Botox", prefs, nil)
	require.NoError(t, err)

	// Should present the different-day slots directly
	assert.False(t, result.ExactMatch)
	assert.NotEmpty(t, result.Slots, "should present the alternative slots")
	assert.Len(t, result.Slots, 1)
	assert.Empty(t, result.Message, "no message when slots are presented directly")
}

func TestAdjacentProposal_NeitherAlternative(t *testing.T) {
	// Patient wants Mondays after 3pm. Nothing matches at all.
	// No same-time-different-day, no same-day-different-time.
	mock := &dateAwareMock{responses: map[string]*browser.AvailabilityResponse{}}
	adapter := NewBrowserAdapter(mock, nil)

	prefs := TimePreferences{DaysOfWeek: []int{1}, AfterTime: "15:00"}

	result, err := FetchAvailableTimesWithFallback(context.Background(), adapter, "https://example.com/book", "Botox", prefs, nil)
	require.NoError(t, err)

	assert.Nil(t, result.Slots)
	assert.Contains(t, result.Message, "3 months")
}

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

func TestProgressiveSearch_StopsEarly(t *testing.T) {
	// Slot on day 0 → mock should only be called for first batch (up to 31 dates)
	now := time.Now()
	day0 := now.Format("2006-01-02")

	mock := &dateAwareMock{
		responses: map[string]*browser.AvailabilityResponse{
			day0: makeSlotResponse("10:00 AM"),
		},
	}
	adapter := NewBrowserAdapter(mock, nil)

	result, err := FetchAvailableTimesWithFallback(context.Background(), adapter, "https://example.com/book", "Botox", TimePreferences{}, nil)
	require.NoError(t, err)

	assert.Len(t, result.Slots, 1)
	// With no day filter, 90 dates → 3 batches of 31/31/28.
	// Slot found in first batch → mock called for 31 dates in the first batch.
	assert.Equal(t, int32(31), mock.callCount.Load())
}

// === Progress Callback Tests ===

func TestProgressiveSearch_CallsProgressCallback(t *testing.T) {
	// No slots at all → should call progress callback between batches of 31 dates
	mock := &dateAwareMock{responses: map[string]*browser.AvailabilityResponse{}}
	adapter := NewBrowserAdapter(mock, nil)

	var progressMessages []string
	onProgress := func(_ context.Context, msg string) {
		progressMessages = append(progressMessages, msg)
	}

	result, err := FetchAvailableTimesWithFallback(
		context.Background(), adapter, "https://example.com/book", "Botox",
		TimePreferences{AfterTime: "20:00"}, onProgress,
	)
	require.NoError(t, err)
	assert.Nil(t, result.Slots)

	// First message is the initial "Checking available times..." notification.
	// Then 90 dates / 31 per batch = 3 batches; progress called before batches 2+3 → 2 more calls.
	assert.GreaterOrEqual(t, len(progressMessages), 2, "should send initial + progress messages between batches")
	assert.Contains(t, progressMessages[0], "Checking available times")
	// Subsequent messages mention no availability
	for _, msg := range progressMessages[1:] {
		assert.Contains(t, msg, "no availability")
	}
}

func TestProgressiveSearch_NoCallbackOnFirstBatch(t *testing.T) {
	// Slot on day 20 → all dates in first batch (31 dates), found immediately, no progress needed
	now := time.Now()
	day20 := now.AddDate(0, 0, 20).Format("2006-01-02")
	mock := &dateAwareMock{
		responses: map[string]*browser.AvailabilityResponse{
			day20: makeSlotResponse("3:00 PM"),
		},
	}
	adapter := NewBrowserAdapter(mock, nil)

	var progressMessages []string
	onProgress := func(_ context.Context, msg string) {
		progressMessages = append(progressMessages, msg)
	}

	result, err := FetchAvailableTimesWithFallback(
		context.Background(), adapter, "https://example.com/book", "Botox",
		TimePreferences{}, onProgress,
	)
	require.NoError(t, err)
	assert.Len(t, result.Slots, 1)

	// 1 progress message: the initial "Checking available times..." notification.
	// Phase 0 fails (mock doesn't implement CalendarSlotsProvider), falls to Phase 1.
	// Day 20 is in the first batch of 31 dates → found immediately, no between-batch messages.
	assert.Equal(t, 1, len(progressMessages), "should send initial progress message only")
	assert.Contains(t, progressMessages[0], "Checking available times")
}

func TestProgressiveSearch_NilCallbackSafe(t *testing.T) {
	// Ensure nil callback doesn't panic
	mock := &dateAwareMock{responses: map[string]*browser.AvailabilityResponse{}}
	adapter := NewBrowserAdapter(mock, nil)

	result, err := FetchAvailableTimesWithFallback(
		context.Background(), adapter, "https://example.com/book", "Botox",
		TimePreferences{}, nil,
	)
	require.NoError(t, err)
	assert.Nil(t, result.Slots)
	assert.Contains(t, result.Message, "3 months")
}

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
