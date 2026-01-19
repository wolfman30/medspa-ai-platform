package handlers

import (
	"reflect"
	"testing"
)

func TestPhoneDigitsCandidates(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "ten digits adds leading one",
			input: "(937) 896-2713",
			want:  []string{"9378962713", "19378962713"},
		},
		{
			name:  "eleven digits adds local variant",
			input: "+1 937 896 2713",
			want:  []string{"19378962713", "9378962713"},
		},
		{
			name:  "non digits returns nil",
			input: "abc",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := phoneDigitsCandidates(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestAppendPhoneDigitsFilter(t *testing.T) {
	args := []any{"org-1"}
	argNum := 2
	filter := appendPhoneDigitsFilter("regexp_replace(phone, '\\\\D', '', 'g')", []string{"15551234567", "5551234567"}, &args, &argNum)

	expectedFilter := " AND regexp_replace(phone, '\\\\D', '', 'g') IN ($2,$3)"
	if filter != expectedFilter {
		t.Fatalf("expected filter %q, got %q", expectedFilter, filter)
	}
	if argNum != 4 {
		t.Fatalf("expected argNum 4, got %d", argNum)
	}
	expectedArgs := []any{"org-1", "15551234567", "5551234567"}
	if !reflect.DeepEqual(args, expectedArgs) {
		t.Fatalf("expected args %v, got %v", expectedArgs, args)
	}
}
