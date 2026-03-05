package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// ResearchDoc represents a research document stored in S3.
type ResearchDoc struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Summary   string   `json:"summary"`
	Category  string   `json:"category"`
	Tags      []string `json:"tags"`
	Content   string   `json:"content"`
	UpdatedAt string   `json:"updatedAt"`
}

// ResearchIndex is the S3-stored index of all research docs.
type ResearchIndex struct {
	Docs []ResearchDoc `json:"docs"`
}

// AdminResearchHandler serves research documents from S3.
type AdminResearchHandler struct {
	s3     S3Client
	bucket string
}

// NewAdminResearchHandler creates a new research handler.
func NewAdminResearchHandler(s3c S3Client, bucket string) *AdminResearchHandler {
	return &AdminResearchHandler{s3: s3c, bucket: bucket}
}

const researchIndexKey = "research/index.json"

// ListDocs returns all research documents.
func (h *AdminResearchHandler) ListDocs(w http.ResponseWriter, r *http.Request) {
	if h.s3 == nil || h.bucket == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"docs": []any{}})
		return
	}

	idx, err := h.readIndex(r.Context())
	if err != nil {
		http.Error(w, "failed to read research index: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"docs": idx.Docs})
}

// PutDoc adds or updates a research document in the index.
func (h *AdminResearchHandler) PutDoc(w http.ResponseWriter, r *http.Request) {
	if h.s3 == nil || h.bucket == "" {
		http.Error(w, "s3 not configured", http.StatusInternalServerError)
		return
	}

	var doc ResearchDoc
	if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if doc.ID == "" || doc.Title == "" {
		http.Error(w, "id and title required", http.StatusBadRequest)
		return
	}
	if doc.Tags == nil {
		doc.Tags = []string{}
	}

	idx, err := h.readIndex(r.Context())
	if err != nil {
		http.Error(w, "failed to read index: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Upsert
	found := false
	for i, existing := range idx.Docs {
		if existing.ID == doc.ID {
			idx.Docs[i] = doc
			found = true
			break
		}
	}
	if !found {
		idx.Docs = append(idx.Docs, doc)
	}

	if err := h.writeIndex(r.Context(), idx); err != nil {
		http.Error(w, "failed to write index: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(doc)
}

func (h *AdminResearchHandler) readIndex(ctx context.Context) (*ResearchIndex, error) {
	out, err := h.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(h.bucket),
		Key:    aws.String(researchIndexKey),
	})
	if err != nil {
		var nsk *s3types.NoSuchKey
		if errors.As(err, &nsk) || strings.Contains(err.Error(), "NoSuchKey") {
			return &ResearchIndex{Docs: []ResearchDoc{}}, nil
		}
		return nil, err
	}
	defer out.Body.Close()

	body, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, err
	}

	var idx ResearchIndex
	if err := json.Unmarshal(body, &idx); err != nil {
		return &ResearchIndex{Docs: []ResearchDoc{}}, nil
	}
	if idx.Docs == nil {
		idx.Docs = []ResearchDoc{}
	}
	return &idx, nil
}

func (h *AdminResearchHandler) writeIndex(ctx context.Context, idx *ResearchIndex) error {
	data, err := json.Marshal(idx)
	if err != nil {
		return err
	}
	_, err = h.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(h.bucket),
		Key:         aws.String(researchIndexKey),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	})
	return err
}
