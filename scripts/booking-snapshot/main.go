package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"strconv"
	"time"

	_ "github.com/lib/pq"
	"github.com/wolfman30/medspa-ai-platform/internal/emr/moxie"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

type clinicConfig struct {
	OrgID    string
	MedspaID string
	Name     string
	Services []serviceConfig
}

type serviceConfig struct {
	Name       string
	MenuItemID string
}

var clinics = []clinicConfig{
	{
		OrgID:    "d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599",
		MedspaID: "57",
		Name:     "Forever 22 Med Spa",
		Services: []serviceConfig{
			{Name: "Botox", MenuItemID: "20424"},
			{Name: "Lip Filler", MenuItemID: "47693"},
			{Name: "Dermal Filler", MenuItemID: "47709"},
			{Name: "Weight Loss In Person", MenuItemID: "18434"},
		},
	},
	{
		OrgID:    "brilliant-aesthetics-org-id",
		MedspaID: "95",
		Name:     "Brilliant Aesthetics",
		Services: []serviceConfig{
			{Name: "Botox", MenuItemID: "659"},
		},
	},
}

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	log.Println("Connected to database")

	logger := logging.New("info")
	client := moxie.NewClient(logger)

	ctx := context.Background()
	now := time.Now()
	snapshotDate := now.Format("2006-01-02")
	startDate := now.Format("2006-01-02")
	endDate := now.AddDate(0, 0, 30).Format("2006-01-02")

	log.Printf("Snapshot date: %s, querying slots from %s to %s", snapshotDate, startDate, endDate)

	totalInserted := 0

	for _, clinic := range clinics {
		log.Printf("\n=== %s (medspa_id=%s) ===", clinic.Name, clinic.MedspaID)

		for _, svc := range clinic.Services {
			log.Printf("  Querying %s (menu_item=%s)...", svc.Name, svc.MenuItemID)

			result, err := client.GetAvailableSlots(ctx, clinic.MedspaID, startDate, endDate, svc.MenuItemID, true)
			if err != nil {
				log.Printf("  ERROR querying %s: %v", svc.Name, err)
				continue
			}

			menuItemID, _ := strconv.Atoi(svc.MenuItemID)

			for _, d := range result.Dates {
				totalSlots := len(d.Slots)
				availableSlots := totalSlots // all returned slots are available

				if totalSlots == 0 {
					continue
				}

				_, err := db.ExecContext(ctx, `
					INSERT INTO booking_snapshots (org_id, snapshot_date, service_name, service_id, target_date, total_slots, available_slots)
					VALUES ($1, $2, $3, $4, $5, $6, $7)
					ON CONFLICT (org_id, service_id, provider_id, target_date, snapshot_date)
					DO UPDATE SET total_slots = EXCLUDED.total_slots, available_slots = EXCLUDED.available_slots
				`, clinic.OrgID, snapshotDate, svc.Name, menuItemID, d.Date, totalSlots, availableSlots)
				if err != nil {
					log.Printf("  ERROR inserting snapshot for %s on %s: %v", svc.Name, d.Date, err)
					continue
				}
				totalInserted++
			}

			slotCount := 0
			for _, d := range result.Dates {
				slotCount += len(d.Slots)
			}
			log.Printf("  %s: %d dates with slots, %d total slots", svc.Name, len(result.Dates), slotCount)
		}
	}

	log.Printf("\nDone! Inserted/updated %d snapshot rows.", totalInserted)
}
