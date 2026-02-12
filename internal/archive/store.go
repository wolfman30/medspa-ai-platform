package archive

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3API is the subset of the S3 client used by Store.
type S3API interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

// Store archives conversation records to S3 for LLM training.
type Store struct {
	bucket   string
	s3Client S3API
	logger   *slog.Logger
}

// NewStore creates an archive Store. If bucket is empty, all operations are no-ops.
func NewStore(s3Client S3API, bucket string, logger *slog.Logger) *Store {
	if logger == nil {
		logger = slog.Default()
	}
	return &Store{bucket: bucket, s3Client: s3Client, logger: logger}
}

// Enabled returns true if archival is configured (bucket is set).
func (s *Store) Enabled() bool {
	return s != nil && s.bucket != "" && s.s3Client != nil
}

// ArchiveConversation writes a ConversationRecord as JSON to S3 and appends to the manifest.
func (s *Store) ArchiveConversation(ctx context.Context, record *ConversationRecord) error {
	if !s.Enabled() {
		return nil
	}

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("archive: marshal record: %w", err)
	}

	now := record.ArchivedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}

	s3Key := fmt.Sprintf("conversations/v1/by-date/%d/%02d/%02d/%s.json",
		now.Year(), now.Month(), now.Day(), record.ConversationID)

	_, err = s.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(s3Key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("archive: s3 put %s: %w", s3Key, err)
	}

	s.logger.Info("archived conversation to S3",
		"conversation_id", record.ConversationID,
		"s3_key", s3Key,
		"message_count", record.MessageCount,
		"category", record.Labels.ConversationCategory,
	)

	// Append to manifest
	entry := ManifestEntry{
		ConversationID:    record.ConversationID,
		S3Key:             s3Key,
		Category:          record.Labels.ConversationCategory,
		MedicalRisk:       record.Labels.MedicalLiabilityRisk,
		InjectionDetected: record.Labels.PromptInjectionDetected,
		ArchivedAt:        now.Format(time.RFC3339),
		MessageCount:      record.MessageCount,
		Outcome:           record.Outcome,
	}

	if err := s.AppendManifest(ctx, entry); err != nil {
		// Log but don't fail — the conversation is already archived
		s.logger.Warn("failed to append manifest", "error", err, "conversation_id", record.ConversationID)
	}

	return nil
}

// AppendManifest appends a JSONL line to the monthly manifest file.
// Uses read-modify-write since S3 doesn't support append.
func (s *Store) AppendManifest(ctx context.Context, entry ManifestEntry) error {
	if !s.Enabled() {
		return nil
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("archive: marshal manifest entry: %w", err)
	}

	now := time.Now().UTC()
	manifestKey := fmt.Sprintf("conversations/v1/manifests/%d-%02d.jsonl", now.Year(), now.Month())

	// Try to read existing manifest
	var existing []byte
	getResp, err := s.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(manifestKey),
	})
	if err != nil {
		// If not found, start fresh
		var nsk *s3types.NoSuchKey
		if !isNotFoundErr(err, nsk) {
			s.logger.Debug("manifest not found, creating new", "key", manifestKey)
		}
	} else {
		existing, _ = io.ReadAll(getResp.Body)
		getResp.Body.Close()
	}

	// Append new line
	var buf bytes.Buffer
	if len(existing) > 0 {
		buf.Write(existing)
		// Ensure existing content ends with newline
		if existing[len(existing)-1] != '\n' {
			buf.WriteByte('\n')
		}
	}
	buf.Write(line)
	buf.WriteByte('\n')

	_, err = s.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(manifestKey),
		Body:        bytes.NewReader(buf.Bytes()),
		ContentType: aws.String("application/x-ndjson"),
	})
	if err != nil {
		return fmt.Errorf("archive: s3 put manifest: %w", err)
	}

	return nil
}

// isNotFoundErr checks if the error is an S3 NoSuchKey error.
func isNotFoundErr(err error, _ *s3types.NoSuchKey) bool {
	// Simple string check since errors.As with S3 types can be tricky
	return err != nil && (contains(err.Error(), "NoSuchKey") || contains(err.Error(), "404") || contains(err.Error(), "not found"))
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
