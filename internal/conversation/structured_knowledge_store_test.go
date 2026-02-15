package conversation

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestStructuredKnowledgeStoreRoundTrip(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := NewStructuredKnowledgeStore(client)
	ctx := context.Background()

	// Get non-existent
	sk, err := store.GetStructured(ctx, "org-1")
	if err != nil {
		t.Fatal(err)
	}
	if sk != nil {
		t.Fatal("expected nil for non-existent")
	}

	// Set and get
	now := time.Now().UTC().Truncate(time.Second)
	input := &StructuredKnowledge{
		OrgID:   "org-1",
		Version: 1,
		Sections: KnowledgeSections{
			Services: ServiceSection{
				Items: []ServiceItem{
					{ID: "svc-1", Name: "Tox", BookingID: "18430", Order: 1},
				},
			},
			Providers: ProviderSection{
				Items: []ProviderItem{
					{ID: "33950", Name: "Brandi", Order: 1},
				},
			},
			Policies: PolicySection{
				Cancellation: "24h notice",
				Deposit:      "$50",
			},
		},
		UpdatedAt: now,
	}

	if err := store.SetStructured(ctx, "org-1", input); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetStructured(ctx, "org-1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if got.OrgID != "org-1" {
		t.Errorf("OrgID = %q", got.OrgID)
	}
	if got.Version != 1 {
		t.Errorf("Version = %d", got.Version)
	}
	if len(got.Sections.Services.Items) != 1 {
		t.Errorf("services count = %d", len(got.Sections.Services.Items))
	}
	if got.Sections.Services.Items[0].Name != "Tox" {
		t.Errorf("service name = %q", got.Sections.Services.Items[0].Name)
	}
	if got.Sections.Policies.Cancellation != "24h notice" {
		t.Errorf("cancellation = %q", got.Sections.Policies.Cancellation)
	}
}
