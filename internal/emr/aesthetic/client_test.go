package aesthetic

import (
	"context"
	"testing"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/emr"
)

type fakeUpstream struct {
	slots []emr.Slot
	err   error

	calls int
	last  emr.AvailabilityRequest
}

func (f *fakeUpstream) GetAvailability(ctx context.Context, req emr.AvailabilityRequest) ([]emr.Slot, error) {
	_ = ctx
	f.calls++
	f.last = req
	return f.slots, f.err
}

func TestClient_SyncAndGetAvailability(t *testing.T) {
	now := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	upstream := &fakeUpstream{
		slots: []emr.Slot{
			{
				ID:           "slot-1",
				ProviderID:   "p1",
				ProviderName: "Dr. A",
				StartTime:    now.Add(2 * time.Hour),
				EndTime:      now.Add(2*time.Hour + 30*time.Minute),
				Status:       "free",
				ServiceType:  "Consult",
			},
		},
	}

	client, err := New(Config{
		ClinicID: "clinic-1",
		Upstream: upstream,
		Now: func() time.Time {
			return now
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := client.SyncAvailability(context.Background(), SyncAvailabilityOptions{
		WindowDays:   1,
		DurationMins: 30,
	}); err != nil {
		t.Fatalf("SyncAvailability: %v", err)
	}
	if upstream.calls != 1 {
		t.Fatalf("expected 1 upstream call, got %d", upstream.calls)
	}
	if upstream.last.ClinicID != "clinic-1" {
		t.Fatalf("expected clinic id passed upstream, got %q", upstream.last.ClinicID)
	}
	if upstream.last.DurationMins != 30 {
		t.Fatalf("expected duration passed upstream, got %d", upstream.last.DurationMins)
	}
	if !upstream.last.StartDate.Equal(now) {
		t.Fatalf("unexpected upstream start: got %s want %s", upstream.last.StartDate.Format(time.RFC3339), now.Format(time.RFC3339))
	}
	if !upstream.last.EndDate.Equal(now.AddDate(0, 0, 1)) {
		t.Fatalf("unexpected upstream end: got %s want %s", upstream.last.EndDate.Format(time.RFC3339), now.AddDate(0, 0, 1).Format(time.RFC3339))
	}

	slots, err := client.GetAvailability(context.Background(), emr.AvailabilityRequest{
		ClinicID:     "clinic-1",
		StartDate:    now,
		EndDate:      now.AddDate(0, 0, 1),
		DurationMins: 30,
	})
	if err != nil {
		t.Fatalf("GetAvailability: %v", err)
	}
	if len(slots) != 1 {
		t.Fatalf("expected 1 slot, got %d", len(slots))
	}
	if slots[0].ID != "slot-1" {
		t.Fatalf("unexpected slot id: %q", slots[0].ID)
	}
}

func TestClient_CreateAppointment_RemovesSlotAndCancelRestores(t *testing.T) {
	now := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	client, err := New(Config{
		ClinicID: "clinic-1",
		Now: func() time.Time {
			return now
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	slot := emr.Slot{
		ID:           "slot-1",
		ProviderID:   "p1",
		ProviderName: "Dr. A",
		StartTime:    now.Add(2 * time.Hour),
		EndTime:      now.Add(2*time.Hour + 30*time.Minute),
		Status:       "free",
		ServiceType:  "Consult",
	}
	client.store.replaceSlots("clinic-1", now, []emr.Slot{slot})

	patient, err := client.CreatePatient(context.Background(), emr.Patient{
		FirstName: "Jane",
		LastName:  "Doe",
		Phone:     "+15551234567",
	})
	if err != nil {
		t.Fatalf("CreatePatient: %v", err)
	}

	appt, err := client.CreateAppointment(context.Background(), emr.AppointmentRequest{
		ClinicID:   "clinic-1",
		PatientID:  patient.ID,
		ProviderID: "p1",
		SlotID:     "slot-1",
		Status:     "booked",
		Notes:      "shadow booked",
	})
	if err != nil {
		t.Fatalf("CreateAppointment: %v", err)
	}
	if appt.ID == "" {
		t.Fatalf("expected appointment id to be set")
	}

	slots, err := client.GetAvailability(context.Background(), emr.AvailabilityRequest{
		ClinicID:  "clinic-1",
		StartDate: now,
		EndDate:   now.AddDate(0, 0, 1),
	})
	if err != nil {
		t.Fatalf("GetAvailability: %v", err)
	}
	if len(slots) != 0 {
		t.Fatalf("expected slot to be removed after booking, got %d", len(slots))
	}

	if err := client.CancelAppointment(context.Background(), appt.ID); err != nil {
		t.Fatalf("CancelAppointment: %v", err)
	}

	slots, err = client.GetAvailability(context.Background(), emr.AvailabilityRequest{
		ClinicID:  "clinic-1",
		StartDate: now,
		EndDate:   now.AddDate(0, 0, 1),
	})
	if err != nil {
		t.Fatalf("GetAvailability: %v", err)
	}
	if len(slots) != 1 {
		t.Fatalf("expected slot restored after cancel, got %d", len(slots))
	}
	if slots[0].ID != "slot-1" {
		t.Fatalf("unexpected restored slot id: %q", slots[0].ID)
	}
}

func TestClient_SyncDoesNotReintroduceBookedSlot(t *testing.T) {
	now := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	slot := emr.Slot{
		ID:        "slot-1",
		StartTime: now.Add(2 * time.Hour),
		EndTime:   now.Add(2*time.Hour + 30*time.Minute),
		Status:    "free",
	}
	upstream := &fakeUpstream{slots: []emr.Slot{slot}}

	client, err := New(Config{
		ClinicID: "clinic-1",
		Upstream: upstream,
		Now: func() time.Time {
			return now
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := client.SyncAvailability(context.Background(), SyncAvailabilityOptions{WindowDays: 1}); err != nil {
		t.Fatalf("SyncAvailability: %v", err)
	}

	patient, err := client.CreatePatient(context.Background(), emr.Patient{FirstName: "Jane", LastName: "Doe"})
	if err != nil {
		t.Fatalf("CreatePatient: %v", err)
	}

	appt, err := client.CreateAppointment(context.Background(), emr.AppointmentRequest{
		ClinicID:  "clinic-1",
		PatientID: patient.ID,
		SlotID:    "slot-1",
	})
	if err != nil {
		t.Fatalf("CreateAppointment: %v", err)
	}

	if err := client.SyncAvailability(context.Background(), SyncAvailabilityOptions{WindowDays: 1}); err != nil {
		t.Fatalf("SyncAvailability (second): %v", err)
	}

	slots, err := client.GetAvailability(context.Background(), emr.AvailabilityRequest{
		ClinicID:  "clinic-1",
		StartDate: now,
		EndDate:   now.AddDate(0, 0, 1),
	})
	if err != nil {
		t.Fatalf("GetAvailability: %v", err)
	}
	if len(slots) != 0 {
		t.Fatalf("expected no available slots due to local booking, got %d", len(slots))
	}

	if got, err := client.GetAppointment(context.Background(), appt.ID); err != nil || got == nil {
		t.Fatalf("expected appointment to exist, err=%v", err)
	}
}

func TestClient_SearchPatients(t *testing.T) {
	now := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	client, err := New(Config{
		ClinicID: "clinic-1",
		Now: func() time.Time {
			return now
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	patient, err := client.CreatePatient(context.Background(), emr.Patient{
		FirstName: "Jane",
		LastName:  "Doe",
		Email:     "JANE.DOE@example.com",
		Phone:     "+15551234567",
	})
	if err != nil {
		t.Fatalf("CreatePatient: %v", err)
	}

	if _, err := client.SearchPatients(context.Background(), emr.PatientSearchQuery{}); err == nil {
		t.Fatalf("expected empty search query to error")
	}

	found, err := client.SearchPatients(context.Background(), emr.PatientSearchQuery{Phone: "+15551234567"})
	if err != nil {
		t.Fatalf("SearchPatients by phone: %v", err)
	}
	if len(found) != 1 || found[0].ID != patient.ID {
		t.Fatalf("unexpected phone search result: %+v", found)
	}

	found, err = client.SearchPatients(context.Background(), emr.PatientSearchQuery{Email: "jane.doe@example.com"})
	if err != nil {
		t.Fatalf("SearchPatients by email: %v", err)
	}
	if len(found) != 1 || found[0].ID != patient.ID {
		t.Fatalf("unexpected email search result: %+v", found)
	}

	found, err = client.SearchPatients(context.Background(), emr.PatientSearchQuery{FirstName: "Jane", LastName: "Doe"})
	if err != nil {
		t.Fatalf("SearchPatients by name: %v", err)
	}
	if len(found) != 1 || found[0].ID != patient.ID {
		t.Fatalf("unexpected name search result: %+v", found)
	}
}
