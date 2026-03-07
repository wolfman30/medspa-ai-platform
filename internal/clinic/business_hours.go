package clinic

import (
	"fmt"
	"strings"
	"time"
)

// GetHoursForDay returns the hours for a given weekday (0=Sunday, 6=Saturday).
func (b *BusinessHours) GetHoursForDay(weekday time.Weekday) *DayHours {
	switch weekday {
	case time.Sunday:
		return b.Sunday
	case time.Monday:
		return b.Monday
	case time.Tuesday:
		return b.Tuesday
	case time.Wednesday:
		return b.Wednesday
	case time.Thursday:
		return b.Thursday
	case time.Friday:
		return b.Friday
	case time.Saturday:
		return b.Saturday
	default:
		return nil
	}
}

// HasAnyHours returns true if at least one day has business hours configured.
func (b *BusinessHours) HasAnyHours() bool {
	return b.Sunday != nil || b.Monday != nil || b.Tuesday != nil ||
		b.Wednesday != nil || b.Thursday != nil || b.Friday != nil || b.Saturday != nil
}

// IsOpenAt checks if the clinic is open at the given time.
// If no business hours are configured, the clinic is treated as always open
// (e.g., "by appointment only" clinics with no set hours).
func (c *Config) IsOpenAt(t time.Time) bool {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		loc = time.UTC
	}
	localTime := t.In(loc)

	hours := c.BusinessHours.GetHoursForDay(localTime.Weekday())
	if hours == nil {
		if !c.BusinessHours.HasAnyHours() {
			// No hours configured at all (appointment-only clinic).
			// Use time-based heuristic: 7 AM - 9 PM = "open", otherwise "after hours".
			// This ensures after-hours greetings fire at night instead of claiming
			// "providers are with patients" at 11 PM.
			h := localTime.Hour()
			return h >= 7 && h < 21
		}
		// Hours configured for other days but not today — closed today.
		return false
	}

	openTime, err := time.Parse("15:04", hours.Open)
	if err != nil {
		return false
	}
	closeTime, err := time.Parse("15:04", hours.Close)
	if err != nil {
		return false
	}

	currentMinutes := localTime.Hour()*60 + localTime.Minute()
	openMinutes := openTime.Hour()*60 + openTime.Minute()
	closeMinutes := closeTime.Hour()*60 + closeTime.Minute()

	return currentMinutes >= openMinutes && currentMinutes < closeMinutes
}

// NextOpenTime returns when the clinic next opens.
// Returns the current time if already open.
func (c *Config) NextOpenTime(t time.Time) time.Time {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		loc = time.UTC
	}
	localTime := t.In(loc)

	// Check up to 7 days ahead
	for i := 0; i < 7; i++ {
		checkDate := localTime.AddDate(0, 0, i)
		hours := c.BusinessHours.GetHoursForDay(checkDate.Weekday())

		if hours == nil {
			continue // Closed this day
		}

		openTime, err := time.Parse("15:04", hours.Open)
		if err != nil {
			continue
		}

		openDateTime := time.Date(
			checkDate.Year(), checkDate.Month(), checkDate.Day(),
			openTime.Hour(), openTime.Minute(), 0, 0, loc,
		)

		// If it's today and we haven't passed opening time yet
		if i == 0 {
			closeTime, _ := time.Parse("15:04", hours.Close)
			closeDateTime := time.Date(
				checkDate.Year(), checkDate.Month(), checkDate.Day(),
				closeTime.Hour(), closeTime.Minute(), 0, 0, loc,
			)

			if localTime.Before(openDateTime) {
				return openDateTime
			}
			if localTime.Before(closeDateTime) {
				return localTime // Already open
			}
			// Past closing, check next day
			continue
		}

		return openDateTime
	}

	// Fallback: return tomorrow 9 AM
	return time.Date(localTime.Year(), localTime.Month(), localTime.Day()+1, 9, 0, 0, 0, loc)
}

// BusinessHoursContext generates a string describing current status for the LLM.
func (c *Config) BusinessHoursContext(t time.Time) string {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		loc = time.UTC
	}
	localTime := t.In(loc)

	isOpen := c.IsOpenAt(t)
	nextOpen := c.NextOpenTime(t)

	status := "CLOSED"
	if isOpen {
		status = "OPEN"
	}

	hours := c.BusinessHours.GetHoursForDay(localTime.Weekday())
	todayHours := "Closed today"
	if hours != nil {
		todayHours = fmt.Sprintf("%s - %s", hours.Open, hours.Close)
	}

	ctx := fmt.Sprintf(
		"Clinic: %s\n"+
			"Current time: %s (%s)\n"+
			"Status: %s\n"+
			"Today's hours: %s\n",
		c.Name,
		localTime.Format("Monday, January 2, 2006 3:04 PM"),
		c.Timezone,
		status,
		todayHours,
	)

	if !isOpen {
		ctx += fmt.Sprintf("Next open: %s\n", nextOpen.Format("Monday at 3:04 PM"))
		// Calculate callback expectation for after-hours messages
		callbackTime := c.ExpectedCallbackTime(t)
		ctx += fmt.Sprintf("CALLBACK INSTRUCTION: When the clinic is closed, tell patients our team will reach out around %s. NEVER say '24 hours' if we're closed for the weekend or holiday.\n", callbackTime)
	} else {
		ctx += "CALLBACK INSTRUCTION: We're currently open! Our team can reach out shortly.\n"
	}

	ctx += fmt.Sprintf("Callback SLA: %d business hours\n", c.CallbackSLAHours)

	return ctx
}

