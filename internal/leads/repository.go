package leads

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Repository defines the interface for lead storage
type Repository interface {
	Create(ctx context.Context, req *CreateLeadRequest) (*Lead, error)
	GetByID(ctx context.Context, id string) (*Lead, error)
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
func (r *InMemoryRepository) GetByID(ctx context.Context, id string) (*Lead, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lead, ok := r.leads[id]
	if !ok {
		return nil, ErrLeadNotFound
	}

	return lead, nil
}
