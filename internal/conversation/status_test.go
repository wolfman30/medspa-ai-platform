package conversation

import "testing"

func TestConversationStatusConstants(t *testing.T) {
	// Verify all expected status values exist and are distinct
	statuses := []string{
		StatusActive,
		StatusEnded,
		StatusDepositPaid,
		StatusAwaitingTimeSelection,
		StatusBooked,
	}

	seen := make(map[string]bool)
	for _, s := range statuses {
		if s == "" {
			t.Error("status constant must not be empty")
		}
		if seen[s] {
			t.Errorf("duplicate status constant: %s", s)
		}
		seen[s] = true
	}

	// Verify specific values that are stored in the database
	if StatusBooked != "booked" {
		t.Errorf("StatusBooked = %q, want %q", StatusBooked, "booked")
	}
	if StatusDepositPaid != "deposit_paid" {
		t.Errorf("StatusDepositPaid = %q, want %q", StatusDepositPaid, "deposit_paid")
	}
	if StatusAwaitingTimeSelection != "awaiting_time_selection" {
		t.Errorf("StatusAwaitingTimeSelection = %q, want %q", StatusAwaitingTimeSelection, "awaiting_time_selection")
	}
}
