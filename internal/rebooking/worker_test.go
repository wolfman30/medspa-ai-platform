package rebooking

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsOptOut(t *testing.T) {
	assert.True(t, isOptOut("stop"))
	assert.True(t, isOptOut("no thanks"))
	assert.True(t, isOptOut("no"))
	assert.True(t, isOptOut("not interested"))
	assert.False(t, isOptOut("yes"))
	assert.False(t, isOptOut("hello"))
}

func TestIsRebookConfirm(t *testing.T) {
	assert.True(t, isRebookConfirm("yes"))
	assert.True(t, isRebookConfirm("yeah"))
	assert.True(t, isRebookConfirm("ok"))
	assert.True(t, isRebookConfirm("schedule"))
	assert.False(t, isRebookConfirm("stop"))
	assert.False(t, isRebookConfirm("hello"))
}

func TestHumanDuration(t *testing.T) {
	tests := []struct {
		weeks int
		want  string
	}{
		{4, "4 weeks"},
		{10, "10 weeks"},
		{12, "12 weeks"},
		{14, "3 months"},
		{26, "6 months"},
		{52, "13 months"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			from := testTime
			to := from.AddDate(0, 0, tt.weeks*7)
			got := humanDuration(from, to)
			assert.Equal(t, tt.want, got)
		})
	}
}
