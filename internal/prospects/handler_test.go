package prospects

import (
	"net/http"
	"testing"
)

func TestExtractProspectID(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/admin/prospects/forever-22", "forever-22"},
		{"/admin/prospects/forever-22/events", "forever-22"},
		{"/admin/prospects", ""},
	}
	for _, tt := range tests {
		r, _ := http.NewRequest("GET", tt.path, nil)
		got := extractProspectID(r)
		if got != tt.want {
			t.Errorf("extractProspectID(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
