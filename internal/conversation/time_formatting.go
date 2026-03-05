package conversation

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// humanizeDays converts a day count to a human-readable duration string.
func humanizeDays(days int) string {
	switch {
	case days <= 7:
		return "week"
	case days <= 14:
		return "2 weeks"
	case days >= 84:
		return "3 months"
	case days >= 56:
		return "2 months"
	case days >= 28:
		return "month"
	default:
		return fmt.Sprintf("%d weeks", days/7)
	}
}

// spreadSlotsAcrossDays picks slots spread across multiple days.
// maxPerDay limits slots from any single day. total caps the result.
// Slots must be pre-sorted by time.
func spreadSlotsAcrossDays(slots []PresentedSlot, total, maxPerDay int) []PresentedSlot {
	if len(slots) <= total {
		return slots
	}

	// Group by date
	type dayGroup struct {
		date  string
		slots []PresentedSlot
	}
	var days []dayGroup
	dayMap := map[string]int{} // date -> index in days
	for _, s := range slots {
		d := s.DateTime.Format("2006-01-02")
		if idx, ok := dayMap[d]; ok {
			days[idx].slots = append(days[idx].slots, s)
		} else {
			dayMap[d] = len(days)
			days = append(days, dayGroup{date: d, slots: []PresentedSlot{s}})
		}
	}

	// Round-robin: pick up to maxPerDay from each day until we have enough
	var result []PresentedSlot
	for round := 0; round < maxPerDay && len(result) < total; round++ {
		for i := range days {
			if round < len(days[i].slots) && len(result) < total {
				result = append(result, days[i].slots[round])
			}
		}
	}

	// Sort result by time
	sort.Slice(result, func(i, j int) bool {
		return result[i].DateTime.Before(result[j].DateTime)
	})

	return result
}

// formatSlotForDisplay formats a time slot for SMS display
func formatSlotForDisplay(t time.Time) string {
	// Format: "Mon Feb 10 at 10:00 AM"
	return t.Format("Mon Jan 2 at 3:04 PM")
}

// FormatTimeSlotsForSMS formats slots as a numbered list for SMS
func FormatTimeSlotsForSMS(slots []PresentedSlot, service string, exactMatch bool) string {
	if len(slots) == 0 {
		return fmt.Sprintf("Hmm, I'm not finding any open times for %s in the next week 😕\n\nWant me to check different dates or times?", service)
	}

	var sb strings.Builder

	if exactMatch {
		sb.WriteString(fmt.Sprintf("Here's what's open for %s 👇\n\n", service))
	} else {
		sb.WriteString(fmt.Sprintf("Closest I could find for %s 👇\n\n", service))
	}

	for _, slot := range slots {
		sb.WriteString(fmt.Sprintf("  %d → %s\n", slot.Index, slot.TimeStr))
	}

	sb.WriteString("\nJust reply with the number that works best!")

	return sb.String()
}

// FormatSlotNoLongerAvailableMessage formats message when selected slot was taken
func FormatSlotNoLongerAvailableMessage(selectedTime time.Time, remainingSlots []PresentedSlot) string {
	timeStr := selectedTime.Format("3:04 PM")
	if len(remainingSlots) == 0 {
		return fmt.Sprintf("I'm sorry, but the %s slot was just booked. Would you like me to check for other available times?", timeStr)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("I'm sorry, but the %s slot was just booked. Here are the remaining times:\n\n", timeStr))

	for i, slot := range remainingSlots {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, slot.TimeStr))
	}

	sb.WriteString("\nReply with the number of your preferred time.")

	return sb.String()
}

// FormatTimeSelectionConfirmation formats the confirmation message after time selection
func FormatTimeSelectionConfirmation(selectedTime time.Time, service string, depositAmount int) string {
	timeStr := selectedTime.Format("Monday, January 2 at 3:04 PM")
	depositDollars := float64(depositAmount) / 100.0

	return fmt.Sprintf(
		"Perfect! I've reserved %s for your %s appointment.\n\nTo confirm your booking, please complete the $%.0f refundable deposit:",
		timeStr, service, depositDollars,
	)
}
