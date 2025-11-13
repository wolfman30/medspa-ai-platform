package langchain

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

func TestClientGenerate(t *testing.T) {
	var seenPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("missing auth header")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"ok"}`))
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(Config{BaseURL: server.URL, APIKey: "token"})
	if err != nil {
		t.Fatalf("client init failed: %v", err)
	}

	resp, err := client.Generate(context.Background(), GenerateRequest{
		Metadata: GenerateMetadata{
			ConversationID: "conv",
			ClinicID:       "clinic",
		},
		History: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: "system"},
		},
		LatestInput: "hello",
	})
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if resp.Message != "ok" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if seenPath != "/v1/conversations/respond" {
		t.Fatalf("unexpected path %s", seenPath)
	}
}

func TestClientAddKnowledge(t *testing.T) {
	var seen bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/knowledge/clinic-1" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		seen = true
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("client init failed: %v", err)
	}
	if err := client.AddKnowledge(context.Background(), "clinic-1", []string{"doc"}); err != nil {
		t.Fatalf("add knowledge failed: %v", err)
	}
	if !seen {
		t.Fatal("expected ingest call")
	}
}
