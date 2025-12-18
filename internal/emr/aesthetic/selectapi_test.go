package aesthetic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/emr"
)

func TestSelectAPIClient_GetAvailability_BuildsRequestAndParsesSlot(t *testing.T) {
	fixture, err := os.ReadFile("testdata/select_slot_bundle.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var gotAuth string
	var gotPath string
	var gotQuery url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	client, err := NewSelectAPIClient(SelectAPIConfig{
		BaseURL:     srv.URL,
		BearerToken: "token-123",
		HTTPClient:  srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewSelectAPIClient: %v", err)
	}

	start := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 7)

	slots, err := client.GetAvailability(context.Background(), emr.AvailabilityRequest{
		ClinicID:     "1",
		ProviderID:   "9323",
		StartDate:    start,
		EndDate:      end,
		DurationMins: 15,
	})
	if err != nil {
		t.Fatalf("GetAvailability: %v", err)
	}

	if gotAuth != "Bearer token-123" {
		t.Fatalf("expected Authorization header, got %q", gotAuth)
	}
	if gotPath != "/slot" {
		t.Fatalf("expected path /slot, got %q", gotPath)
	}

	startParams := append([]string(nil), gotQuery["start"]...)
	sort.Strings(startParams)
	if len(startParams) != 2 {
		t.Fatalf("expected 2 start params, got %v", startParams)
	}
	if startParams[0] != "ge2025-01-01" || startParams[1] != "lt2025-01-08" {
		t.Fatalf("unexpected start params: %v", startParams)
	}

	actors := append([]string(nil), gotQuery["schedule.actor"]...)
	sort.Strings(actors)
	if len(actors) != 2 {
		t.Fatalf("expected 2 schedule.actor params, got %v", actors)
	}
	if actors[0] != "location/1" || actors[1] != "practitioner/9323" {
		t.Fatalf("unexpected schedule.actor params: %v", actors)
	}

	if gotQuery.Get("slot-length") != "15" {
		t.Fatalf("expected slot-length=15, got %q", gotQuery.Get("slot-length"))
	}

	if len(slots) != 1 {
		t.Fatalf("expected 1 slot, got %d", len(slots))
	}
	slot := slots[0]
	if slot.ID == "" || !strings.HasPrefix(slot.ID, "derived_") {
		t.Fatalf("expected derived slot id, got %q", slot.ID)
	}
	if slot.ProviderID != "9323" {
		t.Fatalf("expected provider id 9323, got %q", slot.ProviderID)
	}
	if slot.ProviderName != "Walters, Sherri" {
		t.Fatalf("expected provider name, got %q", slot.ProviderName)
	}
	if slot.ServiceType != "Skin Check" {
		t.Fatalf("expected service type, got %q", slot.ServiceType)
	}

	expectedStart := time.Date(2018, 2, 26, 19, 0, 0, 0, time.UTC)
	expectedEnd := time.Date(2018, 2, 26, 20, 0, 0, 0, time.UTC)
	if !slot.StartTime.Equal(expectedStart) {
		t.Fatalf("unexpected start time: got %s want %s", slot.StartTime.Format(time.RFC3339), expectedStart.Format(time.RFC3339))
	}
	if !slot.EndTime.Equal(expectedEnd) {
		t.Fatalf("unexpected end time: got %s want %s", slot.EndTime.Format(time.RFC3339), expectedEnd.Format(time.RFC3339))
	}
}
