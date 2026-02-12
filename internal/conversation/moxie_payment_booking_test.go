package conversation

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	moxieclient "github.com/wolfman30/medspa-ai-platform/internal/moxie"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestCreateMoxieBookingAfterPayment_Success(t *testing.T) {
	orgID := uuid.New().String()
	scheduled := time.Date(2026, 3, 15, 14, 30, 0, 0, time.UTC)

	leadsRepo := leads.NewInMemoryRepository()
	lead, err := leadsRepo.Create(context.Background(), &leads.CreateLeadRequest{
		OrgID: orgID,
		Name:  "Jane Smith",
		Phone: "+15551234567",
		Email: "jane@example.com",
	})
	if err != nil {
		t.Fatalf("failed to create lead: %v", err)
	}

	// Set the selected appointment
	if err := leadsRepo.UpdateSelectedAppointment(context.Background(), lead.ID, leads.SelectedAppointment{
		DateTime: &scheduled,
		Service:  "Lip Filler",
	}); err != nil {
		t.Fatalf("failed to update selected appointment: %v", err)
	}

	// Create a dry-run moxie client
	mc := moxieclient.NewClient(logging.Default(), moxieclient.WithDryRun(true))

	// Build a minimal worker with moxie client and leads repo
	worker := &Worker{
		moxieClient: mc,
		leadsRepo:   leadsRepo,
		logger:      logging.Default(),
	}

	cfg := &clinic.Config{
		OrgID:           orgID,
		Name:            "Forever 22 MedSpa",
		Timezone:        "America/New_York",
		BookingPlatform: "moxie",
		PaymentProvider: "stripe",
		MoxieConfig: &clinic.MoxieConfig{
			MedspaID:   "1264",
			MedspaSlug: "forever-22",
			ServiceMenuItems: map[string]string{
				"lip filler": "20425",
				"tox":        "20424",
			},
			DefaultProviderID: "prov-1",
		},
	}

	evt := &events.PaymentSucceededV1{
		EventID:     "evt_stripe_test",
		OrgID:       orgID,
		LeadID:      lead.ID,
		Provider:    "stripe",
		ProviderRef: "pi_test123",
		AmountCents: 5000,
		OccurredAt:  time.Now().UTC(),
		LeadPhone:   "+15551234567",
		FromNumber:  "+19998887777",
	}

	booked, confirmMsg := worker.createMoxieBookingAfterPayment(context.Background(), evt, cfg)
	if !booked {
		t.Fatal("expected moxie booking to succeed")
	}
	if confirmMsg == "" {
		t.Fatal("expected non-empty confirmation message")
	}
	if !containsAll(confirmMsg, "Lip Filler", "Forever 22 MedSpa", "🎉") {
		t.Fatalf("confirmation message missing expected content: %s", confirmMsg)
	}

	// Verify lead was updated with booking session
	updatedLead, err := leadsRepo.GetByID(context.Background(), orgID, lead.ID)
	if err != nil {
		t.Fatalf("failed to get updated lead: %v", err)
	}
	if updatedLead.BookingPlatform != "moxie" {
		t.Fatalf("expected booking platform moxie, got %s", updatedLead.BookingPlatform)
	}
	if updatedLead.BookingOutcome != "success" {
		t.Fatalf("expected booking outcome success, got %s", updatedLead.BookingOutcome)
	}
}

func TestCreateMoxieBookingAfterPayment_NoSelectedAppointment_FallsBackToScheduledFor(t *testing.T) {
	orgID := uuid.New().String()
	scheduled := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)

	leadsRepo := leads.NewInMemoryRepository()
	lead, _ := leadsRepo.Create(context.Background(), &leads.CreateLeadRequest{
		OrgID: orgID,
		Name:  "John Doe",
		Phone: "+15559876543",
	})

	// Set service interest but NOT selected appointment datetime
	_ = leadsRepo.UpdateSchedulingPreferences(context.Background(), lead.ID, leads.SchedulingPreferences{
		ServiceInterest: "Tox",
	})

	mc := moxieclient.NewClient(logging.Default(), moxieclient.WithDryRun(true))

	worker := &Worker{
		moxieClient: mc,
		leadsRepo:   leadsRepo,
		logger:      logging.Default(),
	}

	cfg := &clinic.Config{
		OrgID:           orgID,
		Name:            "Test Clinic",
		Timezone:        "America/Chicago",
		BookingPlatform: "moxie",
		PaymentProvider: "stripe",
		MoxieConfig: &clinic.MoxieConfig{
			MedspaID:         "999",
			ServiceMenuItems: map[string]string{"tox": "111"},
		},
	}

	evt := &events.PaymentSucceededV1{
		EventID:      "evt_test_fallback",
		OrgID:        orgID,
		LeadID:       lead.ID,
		Provider:     "stripe",
		ProviderRef:  "pi_fallback",
		ScheduledFor: &scheduled,
	}

	booked, _ := worker.createMoxieBookingAfterPayment(context.Background(), evt, cfg)
	if !booked {
		t.Fatal("expected booking to succeed with fallback to ScheduledFor")
	}
}

func TestCreateMoxieBookingAfterPayment_NoService_Skips(t *testing.T) {
	orgID := uuid.New().String()
	scheduled := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)

	leadsRepo := leads.NewInMemoryRepository()
	lead, _ := leadsRepo.Create(context.Background(), &leads.CreateLeadRequest{
		OrgID: orgID,
		Name:  "No Service",
		Phone: "+15551111111",
	})
	// Set datetime but NO service
	_ = leadsRepo.UpdateSelectedAppointment(context.Background(), lead.ID, leads.SelectedAppointment{
		DateTime: &scheduled,
	})

	mc := moxieclient.NewClient(logging.Default(), moxieclient.WithDryRun(true))
	worker := &Worker{
		moxieClient: mc,
		leadsRepo:   leadsRepo,
		logger:      logging.Default(),
	}

	cfg := &clinic.Config{
		OrgID:           orgID,
		BookingPlatform: "moxie",
		PaymentProvider: "stripe",
		MoxieConfig:     &clinic.MoxieConfig{MedspaID: "999"},
	}

	evt := &events.PaymentSucceededV1{
		EventID:    "evt_no_svc",
		OrgID:      orgID,
		LeadID:     lead.ID,
		Provider:   "stripe",
		LeadPhone:  "+15551111111",
		FromNumber: "+19990001111",
	}

	booked, _ := worker.createMoxieBookingAfterPayment(context.Background(), evt, cfg)
	if booked {
		t.Fatal("expected booking to be skipped when no service is set")
	}
}

func TestCreateMoxieBookingAfterPayment_NoMoxieConfig_Skips(t *testing.T) {
	worker := &Worker{logger: logging.Default()}
	cfg := &clinic.Config{
		BookingPlatform: "moxie",
		PaymentProvider: "stripe",
		MoxieConfig:     nil,
	}
	evt := &events.PaymentSucceededV1{OrgID: "org-1", LeadID: "lead-1"}

	booked, _ := worker.createMoxieBookingAfterPayment(context.Background(), evt, cfg)
	if booked {
		t.Fatal("expected skip when no moxie config")
	}
}

func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}
