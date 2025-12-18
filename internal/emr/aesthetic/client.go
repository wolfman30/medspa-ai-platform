package aesthetic

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/emr"
)

// AvailabilitySource provides upstream availability for syncing into the shadow schedule.
type AvailabilitySource interface {
	GetAvailability(ctx context.Context, req emr.AvailabilityRequest) ([]emr.Slot, error)
}

// Config configures the Aesthetic Record shadow scheduler client.
type Config struct {
	ClinicID string

	Upstream AvailabilitySource
	Now      func() time.Time
}

// Client is an emr.Client implementation backed by a local "shadow schedule".
// Availability is served from a periodically-synced cache rather than real-time booking APIs.
type Client struct {
	clinicID string
	upstream AvailabilitySource
	store    *memoryStore
	now      func() time.Time
}

// New creates a new Aesthetic Record shadow scheduling client.
func New(cfg Config) (*Client, error) {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	return &Client{
		clinicID: strings.TrimSpace(cfg.ClinicID),
		upstream: cfg.Upstream,
		store:    newMemoryStore(),
		now:      now,
	}, nil
}

type SyncAvailabilityOptions struct {
	ClinicID     string
	ProviderID   string
	ServiceType  string
	WindowDays   int
	DurationMins int
}

// SyncAvailability refreshes the shadow cache by fetching availability from the upstream source.
func (c *Client) SyncAvailability(ctx context.Context, opts SyncAvailabilityOptions) error {
	if c == nil || c.store == nil {
		return errors.New("aesthetic: client not initialized")
	}
	if c.upstream == nil {
		return errors.New("aesthetic: upstream not configured")
	}

	clinicID := strings.TrimSpace(opts.ClinicID)
	if clinicID == "" {
		clinicID = c.clinicID
	}
	if clinicID == "" {
		return errors.New("aesthetic: clinic id is required")
	}

	windowDays := opts.WindowDays
	if windowDays <= 0 {
		windowDays = 7
	}
	if windowDays > 60 {
		windowDays = 60
	}

	durationMins := opts.DurationMins
	if durationMins <= 0 {
		durationMins = 30
	}

	start := c.now().UTC()
	end := start.AddDate(0, 0, windowDays)
	req := emr.AvailabilityRequest{
		ClinicID:     clinicID,
		ProviderID:   strings.TrimSpace(opts.ProviderID),
		ServiceType:  strings.TrimSpace(opts.ServiceType),
		StartDate:    start,
		EndDate:      end,
		DurationMins: durationMins,
	}

	slots, err := c.upstream.GetAvailability(ctx, req)
	if err != nil {
		return err
	}

	c.store.replaceSlots(clinicID, start, slots)
	return nil
}

// GetAvailability returns available appointment slots from the shadow cache.
func (c *Client) GetAvailability(ctx context.Context, req emr.AvailabilityRequest) ([]emr.Slot, error) {
	_ = ctx

	if c == nil || c.store == nil {
		return nil, errors.New("aesthetic: client not initialized")
	}

	clinicID := strings.TrimSpace(req.ClinicID)
	if clinicID == "" {
		clinicID = c.clinicID
	}
	if clinicID == "" {
		return nil, errors.New("aesthetic: clinic id is required")
	}

	durationMins := req.DurationMins
	if durationMins <= 0 {
		durationMins = 30
	}

	start := req.StartDate
	end := req.EndDate

	slots := c.store.listSlots(clinicID, start, end)
	if len(slots) == 0 {
		return nil, nil
	}

	providerID := strings.TrimSpace(req.ProviderID)
	serviceType := strings.ToLower(strings.TrimSpace(req.ServiceType))
	filtered := make([]emr.Slot, 0, len(slots))
	for _, slot := range slots {
		if slot.Status != "" && strings.ToLower(slot.Status) != "free" {
			continue
		}
		if providerID != "" && slot.ProviderID != providerID {
			continue
		}
		if serviceType != "" && !strings.Contains(strings.ToLower(slot.ServiceType), serviceType) {
			continue
		}
		if durationMins > 0 {
			slotMins := int(slot.EndTime.Sub(slot.StartTime).Minutes())
			if slotMins < durationMins {
				continue
			}
		}
		filtered = append(filtered, slot)
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].StartTime.Before(filtered[j].StartTime)
	})

	return filtered, nil
}

// CreateAppointment reserves a slot in the shadow schedule.
func (c *Client) CreateAppointment(ctx context.Context, req emr.AppointmentRequest) (*emr.Appointment, error) {
	_ = ctx

	if c == nil || c.store == nil {
		return nil, errors.New("aesthetic: client not initialized")
	}

	clinicID := strings.TrimSpace(req.ClinicID)
	if clinicID == "" {
		clinicID = c.clinicID
	}
	if clinicID == "" {
		return nil, errors.New("aesthetic: clinic id is required")
	}

	return c.store.createAppointment(clinicID, c.now().UTC(), req)
}

func (c *Client) GetAppointment(ctx context.Context, appointmentID string) (*emr.Appointment, error) {
	_ = ctx

	if c == nil || c.store == nil {
		return nil, errors.New("aesthetic: client not initialized")
	}
	return c.store.getAppointment(strings.TrimSpace(appointmentID))
}

func (c *Client) CancelAppointment(ctx context.Context, appointmentID string) error {
	_ = ctx

	if c == nil || c.store == nil {
		return errors.New("aesthetic: client not initialized")
	}
	return c.store.cancelAppointment(c.now().UTC(), strings.TrimSpace(appointmentID))
}

func (c *Client) CreatePatient(ctx context.Context, patient emr.Patient) (*emr.Patient, error) {
	_ = ctx

	if c == nil || c.store == nil {
		return nil, errors.New("aesthetic: client not initialized")
	}
	return c.store.createPatient(patient, c.now().UTC())
}

func (c *Client) GetPatient(ctx context.Context, patientID string) (*emr.Patient, error) {
	_ = ctx

	if c == nil || c.store == nil {
		return nil, errors.New("aesthetic: client not initialized")
	}
	return c.store.getPatient(strings.TrimSpace(patientID))
}

func (c *Client) SearchPatients(ctx context.Context, query emr.PatientSearchQuery) ([]emr.Patient, error) {
	_ = ctx

	if c == nil || c.store == nil {
		return nil, errors.New("aesthetic: client not initialized")
	}

	if strings.TrimSpace(query.Phone) == "" &&
		strings.TrimSpace(query.Email) == "" &&
		strings.TrimSpace(query.FirstName) == "" &&
		strings.TrimSpace(query.LastName) == "" {
		return nil, errors.New("aesthetic: at least one search field is required")
	}

	return c.store.searchPatients(query)
}
