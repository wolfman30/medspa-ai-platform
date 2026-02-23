package leads

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryRepository_GetOrCreateByPhone(t *testing.T) {
	repo := NewInMemoryRepository()
	ctx := context.Background()

	// Create first
	lead1, err := repo.GetOrCreateByPhone(ctx, "org-1", "+11234567890", "sms", "Jane")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lead1.Name != "Jane" {
		t.Errorf("Name = %q, want Jane", lead1.Name)
	}

	// Get existing
	lead2, err := repo.GetOrCreateByPhone(ctx, "org-1", "+11234567890", "sms", "Different Name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lead2.ID != lead1.ID {
		t.Error("expected same lead to be returned")
	}

	// Different org
	lead3, err := repo.GetOrCreateByPhone(ctx, "org-2", "+11234567890", "sms", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lead3.ID == lead1.ID {
		t.Error("expected different lead for different org")
	}
}

func TestInMemoryRepository_GetByBookingSessionID(t *testing.T) {
	repo := NewInMemoryRepository()
	ctx := context.Background()

	lead, _ := repo.Create(ctx, &CreateLeadRequest{
		OrgID: "org-1", Name: "Test", Phone: "+11234567890", Source: "web",
	})

	// Set booking session
	_ = repo.UpdateBookingSession(ctx, lead.ID, BookingSessionUpdate{SessionID: "sess-123"})

	// Find it
	found, err := repo.GetByBookingSessionID(ctx, "sess-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.ID != lead.ID {
		t.Error("expected same lead")
	}

	// Not found
	_, err = repo.GetByBookingSessionID(ctx, "nonexistent")
	if err != ErrLeadNotFound {
		t.Errorf("expected ErrLeadNotFound, got %v", err)
	}
}

func TestInMemoryRepository_UpdateSchedulingPreferences(t *testing.T) {
	repo := NewInMemoryRepository()
	ctx := context.Background()

	lead, _ := repo.Create(ctx, &CreateLeadRequest{
		OrgID: "org-1", Name: "Test", Phone: "+11234567890", Source: "web",
	})

	err := repo.UpdateSchedulingPreferences(ctx, lead.ID, SchedulingPreferences{
		Name:            "Updated Name",
		ServiceInterest: "Botox",
		PatientType:     "new",
		PreferredDays:   "weekdays",
		PreferredTimes:  "morning",
		PastServices:    "none",
		Notes:           "some notes",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, _ := repo.GetByID(ctx, "org-1", lead.ID)
	if updated.Name != "Updated Name" {
		t.Errorf("Name = %q, want Updated Name", updated.Name)
	}
	if updated.ServiceInterest != "Botox" {
		t.Errorf("ServiceInterest = %q", updated.ServiceInterest)
	}

	// Not found
	err = repo.UpdateSchedulingPreferences(ctx, "nonexistent", SchedulingPreferences{})
	if err != ErrLeadNotFound {
		t.Errorf("expected ErrLeadNotFound, got %v", err)
	}
}

func TestInMemoryRepository_UpdateSelectedAppointment(t *testing.T) {
	repo := NewInMemoryRepository()
	ctx := context.Background()

	lead, _ := repo.Create(ctx, &CreateLeadRequest{
		OrgID: "org-1", Name: "Test", Phone: "+11234567890", Source: "web",
	})

	dt := time.Now().Add(24 * time.Hour)
	err := repo.UpdateSelectedAppointment(ctx, lead.ID, SelectedAppointment{
		DateTime: &dt,
		Service:  "Botox",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, _ := repo.GetByID(ctx, "org-1", lead.ID)
	if updated.SelectedService != "Botox" {
		t.Errorf("SelectedService = %q", updated.SelectedService)
	}

	// Not found
	err = repo.UpdateSelectedAppointment(ctx, "nonexistent", SelectedAppointment{})
	if err != ErrLeadNotFound {
		t.Errorf("expected ErrLeadNotFound")
	}
}

func TestInMemoryRepository_UpdateEmail(t *testing.T) {
	repo := NewInMemoryRepository()
	ctx := context.Background()

	lead, _ := repo.Create(ctx, &CreateLeadRequest{
		OrgID: "org-1", Name: "Test", Phone: "+11234567890", Source: "web",
	})

	// Empty email is no-op
	err := repo.UpdateEmail(ctx, lead.ID, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Update email
	err = repo.UpdateEmail(ctx, lead.ID, "test@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, _ := repo.GetByID(ctx, "org-1", lead.ID)
	if updated.Email != "test@example.com" {
		t.Errorf("Email = %q", updated.Email)
	}

	// Not found
	err = repo.UpdateEmail(ctx, "nonexistent", "test@example.com")
	if err != ErrLeadNotFound {
		t.Errorf("expected ErrLeadNotFound")
	}
}

func TestInMemoryRepository_ClearSelectedAppointment(t *testing.T) {
	repo := NewInMemoryRepository()
	ctx := context.Background()

	lead, _ := repo.Create(ctx, &CreateLeadRequest{
		OrgID: "org-1", Name: "Test", Phone: "+11234567890", Source: "web",
	})

	dt := time.Now()
	_ = repo.UpdateSelectedAppointment(ctx, lead.ID, SelectedAppointment{DateTime: &dt, Service: "Botox"})

	err := repo.ClearSelectedAppointment(ctx, lead.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, _ := repo.GetByID(ctx, "org-1", lead.ID)
	if updated.SelectedDateTime != nil {
		t.Error("SelectedDateTime should be nil")
	}
	if updated.SelectedService != "" {
		t.Error("SelectedService should be empty")
	}

	// Not found
	err = repo.ClearSelectedAppointment(ctx, "nonexistent")
	if err != ErrLeadNotFound {
		t.Errorf("expected ErrLeadNotFound")
	}
}

func TestInMemoryRepository_UpdateBookingSession(t *testing.T) {
	repo := NewInMemoryRepository()
	ctx := context.Background()

	lead, _ := repo.Create(ctx, &CreateLeadRequest{
		OrgID: "org-1", Name: "Test", Phone: "+11234567890", Source: "web",
	})

	now := time.Now()
	err := repo.UpdateBookingSession(ctx, lead.ID, BookingSessionUpdate{
		SessionID:          "sess-1",
		Platform:           "moxie",
		Outcome:            "completed",
		ConfirmationNumber: "CONF-123",
		HandoffURL:         "https://example.com",
		HandoffSentAt:      &now,
		CompletedAt:        &now,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, _ := repo.GetByID(ctx, "org-1", lead.ID)
	if updated.BookingSessionID != "sess-1" {
		t.Errorf("BookingSessionID = %q", updated.BookingSessionID)
	}
	if updated.BookingPlatform != "moxie" {
		t.Errorf("BookingPlatform = %q", updated.BookingPlatform)
	}

	// Not found
	err = repo.UpdateBookingSession(ctx, "nonexistent", BookingSessionUpdate{})
	if err != ErrLeadNotFound {
		t.Errorf("expected ErrLeadNotFound")
	}
}
