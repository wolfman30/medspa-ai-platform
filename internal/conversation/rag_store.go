package conversation

import (
	"context"
	"errors"
	"math"
	"sort"
	"sync"

	openai "github.com/sashabaranov/go-openai"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

type embeddingClient interface {
	CreateEmbeddings(ctx context.Context, request openai.EmbeddingRequestConverter) (openai.EmbeddingResponse, error)
}

// RAGRetriever exposes the query capability needed by GPTService.
type RAGRetriever interface {
	Query(ctx context.Context, clinicID string, query string, topK int) ([]string, error)
}

// RAGIngestor describes how clinic knowledge is ingested.
type RAGIngestor interface {
	AddDocuments(ctx context.Context, clinicID string, contents []string) error
}

// MemoryRAGStore keeps embeddings in memory and supports simple cosine retrieval.
type MemoryRAGStore struct {
	client embeddingClient
	model  string
	logger *logging.Logger

	mu       sync.RWMutex
	docments map[string][]ragDocument // keyed by clinicID ("" for global)
}

type ragDocument struct {
	content   string
	embedding []float32
}

// NewMemoryRAGStore creates an in-memory store.
func NewMemoryRAGStore(client embeddingClient, model string, logger *logging.Logger) *MemoryRAGStore {
	if client == nil {
		panic("conversation: embedding client cannot be nil")
	}
	if model == "" {
		model = "text-embedding-3-small"
	}
	if logger == nil {
		logger = logging.Default()
	}

	return &MemoryRAGStore{
		client:   client,
		model:    model,
		logger:   logger,
		docments: make(map[string][]ragDocument),
	}
}

// AddDocuments embeds and stores the provided contents for a clinic.
func (s *MemoryRAGStore) AddDocuments(ctx context.Context, clinicID string, contents []string) error {
	if len(contents) == 0 {
		return nil
	}

	req := &openai.EmbeddingRequest{
		Model: openai.EmbeddingModel(s.model),
		Input: contents,
	}

	resp, err := s.client.CreateEmbeddings(ctx, req)
	if err != nil {
		return err
	}
	if len(resp.Data) != len(contents) {
		return errors.New("conversation: embedding response size mismatch")
	}

	clinicKey := clinicID
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range resp.Data {
		s.docments[clinicKey] = append(s.docments[clinicKey], ragDocument{
			content:   contents[i],
			embedding: item.Embedding,
		})
	}
	return nil
}

// Query returns the topK documents for a clinic (plus global docs).
func (s *MemoryRAGStore) Query(ctx context.Context, clinicID string, query string, topK int) ([]string, error) {
	if topK <= 0 {
		topK = 3
	}
	req := &openai.EmbeddingRequest{
		Model: openai.EmbeddingModel(s.model),
		Input: []string{query},
	}
	resp, err := s.client.CreateEmbeddings(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, nil
	}

	queryVec := resp.Data[0].Embedding

	s.mu.RLock()
	defer s.mu.RUnlock()
	var candidates []ragDocument
	if docs, ok := s.docments[clinicID]; ok {
		candidates = append(candidates, docs...)
	}
	if docs, ok := s.docments[""]; ok {
		candidates = append(candidates, docs...)
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	type scored struct {
		score   float64
		content string
	}
	results := make([]scored, 0, len(candidates))
	for _, doc := range candidates {
		score := cosineSimilarity(queryVec, doc.embedding)
		results = append(results, scored{score: score, content: doc.content})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	limit := topK
	if len(results) < limit {
		limit = len(results)
	}

	out := make([]string, limit)
	for i := 0; i < limit; i++ {
		out[i] = results[i].content
	}
	return out, nil
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot float64
	var normA float64
	var normB float64
	for i := range a {
		dot += float64(a[i] * b[i])
		normA += float64(a[i] * a[i])
		normB += float64(b[i] * b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
