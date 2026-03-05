package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	moxieclient "github.com/wolfman30/medspa-ai-platform/internal/emr/moxie"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func main() {
	logger := logging.New("info")
	client := moxieclient.NewClient(logger)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	medspaID := "1349"
	serviceMenuItemID := "38140" // $9 Tox Offer
	now := time.Now()
	startDate := now.Format("2006-01-02")
	endDate := now.AddDate(0, 1, 0).Format("2006-01-02")

	fmt.Printf("Checking %s to %s for service %s\n\n", startDate, endDate, serviceMenuItemID)

	// Test with noPreference=true
	fmt.Println("=== noPreference=true ===")
	r, err := client.GetAvailableSlots(ctx, medspaID, startDate, endDate, serviceMenuItemID, true)
	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
		os.Exit(1)
	}
	total := 0
	for _, d := range r.Dates {
		total += len(d.Slots)
	}
	fmt.Printf("Total slots: %d\n", total)

	// Test per provider
	providers := map[string]string{
		"34577": "Angela Solenthaler",
		"34572": "Brady Steineck",
		"34579": "Brandy Roberts",
		"34575": "McKenna Zehnder",
	}

	for pid, name := range providers {
		fmt.Printf("\n=== Provider %s (%s) ===\n", pid, name)
		r2, err := client.GetAvailableSlots(ctx, medspaID, startDate, endDate, serviceMenuItemID, false, pid)
		if err != nil {
			fmt.Printf("ERROR: %v\n", err)
			continue
		}
		count := 0
		for _, d := range r2.Dates {
			count += len(d.Slots)
			if len(d.Slots) > 0 && count <= 5 {
				b, _ := json.Marshal(d)
				fmt.Println(string(b))
			}
		}
		fmt.Printf("Total slots (%s): %d\n", name, count)
	}
}
