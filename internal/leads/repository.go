package leads

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SchedulingPreferences captures the customer's availability preferences from conversation
type SchedulingPreferences struct {
	Name            string // Patient's name (extracted from conversation)
	ServiceInterest string // e.g., "Botox", "Filler", "Consultation"
	PatientType     string // "new" or "existing"
	PreferredDays   string // e.g., "weekdays", "weekends", "any"
	PreferredTimes  string // e.g., "morning", "afternoon", "evening"
	Notes           string // free-form notes from conversation
}

// ListLeadsFilter defines filtering options for listing leads
type ListLeadsFilter struct {
	DepositStatus string // "pending", "paid", "failed", or "" for all
	Limit         int    // max results, default 50
	Offset        int    // pagination offset
}

// Repository defines the interface for lead storage
type Repository interface {
	Create(ctx context.Context, req *CreateLeadRequest) (*Lead, error)
	GetByID(ctx context.Context, orgID string, id string) (*Lead, error)
	GetOrCreateByPhone(ctx context.Context, orgID string, phone string, source string, defaultName string) (*Lead, error)
	UpdateSchedulingPreferences(ctx context.Context, leadID string, prefs SchedulingPreferences) error
	UpdateDepositStatus(ctx context.Context, leadID string, status string, priority string) error
	ListByOrg(ctx context.Context, orgID string, filter ListLeadsFilter) ([]*Lead, error)
}

// InMemoryRepository is a stub implementation of Repository using in-memory storage
type InMemoryRepository struct {
	mu    sync.RWMutex
	leads map[string]*Lead
}

// NewInMemoryRepository creates a new in-memory repository
func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{
		leads: make(map[string]*Lead),
	}
}

// Create creates a new lead in memory
func (r *InMemoryRepository) Create(ctx context.Context, req *CreateLeadRequest) (*Lead, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	lead := &Lead{
		ID:        uuid.New().String(),
		OrgID:     req.OrgID,
		Name:      req.Name,
		Email:     req.Email,
		Phone:     req.Phone,
		Message:   req.Message,
		Source:    req.Source,
		CreatedAt: time.Now().UTC(),
	}

	r.mu.Lock()
	r.leads[lead.ID] = lead
	r.mu.Unlock()

	return lead, nil
}

// GetByID retrieves a lead by ID
func (r *InMemoryRepository) GetByID(ctx context.Context, orgID string, id string) (*Lead, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lead, ok := r.leads[id]
	if !ok || lead.OrgID != orgID {
		return nil, ErrLeadNotFound
	}

	return lead, nil
}

// GetOrCreateByPhone retrieves the most recent lead for an org/phone or creates one.
func (r *InMemoryRepository) GetOrCreateByPhone(ctx context.Context, orgID string, phone string, source string, defaultName string) (*Lead, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var latest *Lead
	for _, l := range r.leads {
		if l.OrgID == orgID && l.Phone == phone {
			if latest == nil || l.CreatedAt.After(latest.CreatedAt) {
				latest = l
			}
		}
	}
	if latest != nil {
		return latest, nil
	}
	name := strings.TrimSpace(defaultName)
	if name == "" {
		name = phone
	}
	req := &CreateLeadRequest{
		OrgID:   orgID,
		Name:    name,
		Phone:   phone,
		Source:  source,
		Message: "",
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	lead := &Lead{
		ID:        uuid.New().String(),
		OrgID:     req.OrgID,
		Name:      req.Name,
		Email:     req.Email,
		Phone:     req.Phone,
		Message:   req.Message,
		Source:    req.Source,
		CreatedAt: time.Now().UTC(),
	}
	r.leads[lead.ID] = lead
	return lead, nil
}

// UpdateSchedulingPreferences updates a lead's scheduling preferences
func (r *InMemoryRepository) UpdateSchedulingPreferences(ctx context.Context, leadID string, prefs SchedulingPreferences) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	lead, ok := r.leads[leadID]
	if !ok {
		return ErrLeadNotFound
	}

	// Only update fields if provided (don't overwrite with empty)
	if prefs.Name != "" {
		lead.Name = prefs.Name
	}
	if prefs.ServiceInterest != "" {
		lead.ServiceInterest = prefs.ServiceInterest
	}
	if prefs.PatientType != "" {
		lead.PatientType = prefs.PatientType
	}
	if prefs.PreferredDays != "" {
		lead.PreferredDays = prefs.PreferredDays
	}
	if prefs.PreferredTimes != "" {
		lead.PreferredTimes = prefs.PreferredTimes
	}
	if prefs.Notes != "" {
		lead.SchedulingNotes = prefs.Notes
	}
	return nil
}

// UpdateDepositStatus updates a lead's deposit status and priority
func (r *InMemoryRepository) UpdateDepositStatus(ctx context.Context, leadID string, status string, priority string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	lead, ok := r.leads[leadID]
	if !ok {
		return ErrLeadNotFound
	}

	lead.DepositStatus = status
	lead.PriorityLevel = priority
	return nil
}

// ListByOrg retrieves leads for an organization with optional filtering
func (r *InMemoryRepository) ListByOrg(ctx context.Context, orgID string, filter ListLeadsFilter) ([]*Lead, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var results []*Lead
	for _, l := range r.leads {
		if l.OrgID != orgID {
			continue
		}
		if filter.DepositStatus != "" && l.DepositStatus != filter.DepositStatus {
			continue
		}
		results = append(results, l)
	}

	// Sort by created_at descending (newest first)
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].CreatedAt.After(results[i].CreatedAt) {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	// Apply pagination
	if filter.Offset >= len(results) {
		return []*Lead{}, nil
	}
	results = results[filter.Offset:]

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}
