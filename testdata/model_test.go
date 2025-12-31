package knowledge

import (
	"encoding/json"
	"os"
	"testing"
)

func TestGetProviderSchedule(t *testing.T) {
	// 1. Load the sample data from the testdata directory
	// Note: Path is relative to the package directory (pkg/knowledge)
	data, err := os.ReadFile("../../testdata/sample-clinic-knowledge.json")
	if err != nil {
		t.Fatalf("Failed to read sample data: %v", err)
	}

	// 2. Unmarshal into the ClinicKnowledge struct
	var ck ClinicKnowledge
	if err := json.Unmarshal(data, &ck); err != nil {
		t.Fatalf("Failed to unmarshal sample data: %v", err)
	}

	// 3. Test Case: Verify Dr. Chen's Monday hours
	t.Run("Dr Chen Monday Availability", func(t *testing.T) {
		// The function should handle case-insensitivity ("Monday" vs "monday")
		slot := ck.GetProviderSchedule("dr_chen", "Monday")

		if slot == nil {
			t.Fatal("Expected schedule for Dr. Chen on Monday, got nil")
		}

		expectedStart := "09:00"
		expectedEnd := "17:00"

		if slot.Start != expectedStart {
			t.Errorf("Expected start time %s, got %s", expectedStart, slot.Start)
		}
		if slot.End != expectedEnd {
			t.Errorf("Expected end time %s, got %s", expectedEnd, slot.End)
		}
	})

	// 4. Test Case: Verify Dr. Chen is NOT working on Tuesday
	t.Run("Dr Chen Tuesday Off", func(t *testing.T) {
		slot := ck.GetProviderSchedule("dr_chen", "Tuesday")
		if slot != nil {
			t.Errorf("Expected nil for Dr. Chen on Tuesday (she is off), got %v", slot)
		}
	})
}

func TestGetService(t *testing.T) {
	// 1. Load the sample data
	// Note: Path is relative to the package directory
	data, err := os.ReadFile("../../testdata/sample-clinic-knowledge.json")
	if err != nil {
		t.Fatalf("Failed to read sample data: %v", err)
	}

	// 2. Unmarshal into the ClinicKnowledge struct
	var ck ClinicKnowledge
	if err := json.Unmarshal(data, &ck); err != nil {
		t.Fatalf("Failed to unmarshal sample data: %v", err)
	}

	// 3. Test Case: Verify Botox Price
	t.Run("Botox Price Lookup", func(t *testing.T) {
		service := ck.GetService("botox")
		if service == nil {
			t.Fatal("Expected service 'botox' to be found, got nil")
		}

		if service.PriceUSD != 12.00 {
			t.Errorf("Expected price 12.00, got %f", service.PriceUSD)
		}
	})

	// 4. Test Case: Verify Non-Existent Service
	t.Run("Non-Existent Service", func(t *testing.T) {
		service := ck.GetService("non_existent_service")
		if service != nil {
			t.Errorf("Expected nil for non-existent service, got %v", service)
		}
	})
}
