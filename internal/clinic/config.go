// Package clinic provides clinic-specific configuration and business logic.
package clinic

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// DayHours represents the opening hours for a single day.
// Nil means the clinic is closed that day.
type DayHours struct {
	Open  string `json:"open"`  // "09:00" in 24-hour format
	Close string `json:"close"` // "18:00" in 24-hour format
}

// BusinessHours maps day names to their hours.
type BusinessHours struct {
	Monday    *DayHours `json:"monday,omitempty"`
	Tuesday   *DayHours `json:"tuesday,omitempty"`
	Wednesday *DayHours `json:"wednesday,omitempty"`
	Thursday  *DayHours `json:"thursday,omitempty"`
	Friday    *DayHours `json:"friday,omitempty"`
	Saturday  *DayHours `json:"saturday,omitempty"`
	Sunday    *DayHours `json:"sunday,omitempty"`
}

// NotificationPrefs holds notification preferences for a clinic.
type NotificationPrefs struct {
	// Email notifications
	EmailEnabled    bool     `json:"email_enabled"`
	EmailRecipients []string `json:"email_recipients,omitempty"` // e.g., ["owner@clinic.com"]

	// SMS notifications to operator
	SMSEnabled   bool   `json:"sms_enabled"`
	SMSRecipient string `json:"sms_recipient,omitempty"` // Operator's cell phone

	// What to notify about
	NotifyOnPayment bool `json:"notify_on_payment"`  // When deposit is paid
	NotifyOnNewLead bool `json:"notify_on_new_lead"` // When new lead comes in
}

// Config holds clinic-specific configuration.
type Config struct {
	OrgID              string            `json:"org_id"`
	Name               string            `json:"name"`
	Timezone           string            `json:"timezone"` // e.g., "America/New_York"
	BusinessHours      BusinessHours     `json:"business_hours"`
	CallbackSLAHours   int               `json:"callback_sla_hours"`   // e.g., 12
	DepositAmountCents int               `json:"deposit_amount_cents"` // e.g., 5000
	Services           []string          `json:"services,omitempty"`   // e.g., ["Botox", "Fillers"]
	Notifications      NotificationPrefs `json:"notifications"`
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig(orgID string) *Config {
	return &Config{
		OrgID:    orgID,
		Name:     "MedSpa",
		Timezone: "America/New_York",
		BusinessHours: BusinessHours{
			Monday:    &DayHours{Open: "09:00", Close: "18:00"},
			Tuesday:   &DayHours{Open: "09:00", Close: "18:00"},
			Wednesday: &DayHours{Open: "09:00", Close: "18:00"},
			Thursday:  &DayHours{Open: "09:00", Close: "18:00"},
			Friday:    &DayHours{Open: "09:00", Close: "17:00"},
			Saturday:  nil, // Closed
			Sunday:    nil, // Closed
		},
		CallbackSLAHours:   12,
		DepositAmountCents: 5000,
		Services:           []string{"Botox", "Fillers", "Laser Treatments"},
		Notifications: NotificationPrefs{
			EmailEnabled:    false, // Disabled by default until configured
			SMSEnabled:      false,
			NotifyOnPayment: true, // When enabled, notify on payment by default
			NotifyOnNewLead: false,
		},
	}
}

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

// IsOpenAt checks if the clinic is open at the given time.
func (c *Config) IsOpenAt(t time.Time) bool {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		loc = time.UTC
	}
	localTime := t.In(loc)

	hours := c.BusinessHours.GetHoursForDay(localTime.Weekday())
	if hours == nil {
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
	}

	ctx += fmt.Sprintf("Callback SLA: %d business hours\n", c.CallbackSLAHours)

	return ctx
}

// Store provides persistence for clinic configurations.
type Store struct {
	redis *redis.Client
}

// NewStore creates a new clinic config store.
func NewStore(redisClient *redis.Client) *Store {
	return &Store{redis: redisClient}
}

func (s *Store) key(orgID string) string {
	return fmt.Sprintf("clinic:config:%s", orgID)
}

// Get retrieves clinic config, returning default if not found.
func (s *Store) Get(ctx context.Context, orgID string) (*Config, error) {
	data, err := s.redis.Get(ctx, s.key(orgID)).Bytes()
	if err == redis.Nil {
		return DefaultConfig(orgID), nil
	}
	if err != nil {
		return nil, fmt.Errorf("clinic: get config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("clinic: unmarshal config: %w", err)
	}

	return &cfg, nil
}

// Set saves clinic config.
func (s *Store) Set(ctx context.Context, cfg *Config) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("clinic: marshal config: %w", err)
	}

	if err := s.redis.Set(ctx, s.key(cfg.OrgID), data, 0).Err(); err != nil {
		return fmt.Errorf("clinic: set config: %w", err)
	}

	return nil
}
