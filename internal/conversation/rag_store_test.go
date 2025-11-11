package conversation

import (
	"context"
	"errors"
	"testing"

	openai "github.com/sashabaranov/go-openai"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestMemoryRAGStore_AddAndQuery(t *testing.T) {
	client := &stubEmbeddingClient{}
	store := NewMemoryRAGStore(client, "text-embedding-3-small", logging.Default())

	client.nextVectors = [][]float32{
		{1, 0},
		{0, 1},
	}
	err := store.AddDocuments(context.Background(), "clinic-1", []string{"Botox overview", "Hydrafacial steps"})
	if err != nil {
		t.Fatalf("AddDocuments error: %v", err)
	}

	client.nextVectors = [][]float32{{0.9, 0.1}}
	results, err := store.Query(context.Background(), "clinic-1", "How do Botox treatments work?", 2)
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0] != "Botox overview" {
		t.Fatalf("expected Botox doc first, got %s", results[0])
	}
}

func TestMemoryRAGStore_UsesGlobalDocs(t *testing.T) {
	client := &stubEmbeddingClient{}
	store := NewMemoryRAGStore(client, "text-embedding-3-small", logging.Default())

	client.nextVectors = [][]float32{{1, 0}}
	_ = store.AddDocuments(context.Background(), "", []string{"Global policy"})

	client.nextVectors = [][]float32{{1, 0}}
	results, err := store.Query(context.Background(), "unknown", "policy question", 1)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(results) != 1 || results[0] != "Global policy" {
		t.Fatalf("expected global doc returned, got %#v", results)
	}
}

func TestMemoryRAGStore_EmbeddingError(t *testing.T) {
	client := &stubEmbeddingClient{err: errors.New("boom")}
	store := NewMemoryRAGStore(client, "text-embedding-3-small", logging.Default())

	if err := store.AddDocuments(context.Background(), "", []string{"a"}); err == nil {
		t.Fatal("expected error when embedding fails")
	}
}

type stubEmbeddingClient struct {
	nextVectors [][]float32
	err         error
	calls       int
}

func (s *stubEmbeddingClient) CreateEmbeddings(ctx context.Context, request openai.EmbeddingRequestConverter) (openai.EmbeddingResponse, error) {
	if s.err != nil {
		return openai.EmbeddingResponse{}, s.err
	}
	req, ok := request.(*openai.EmbeddingRequest)
	if !ok {
		return openai.EmbeddingResponse{}, errors.New("unexpected request type")
	}
	inputs, ok := req.Input.([]string)
	if !ok {
		return openai.EmbeddingResponse{}, errors.New("unexpected input payload")
	}
	if len(s.nextVectors) < len(inputs) {
		return openai.EmbeddingResponse{}, errors.New("insufficient stub embeddings")
	}
	data := make([]openai.Embedding, len(inputs))
	for i := range inputs {
		data[i] = openai.Embedding{Embedding: s.nextVectors[i]}
	}
	s.calls++
	return openai.EmbeddingResponse{Data: data}, nil
}
