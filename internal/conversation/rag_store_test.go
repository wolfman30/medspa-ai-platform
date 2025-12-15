package conversation

import (
	"context"
	"errors"
	"testing"

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

func (s *stubEmbeddingClient) Embed(ctx context.Context, modelID string, texts []string) ([][]float32, error) {
	if s.err != nil {
		return nil, s.err
	}
	if len(s.nextVectors) < len(texts) {
		return nil, errors.New("insufficient stub embeddings")
	}
	data := make([][]float32, len(texts))
	for i := range texts {
		data[i] = s.nextVectors[i]
	}
	s.calls++
	return data, nil
}
