package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type KnowledgeFile struct {
	ClinicID   string     `json:"clinic_id"`
	ClinicName string     `json:"clinic_name"`
	Documents  []Document `json:"documents"`
}

type Document struct {
	Title    string `json:"title"`
	Category string `json:"category"`
	Content  string `json:"content"`
}

type KnowledgeRequest struct {
	Documents []string `json:"documents"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run scripts/seed-knowledge.go <knowledge-file.json>")
		fmt.Println("Example: go run scripts/seed-knowledge.go testdata/sample-clinic-knowledge.json")
		os.Exit(1)
	}

	apiURL := os.Getenv("API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:8080"
	}

	knowledgeFile := os.Args[1]

	fmt.Printf("🌱 Seeding Knowledge Base\n")
	fmt.Printf("============================\n")
	fmt.Printf("API URL: %s\n", apiURL)
	fmt.Printf("Knowledge file: %s\n\n", knowledgeFile)

	// Load knowledge file
	data, err := os.ReadFile(knowledgeFile)
	if err != nil {
		fmt.Printf("❌ Error reading file: %v\n", err)
		os.Exit(1)
	}

	var knowledge KnowledgeFile
	if err := json.Unmarshal(data, &knowledge); err != nil {
		fmt.Printf("❌ Error parsing JSON: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Clinic: %s (%s)\n", knowledge.ClinicName, knowledge.ClinicID)
	fmt.Printf("Documents to upload: %d\n\n", len(knowledge.Documents))

	// Convert documents to format expected by API
	// Format: "Title\n\nContent"
	docs := make([]string, len(knowledge.Documents))
	for i, doc := range knowledge.Documents {
		docs[i] = fmt.Sprintf("%s\n\n%s", doc.Title, doc.Content)
	}

	// Split into batches of 20 (API limit)
	const batchSize = 20
	totalBatches := (len(docs) + batchSize - 1) / batchSize

	ctx := context.Background()
	client := &http.Client{Timeout: 30 * time.Second}
	onboardingToken := strings.TrimSpace(os.Getenv("ONBOARDING_TOKEN"))

	for i := 0; i < len(docs); i += batchSize {
		end := i + batchSize
		if end > len(docs) {
			end = len(docs)
		}

		batch := docs[i:end]
		batchNum := (i / batchSize) + 1

		fmt.Printf("📦 Batch %d/%d: Uploading %d documents...\n", batchNum, totalBatches, len(batch))

		req := KnowledgeRequest{Documents: batch}
		payload, err := json.Marshal(req)
		if err != nil {
			fmt.Printf("   ❌ Error marshaling request: %v\n", err)
			continue
		}

		url := fmt.Sprintf("%s/knowledge/%s", apiURL, knowledge.ClinicID)
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			fmt.Printf("   ❌ Error creating request: %v\n", err)
			continue
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("X-Org-Id", knowledge.ClinicID)
		if onboardingToken != "" {
			httpReq.Header.Set("X-Onboarding-Token", onboardingToken)
		}

		resp, err := client.Do(httpReq)
		if err != nil {
			fmt.Printf("   ❌ Error sending request: %v\n", err)
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusCreated {
			var result map[string]interface{}
			if err := json.Unmarshal(body, &result); err == nil {
				fmt.Printf("   ✅ Success! Status: %v\n", result["status"])
			} else {
				fmt.Printf("   ✅ Success! (status code: %d)\n", resp.StatusCode)
			}
		} else {
			fmt.Printf("   ❌ Failed (status %d): %s\n", resp.StatusCode, string(body))
		}

		// Small delay between batches
		if batchNum < totalBatches {
			time.Sleep(500 * time.Millisecond)
		}
	}

	fmt.Printf("\n✅ Knowledge seeding complete!\n")
	fmt.Printf("\n📝 Next steps:\n")
	fmt.Printf("  1. Test RAG retrieval: curl %s/conversations/start -d '{\"clinicId\":\"%s\",\"message\":\"How much does Botox cost?\"}'\n", apiURL, knowledge.ClinicID)
	fmt.Printf("  2. Check the AI response includes pricing information\n")
	fmt.Printf("  3. Try other queries about services, policies, etc.\n")
}
