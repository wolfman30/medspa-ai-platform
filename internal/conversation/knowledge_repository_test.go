package conversation

import (
	"context"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisKnowledgeRepository(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	repo := NewRedisKnowledgeRepository(client)

	if err := repo.AppendDocuments(context.Background(), "clinic-a", []string{"Doc1", "Doc2"}); err != nil {
		t.Fatalf("AppendDocuments failed: %v", err)
	}

	docs, err := repo.GetDocuments(context.Background(), "clinic-a")
	if err != nil {
		t.Fatalf("GetDocuments failed: %v", err)
	}
	if len(docs) != 2 || docs[0] != "Doc1" {
		t.Fatalf("unexpected docs: %#v", docs)
	}

	all, err := repo.LoadAll(context.Background())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(all) != 1 || len(all["clinic-a"]) != 2 {
		t.Fatalf("expected clinic-a docs, got %#v", all)
	}
}
