package payments

import (
	"testing"

	"github.com/wolfman30/medspa-ai-platform/internal/leads"
)

func TestAbs64(t *testing.T) {
	tests := []struct {
		input int64
		want  int64
	}{
		{0, 0},
		{5, 5},
		{-5, 5},
		{-1, 1},
	}
	for _, tt := range tests {
		if got := abs64(tt.input); got != tt.want {
			t.Errorf("abs64(%d) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestLeadService(t *testing.T) {
	tests := []struct {
		name string
		lead *leads.Lead
		want string
	}{
		{"nil lead", nil, ""},
		{"selected service", &leads.Lead{SelectedService: "Botox"}, "Botox"},
		{"service interest fallback", &leads.Lead{ServiceInterest: "Fillers"}, "Fillers"},
		{"selected over interest", &leads.Lead{SelectedService: "Botox", ServiceInterest: "Fillers"}, "Botox"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := leadService(tt.lead); got != tt.want {
				t.Errorf("leadService() = %q, want %q", got, tt.want)
			}
		})
	}
}

// DefaultVelocityConfig already tested in velocity_test.go
