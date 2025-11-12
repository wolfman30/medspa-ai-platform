package compliance

import "testing"

func TestDetectorStop(t *testing.T) {
	d := NewDetector()
	cases := []struct {
		body string
		want bool
	}{
		{"STOP", true},
		{" Stop ", true},
		{"unsubscribe me", true},
		{"Please stopall now", true},
		{"quit.", true},
		{"this is not stop", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := d.IsStop(tc.body); got != tc.want {
			t.Fatalf("IsStop(%q)=%v want %v", tc.body, got, tc.want)
		}
	}
}

func TestDetectorHelp(t *testing.T) {
	d := NewDetector()
	cases := []struct {
		body string
		want bool
	}{
		{"HELP", true},
		{" info please", true},
		{"need help?", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := d.IsHelp(tc.body); got != tc.want {
			t.Fatalf("IsHelp(%q)=%v want %v", tc.body, got, tc.want)
		}
	}
}

func TestDetectorNilSafety(t *testing.T) {
	var d *Detector
	if d.IsStop("STOP") {
		t.Fatalf("nil detector should not match stop")
	}
	if d.IsHelp("HELP") {
		t.Fatalf("nil detector should not match help")
	}
}
