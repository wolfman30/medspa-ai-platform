package archive

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockS3Client records PutObject/GetObject calls for testing.
type mockS3Client struct {
	putCalls []putCall
	objects  map[string][]byte // key -> body
}

type putCall struct {
	bucket string
	key    string
	body   []byte
}

func newMockS3() *mockS3Client {
	return &mockS3Client{objects: make(map[string][]byte)}
}

func (m *mockS3Client) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	body, _ := io.ReadAll(input.Body)
	m.putCalls = append(m.putCalls, putCall{
		bucket: *input.Bucket,
		key:    *input.Key,
		body:   body,
	})
	m.objects[*input.Key] = body
	return &s3.PutObjectOutput{}, nil
}

func (m *mockS3Client) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	data, ok := m.objects[*input.Key]
	if !ok {
		return nil, &notFoundError{}
	}
	return &s3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader(data)),
	}, nil
}

type notFoundError struct{}

func (e *notFoundError) Error() string { return "NoSuchKey: key not found" }

func TestStore_ArchiveConversation(t *testing.T) {
	mock := newMockS3()
	store := NewStore(mock, "test-bucket", nil)

	now := time.Date(2026, 2, 12, 15, 0, 0, 0, time.UTC)
	record := &ConversationRecord{
		Version:        "1.0",
		ConversationID: "conv-123",
		OrgID:          "org-456",
		PhoneHash:      HashPhone("+15551234567"),
		ArchivedAt:     now,
		MessageCount:   2,
		Outcome:        "booking_completed",
		Labels: Labels{
			ConversationCategory: "normal_booking",
			MedicalLiabilityRisk: "low",
		},
		Messages: []Message{
			{Role: "user", Content: "Book Botox", Timestamp: now},
			{Role: "assistant", Content: "Sure!", Timestamp: now},
		},
	}

	err := store.ArchiveConversation(context.Background(), record)
	require.NoError(t, err)

	// Should have 2 PutObject calls: conversation + manifest
	assert.Len(t, mock.putCalls, 2)

	// Verify conversation key
	assert.Contains(t, mock.putCalls[0].key, "conversations/v1/by-date/2026/02/12/conv-123.json")

	// Verify it's valid JSON
	var decoded ConversationRecord
	err = json.Unmarshal(mock.putCalls[0].body, &decoded)
	require.NoError(t, err)
	assert.Equal(t, "conv-123", decoded.ConversationID)

	// Verify manifest
	assert.Contains(t, mock.putCalls[1].key, "conversations/v1/manifests/")
	var entry ManifestEntry
	err = json.Unmarshal(bytes.TrimSpace(mock.putCalls[1].body), &entry)
	require.NoError(t, err)
	assert.Equal(t, "conv-123", entry.ConversationID)
}

func TestStore_Disabled(t *testing.T) {
	store := NewStore(nil, "", nil)
	assert.False(t, store.Enabled())

	err := store.ArchiveConversation(context.Background(), &ConversationRecord{})
	assert.NoError(t, err) // no-op, no error
}

func TestStore_ManifestAppend(t *testing.T) {
	mock := newMockS3()
	store := NewStore(mock, "test-bucket", nil)

	entry1 := ManifestEntry{ConversationID: "conv-1", Category: "normal_booking"}
	entry2 := ManifestEntry{ConversationID: "conv-2", Category: "abandoned"}

	require.NoError(t, store.AppendManifest(context.Background(), entry1))
	require.NoError(t, store.AppendManifest(context.Background(), entry2))

	// The second append should contain both entries
	lastPut := mock.putCalls[len(mock.putCalls)-1]
	lines := bytes.Split(bytes.TrimSpace(lastPut.body), []byte("\n"))
	assert.Len(t, lines, 2)
}