// ExpectedCallbackTime returns a human-friendly string for when the patient can expect a callback.
// It accounts for business hours and provides a realistic expectation.
func (c *Config) ExpectedCallbackTime(t time.Time) string {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		loc = time.UTC
	}
	localTime := t.In(loc)
	nextOpen := c.NextOpenTime(t)

	// If we're open now, callback within the hour
	if c.IsOpenAt(t) {
		return "shortly"
	}

	// Calculate days until next open
	daysUntil := int(nextOpen.Sub(localTime).Hours() / 24)
	nextOpenLocal := nextOpen.In(loc)

	// Same day (later today)
	if localTime.YearDay() == nextOpenLocal.YearDay() && localTime.Year() == nextOpenLocal.Year() {
		return fmt.Sprintf("this %s around %s", strings.ToLower(nextOpenLocal.Format("Monday")), nextOpenLocal.Format("3 PM"))
	}

	// Tomorrow
	tomorrow := localTime.AddDate(0, 0, 1)
	if tomorrow.YearDay() == nextOpenLocal.YearDay() && tomorrow.Year() == nextOpenLocal.Year() {
		return fmt.Sprintf("tomorrow (%s) around %s", nextOpenLocal.Format("Monday"), nextOpenLocal.Format("3 PM"))
	}

	// This week (2-6 days out)
	if daysUntil <= 6 {
		return fmt.Sprintf("on %s around %s", nextOpenLocal.Format("Monday"), nextOpenLocal.Format("3 PM"))
	}

	// Fallback for longer periods
	return fmt.Sprintf("on %s around %s", nextOpenLocal.Format("Monday, January 2"), nextOpenLocal.Format("3 PM"))
}

// AIPersonaContext generates a string describing the AI persona for the LLM.
// This is injected into the conversation context to customize the AI's voice.
func (c *Config) AIPersonaContext() string {
	if c == nil {
		return ""
	}

	persona := c.AIPersona
	var parts []string

	// If solo operator, add context about the provider
	if persona.IsSoloOperator && persona.ProviderName != "" {
		parts = append(parts, fmt.Sprintf(
			"CLINIC CONTEXT - SOLO PRACTITIONER:\n"+
				"This clinic is operated by %s as a solo practitioner. You are %s's AI assistant (digital assistant).\n"+
				"IMPORTANT: Always identify yourself as %s's AI assistant or virtual assistant - never pretend to BE %s.\n"+
				"- Example greeting: 'Hi! This is %s's AI assistant at %s. %s is currently with a patient...'\n"+
				"- The patient should know they're texting with an AI system, not directly with %s\n"+
				"- The provider handles ALL patient care personally - there is no front desk staff\n"+
				"- This is a boutique, personality-driven practice where clients come specifically for %s",
			persona.ProviderName, persona.ProviderName, persona.ProviderName, persona.ProviderName,
			persona.ProviderName, c.Name, persona.ProviderName, persona.ProviderName, persona.ProviderName,
		))
	} else if persona.ProviderName != "" {
		parts = append(parts, fmt.Sprintf("Primary provider: %s. You are the clinic's AI assistant.", persona.ProviderName))
	}

	// Custom greetings for initial contact (business hours vs after hours)
	if persona.CustomGreeting != "" || persona.AfterHoursGreeting != "" {
		var greetingParts []string
		if persona.CustomGreeting != "" {
			greetingParts = append(greetingParts, fmt.Sprintf(
				"DURING BUSINESS HOURS greeting: \"%s\"", persona.CustomGreeting))
		}
		if persona.AfterHoursGreeting != "" {
			greetingParts = append(greetingParts, fmt.Sprintf(
				"AFTER HOURS greeting (evenings/weekends/closed): \"%s\"", persona.AfterHoursGreeting))
		}
		if len(greetingParts) > 0 {
			parts = append(parts, "GREETING STYLE:\n"+strings.Join(greetingParts, "\n")+
				"\nIMPORTANT: Check the clinic status above to determine which greeting to use.")
		}
	}

	// Busy message for why the provider can't answer
	if persona.BusyMessage != "" {
		parts = append(parts, fmt.Sprintf("BUSY MESSAGE: If explaining why we couldn't answer a call: \"%s\"", persona.BusyMessage))
	}

	// Tone guidance
	if persona.Tone != "" {
		switch strings.ToLower(persona.Tone) {
		case "clinical":
			parts = append(parts, "TONE: Clinical and professional. Focus on medical accuracy and patient safety.")
		case "warm":
			parts = append(parts, "TONE: Warm and approachable. Make patients feel comfortable while maintaining professionalism.")
		case "professional":
			parts = append(parts, "TONE: Straightforward and professional. Efficient communication focused on booking.")
		}
	}

	// Special non-cosmetic medical services
	if len(persona.SpecialServices) > 0 {
		parts = append(parts, fmt.Sprintf(
			"MEDICAL SERVICES NOTE: This clinic also offers medical treatments beyond cosmetics: %s. "+
				"These are functional medical treatments - handle inquiries with appropriate medical sensitivity.",
			strings.Join(persona.SpecialServices, ", "),
		))
	}

	if len(parts) == 0 {
		return ""
	}

	return "\n\n" + strings.Join(parts, "\n\n")
}
