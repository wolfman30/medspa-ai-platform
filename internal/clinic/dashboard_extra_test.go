package clinic

import "testing"

func TestFormatSeconds(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{0, "0s"},
		{-1, "0s"},
		{0.05, "0.05s"},
		{0.5, "0.50s"},
		{1.5, "1.5s"},
		{9.9, "9.9s"},
		{10, "10s"},
		{30.7, "31s"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := formatSeconds(tt.input); got != tt.want {
				t.Errorf("formatSeconds(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
