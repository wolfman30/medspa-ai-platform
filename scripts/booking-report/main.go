package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"
)

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

	// Default to latest snapshot date
	snapshotDate := time.Now().Format("2006-01-02")
	if len(os.Args) > 1 {
		snapshotDate = os.Args[1]
	}

	// Find the Monday of the week for the snapshot date
	t, _ := time.Parse("2006-01-02", snapshotDate)
	weekday := t.Weekday()
	if weekday == 0 {
		weekday = 7
	}
	monday := t.AddDate(0, 0, -int(weekday-1))
	sunday := monday.AddDate(0, 0, 6)

	rows, err := db.Query(`
		SELECT
			bs.org_id,
			bs.service_name,
			SUM(bs.total_slots) AS total_slots,
			SUM(bs.available_slots) AS available_slots
		FROM booking_snapshots bs
		WHERE bs.snapshot_date = $1
		  AND bs.target_date BETWEEN $2 AND $3
		GROUP BY bs.org_id, bs.service_name
		ORDER BY bs.org_id, total_slots DESC
	`, snapshotDate, monday.Format("2006-01-02"), sunday.Format("2006-01-02"))
	if err != nil {
		log.Fatalf("Query failed: %v", err)
	}
	defer rows.Close()

	// Map org IDs to names
	orgNames := map[string]string{
		"d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599": "Forever 22 Med Spa",
		"brilliant-aesthetics-org-id":          "Brilliant Aesthetics",
	}

	currentOrg := ""
	for rows.Next() {
		var orgID, serviceName string
		var totalSlots, availableSlots int

		if err := rows.Scan(&orgID, &serviceName, &totalSlots, &availableSlots); err != nil {
			log.Fatalf("Scan failed: %v", err)
		}

		if orgID != currentOrg {
			currentOrg = orgID
			name := orgNames[orgID]
			if name == "" {
				name = orgID
			}
			fmt.Printf("\n%s — Week of %s\n", name, monday.Format("Jan 2"))
		}

		bookedPct := 0
		if totalSlots > 0 {
			bookedPct = int(float64(totalSlots-availableSlots) / float64(totalSlots) * 100)
		}
		fmt.Printf("  %s: %d total slots, %d available (%d%% booked)\n", serviceName, totalSlots, availableSlots, bookedPct)
	}

	if currentOrg == "" {
		fmt.Printf("No snapshot data found for %s\n", snapshotDate)
	}
	fmt.Println()
}
