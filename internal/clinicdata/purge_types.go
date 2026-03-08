package clinicdata

import (
	"github.com/redis/go-redis/v9"

	"github.com/wolfman30/medspa-ai-platform/internal/archive"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// Purger deletes demo/test data for a phone number within a clinic/org.
// This is intended for development/sandbox environments only.
// If an Archiver is configured, data is archived to S3 before deletion.
type Purger struct {
	db               db
	redis            *redis.Client
	logger           *logging.Logger
	archiver         *Archiver
	trainingArchiver *archive.TrainingArchiver
}

// PurgerConfig holds configuration for creating a Purger.
type PurgerConfig struct {
	DB               db
	Redis            *redis.Client
	Logger           *logging.Logger
	Archiver         *Archiver                 // Optional: if set, archives data before purging
	TrainingArchiver *archive.TrainingArchiver // Optional: if set, archives classified data for LLM training
}

// NewPurger creates a Purger with minimal configuration (db + redis + logger).
func NewPurger(db db, redis *redis.Client, logger *logging.Logger) *Purger {
	if logger == nil {
		logger = logging.Default()
	}
	return &Purger{
		db:     db,
		redis:  redis,
		logger: logger,
	}
}

// NewPurgerWithConfig creates a Purger with full configuration including archiver.
func NewPurgerWithConfig(cfg PurgerConfig) *Purger { //nolint:revive
	if cfg.Logger == nil {
		cfg.Logger = logging.Default()
	}
	return &Purger{
		db:               cfg.DB,
		redis:            cfg.Redis,
		logger:           cfg.Logger,
		archiver:         cfg.Archiver,
		trainingArchiver: cfg.TrainingArchiver,
	}
}

// PurgeCounts tracks how many rows were deleted from each table.
type PurgeCounts struct {
	ConversationJobs      int64
	ConversationMessages  int64
	Conversations         int64
	Outbox                int64
	Payments              int64
	Bookings              int64
	CallbackPromises      int64
	Escalations           int64
	ComplianceAuditEvents int64
	Leads                 int64
	Messages              int64
	Unsubscribes          int64
}

// PurgeResult contains the outcome of a purge operation.
type PurgeResult struct {
	OrgID          string
	Phone          string
	PhoneDigits    string
	PhoneE164      string
	ConversationID string // canonical conversation id (digits form)
	Deleted        PurgeCounts
	RedisDeleted   int64
	// Archive info (populated if archiver is configured)
	Archived *ArchiveResult
}

// PurgeOrgOptions provides optional parameters for PurgeOrg.
type PurgeOrgOptions struct {
	OrgName string // Used for archive metadata
}

// PurgePhoneOptions provides optional parameters for PurgePhone.
type PurgePhoneOptions struct {
	OrgName string // Used for archive metadata
}
