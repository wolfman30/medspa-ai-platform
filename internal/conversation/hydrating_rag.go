package conversation

import (
	"context"
	"sync"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// HydratingRAGRetriever wraps a MemoryRAGStore and keeps it up-to-date by embedding any new
// documents appended to the KnowledgeRepository.
//
// This is intentionally simple: it assumes documents are append-only per clinic (true for RedisKnowledgeRepository).
// Each process embeds new docs on-demand, which keeps RAG fresh without requiring cross-process shared memory.
type HydratingRAGRetriever struct {
	repo   KnowledgeRepository
	store  *MemoryRAGStore
	logger *logging.Logger

	hydratedCounts sync.Map // clinicID -> int
	locks          sync.Map // clinicID -> *sync.Mutex
}

func NewHydratingRAGRetriever(ctx context.Context, repo KnowledgeRepository, store *MemoryRAGStore, logger *logging.Logger) *HydratingRAGRetriever {
	if repo == nil {
		panic("conversation: knowledge repo cannot be nil")
	}
	if store == nil {
		panic("conversation: rag store cannot be nil")
	}
	if logger == nil {
		logger = logging.Default()
	}
	if ctx == nil {
		ctx = context.Background()
	}

	h := &HydratingRAGRetriever{
		repo:   repo,
		store:  store,
		logger: logger,
	}

	// Seed hydrated counts so we don't re-embed docs that were already hydrated on startup.
	if docsByClinic, err := repo.LoadAll(ctx); err == nil {
		for clinicID, docs := range docsByClinic {
			h.hydratedCounts.Store(clinicID, len(docs))
		}
	} else {
		logger.Warn("failed to initialize rag hydration state", "error", err)
	}

	return h
}

func (h *HydratingRAGRetriever) Query(ctx context.Context, clinicID string, query string, topK int) ([]string, error) {
	if err := h.ensureHydrated(ctx, ""); err != nil {
		h.logger.Warn("failed to hydrate global rag docs", "error", err)
	}
	if clinicID != "" {
		if err := h.ensureHydrated(ctx, clinicID); err != nil {
			h.logger.Warn("failed to hydrate clinic rag docs", "clinic_id", clinicID, "error", err)
		}
	}
	return h.store.Query(ctx, clinicID, query, topK)
}

func (h *HydratingRAGRetriever) ensureHydrated(ctx context.Context, clinicID string) error {
	lock := h.lockForClinic(clinicID)
	lock.Lock()
	defer lock.Unlock()

	docs, err := h.repo.GetDocuments(ctx, clinicID)
	if err != nil {
		return err
	}

	start := 0
	if v, ok := h.hydratedCounts.Load(clinicID); ok {
		if n, ok := v.(int); ok {
			start = n
		}
	}
	if start >= len(docs) {
		return nil
	}

	newDocs := docs[start:]
	if err := h.store.AddDocuments(ctx, clinicID, newDocs); err != nil {
		return err
	}
	h.hydratedCounts.Store(clinicID, len(docs))
	return nil
}

func (h *HydratingRAGRetriever) lockForClinic(clinicID string) *sync.Mutex {
	lockAny, _ := h.locks.LoadOrStore(clinicID, &sync.Mutex{})
	return lockAny.(*sync.Mutex)
}
