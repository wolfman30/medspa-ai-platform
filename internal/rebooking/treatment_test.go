package rebooking

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookupDuration(t *testing.T) {
	tests := []struct {
		service  string
		wantOK   bool
		wantMin  int
		isSeries bool
	}{
		{"Botox", true, 10, false},
		{"botox", true, 10, false},
		{"BOTOX", true, 10, false},
		{"tox", true, 10, false},
		{"Dermal Filler", true, 26, false},
		{"lip filler", true, 26, false},
		{"Microneedling", true, 4, true},
		{"Chemical Peel", true, 4, true},
		{"Laser Hair Removal", true, 4, true},
		{"Weight Loss", true, 4, false},
		{"unknown treatment", false, 0, false},
		// Fuzzy matching
		{"Botox Treatment", true, 10, false},
		{"Full Face Botox", true, 10, false},
	}
	for _, tt := range tests {
		t.Run(tt.service, func(t *testing.T) {
			td, ok := LookupDuration(tt.service)
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantMin, td.MinWeeks)
				assert.Equal(t, tt.isSeries, td.IsSeries)
			}
		})
	}
}

func TestRebookAfter(t *testing.T) {
	booked := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)

	rebook, ok := RebookAfter("botox", booked)
	require.True(t, ok)
	expected := booked.Add(10 * 7 * 24 * time.Hour)
	assert.Equal(t, expected, rebook)

	_, ok = RebookAfter("unknown", booked)
	assert.False(t, ok)
}
