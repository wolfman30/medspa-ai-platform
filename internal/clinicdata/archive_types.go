package clinicdata

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// S3Client defines the interface for S3 operations (allows mocking in tests).
type S3Client interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

// Archiver archives conversation data to S3 before deletion.
// Data is stored in JSONL format optimized for LLM training.
// PII (names, phones) is redacted for HIPAA compliance.
type Archiver struct {
	db       db
	s3       S3Client
	bucket   string
	kmsKeyID string // Optional: KMS key for SSE-KMS encryption
	logger   *logging.Logger
}

// ArchiverConfig holds configuration for the Archiver.
type ArchiverConfig struct {
	DB       db
	S3       S3Client
	Bucket   string
	KMSKeyID string // Optional: KMS key ID for server-side encryption (SSE-KMS)
	Logger   *logging.Logger
}

// NewArchiver creates a new Archiver instance.
func NewArchiver(cfg ArchiverConfig) *Archiver {
	if cfg.Logger == nil {
		cfg.Logger = logging.Default()
	}
	return &Archiver{
		db:       cfg.DB,
		s3:       cfg.S3,
		bucket:   cfg.Bucket,
		kmsKeyID: cfg.KMSKeyID,
		logger:   cfg.Logger,
	}
}

// ArchivedConversation represents a conversation archived for training.
// Format optimized for LLM fine-tuning (JSONL).
// All PII (names, phones) is redacted.
type ArchivedConversation struct {
	ConversationID string            `json:"conversation_id"`
	OrgID          string            `json:"org_id"`
	OrgName        string            `json:"org_name,omitempty"`
	Phone          string            `json:"phone_redacted"` // Fully redacted as [PHONE]
	Channel        string            `json:"channel"`
	Messages       []ArchivedMessage `json:"messages"`
	Metadata       ArchiveMetadata   `json:"metadata"`
	ArchivedAt     time.Time         `json:"archived_at"`
	ArchiveReason  string            `json:"archive_reason"`
}

// ArchivedMessage represents a single message in the conversation.
type ArchivedMessage struct {
	Role        string    `json:"role"` // "user" or "assistant"
	Content     string    `json:"content"`
	Timestamp   time.Time `json:"timestamp"`
	Status      string    `json:"status,omitempty"`       // e.g., "delivered", "sent", "failed"
	ErrorReason string    `json:"error_reason,omitempty"` // e.g., "spam", "carrier_rejected" - for training LLM on blocked messages
}

// ArchiveMetadata contains metadata about the conversation.
type ArchiveMetadata struct {
	LeadID           string   `json:"lead_id,omitempty"`
	MessageCount     int      `json:"message_count"`
	UserMessageCount int      `json:"user_message_count"`
	AIMessageCount   int      `json:"ai_message_count"`
	StartedAt        string   `json:"started_at,omitempty"`
	EndedAt          string   `json:"ended_at,omitempty"`
	Services         []string `json:"services_discussed,omitempty"`
	DepositCollected bool     `json:"deposit_collected"`
}

// ArchiveResult contains the result of an archive operation.
type ArchiveResult struct {
	ConversationsArchived int
	MessagesArchived      int
	S3Key                 string
	BytesWritten          int64
	Encrypted             bool   // True if SSE-KMS encryption was applied
	KMSKeyID              string // KMS key used for encryption (if any)
}
