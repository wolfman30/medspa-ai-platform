package conversation

import (
	"context"
	"fmt"

	"github.com/wolfman30/medspa-ai-platform/internal/langchain"
)

// LangChainIngestor forwards knowledge docs to the LangChain orchestrator.
type LangChainIngestor struct {
	client *langchain.Client
}

// NewLangChainIngestor builds a RAGIngestor backed by the orchestrator.
func NewLangChainIngestor(client *langchain.Client) *LangChainIngestor {
	if client == nil {
		panic("conversation: langchain client cannot be nil")
	}
	return &LangChainIngestor{client: client}
}

// AddDocuments syncs clinic-specific docs with Astra DB via LangChain.
func (i *LangChainIngestor) AddDocuments(ctx context.Context, clinicID string, docs []string) error {
	if err := i.client.AddKnowledge(ctx, clinicID, docs); err != nil {
		return fmt.Errorf("conversation: langchain ingest failed: %w", err)
	}
	return nil
}
