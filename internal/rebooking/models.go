package rebooking

import (
	"time"

	"github.com/google/uuid"
)

// ReminderStatus tracks the lifecycle of a rebooking reminder.
type ReminderStatus string

const (
	StatusPending   ReminderStatus = "pending"
	StatusSent      ReminderStatus = "sent"
	StatusBooked    ReminderStatus = "booked"
	StatusDismissed ReminderStatus = "dismissed"
)

// Channel specifies how the reminder is delivered.
type Channel string

const (
	ChannelSMS       Channel = "sms"
	ChannelInstagram Channel = "instagram"
)

// Reminder represents a scheduled rebooking outreach.
type Reminder struct {
	ID          uuid.UUID      `json:"id"`
	OrgID       string         `json:"org_id"`
	PatientID   uuid.UUID      `json:"patient_id"`
	Phone       string         `json:"phone"`
	PatientName string         `json:"patient_name"`
	Service     string         `json:"service"`
	Provider    string         `json:"provider"`
	BookedAt    time.Time      `json:"booked_at"`
	RebookAfter time.Time      `json:"rebook_after"`
	Status      ReminderStatus `json:"status"`
	Channel     Channel        `json:"channel"`
	SentAt      *time.Time     `json:"sent_at,omitempty"`
	DismissedAt *time.Time     `json:"dismissed_at,omitempty"`
	RebookedAt  *time.Time     `json:"rebooked_at,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// DashboardStats holds aggregated rebooking metrics for the admin dashboard.
type DashboardStats struct {
	UpcomingCount  int64   `json:"upcoming_count"`
	SentCount      int64   `json:"sent_count"`
	RebookedCount  int64   `json:"rebooked_count"`
	DismissedCount int64   `json:"dismissed_count"`
	ConversionPct  float64 `json:"conversion_pct"`
}
