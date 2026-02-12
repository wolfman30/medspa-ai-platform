package clinicdata

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/wolfman30/medspa-ai-platform/internal/archive"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

type db interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

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

// PurgeOrg deletes ALL data for an organization. Use with caution!
// If an archiver is configured, data is archived to S3 first.
func (p *Purger) PurgeOrg(ctx context.Context, orgID string, opts ...PurgeOrgOptions) (PurgeResult, error) {
	orgID = strings.TrimSpace(orgID)
	if p == nil || p.db == nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: database not configured")
	}
	if orgID == "" {
		return PurgeResult{}, fmt.Errorf("clinicdata: missing orgID")
	}

	orgUUID, err := uuid.Parse(orgID)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: orgID must be a UUID: %w", err)
	}

	// Extract options
	var orgName string
	if len(opts) > 0 {
		orgName = opts[0].OrgName
	}

	// Archive before purging if archiver is configured
	var archiveResult *ArchiveResult
	if p.archiver != nil {
		result, err := p.archiver.ArchiveOrg(ctx, orgID, orgName)
		if err != nil {
			p.logger.Error("clinicdata: archive failed, aborting purge", "error", err, "org_id", orgID)
			return PurgeResult{}, fmt.Errorf("clinicdata: archive failed: %w", err)
		}
		archiveResult = result
		p.logger.Info("clinicdata: archived org data before purge",
			"org_id", orgID,
			"conversations", result.ConversationsArchived,
			"messages", result.MessagesArchived,
			"s3_key", result.S3Key,
		)
	}

	// Training archive for org: archive each conversation for LLM training (non-blocking)
	if p.trainingArchiver != nil {
		p.runTrainingArchiveOrg(ctx, orgID)
	}

	conversationPattern := "sms:" + orgID + ":%"

	tx, err := p.db.Begin(ctx)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var resp PurgeResult
	resp.OrgID = orgID
	resp.Phone = "ALL"
	resp.ConversationID = conversationPattern

	// Delete ALL conversation jobs for this org
	resp.Deleted.ConversationJobs, err = execRowsAffected(ctx, tx, `
		DELETE FROM conversation_jobs WHERE conversation_id LIKE $1
	`, conversationPattern)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete conversation jobs: %w", err)
	}

	// Delete ALL conversation messages for this org
	resp.Deleted.ConversationMessages, err = execRowsAffected(ctx, tx, `
		DELETE FROM conversation_messages WHERE conversation_id LIKE $1
	`, conversationPattern)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete conversation messages: %w", err)
	}

	// Delete ALL conversations for this org
	resp.Deleted.Conversations, err = execRowsAffected(ctx, tx, `
		DELETE FROM conversations WHERE org_id = $1
	`, orgID)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete conversations: %w", err)
	}

	// Delete ALL outbox events for this org (column is named 'aggregate' not 'org_id')
	resp.Deleted.Outbox, err = execRowsAffected(ctx, tx, `
		DELETE FROM outbox WHERE aggregate = $1
	`, orgID)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete outbox: %w", err)
	}

	// Delete ALL payments for this org
	resp.Deleted.Payments, err = execRowsAffected(ctx, tx, `
		DELETE FROM payments WHERE org_id = $1
	`, orgID)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete payments: %w", err)
	}

	// Delete ALL bookings for this org
	resp.Deleted.Bookings, err = execRowsAffected(ctx, tx, `
		DELETE FROM bookings WHERE org_id = $1
	`, orgID)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete bookings: %w", err)
	}

	// Delete ALL callback promises for this org
	resp.Deleted.CallbackPromises, err = execRowsAffected(ctx, tx, `
		DELETE FROM callback_promises WHERE org_id = $1
	`, orgUUID)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete callback promises: %w", err)
	}

	// Delete ALL escalations for this org
	resp.Deleted.Escalations, err = execRowsAffected(ctx, tx, `
		DELETE FROM escalations WHERE org_id = $1
	`, orgUUID)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete escalations: %w", err)
	}

	// Delete ALL compliance events for this org
	resp.Deleted.ComplianceAuditEvents, err = execRowsAffected(ctx, tx, `
		DELETE FROM compliance_audit_events WHERE org_id = $1
	`, orgUUID)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete compliance events: %w", err)
	}

	// Delete ALL leads for this org
	resp.Deleted.Leads, err = execRowsAffected(ctx, tx, `
		DELETE FROM leads WHERE org_id = $1
	`, orgID)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete leads: %w", err)
	}

	// Delete ALL messages for this org
	resp.Deleted.Messages, err = execRowsAffected(ctx, tx, `
		DELETE FROM messages WHERE clinic_id = $1
	`, orgUUID)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete messages: %w", err)
	}

	// Delete ALL unsubscribes for this org
	resp.Deleted.Unsubscribes, err = execRowsAffected(ctx, tx, `
		DELETE FROM unsubscribes WHERE clinic_id = $1
	`, orgUUID)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete unsubscribes: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: commit purge: %w", err)
	}

	// Clear Redis keys for this org - ONLY conversation-related keys
	// Preserve clinic:config:{orgID} and rag:docs:{orgID} (clinic configuration and knowledge)
	if p.redis != nil {
		// Only delete conversation and transcript keys, not clinic config or knowledge
		patterns := []string{
			fmt.Sprintf("conversation:sms:%s:*", orgID),
			fmt.Sprintf("sms_transcript:sms:%s:*", orgID),
			fmt.Sprintf("time_selection:sms:%s:*", orgID),
		}
		for _, pattern := range patterns {
			keys, err := p.redis.Keys(ctx, pattern).Result()
			if err == nil && len(keys) > 0 {
				res := p.redis.Del(ctx, keys...)
				if err := res.Err(); err != nil {
					p.logger.Warn("clinicdata purge: redis DEL failed", "error", err, "pattern", pattern, "org_id", orgID)
				} else {
					resp.RedisDeleted += res.Val()
				}
			}
		}
	}

	resp.Archived = archiveResult
	return resp, nil
}

// PurgePhoneOptions provides optional parameters for PurgePhone.
type PurgePhoneOptions struct {
	OrgName string // Used for archive metadata
}

func (p *Purger) PurgePhone(ctx context.Context, orgID string, phone string, opts ...PurgePhoneOptions) (PurgeResult, error) {
	orgID = strings.TrimSpace(orgID)
	phone = strings.TrimSpace(phone)
	if p == nil || p.db == nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: database not configured")
	}
	if orgID == "" || phone == "" {
		return PurgeResult{}, fmt.Errorf("clinicdata: missing orgID or phone")
	}

	orgUUID, err := uuid.Parse(orgID)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: orgID must be a UUID: %w", err)
	}

	digits := sanitizeDigits(phone)
	if digits == "" {
		return PurgeResult{}, fmt.Errorf("clinicdata: invalid phone")
	}
	digits = normalizeUSDigits(digits)
	e164 := "+" + digits

	// Extract options
	var orgName string
	if len(opts) > 0 {
		orgName = opts[0].OrgName
	}

	// Archive before purging if archiver is configured
	var archiveResult *ArchiveResult
	if p.archiver != nil {
		result, err := p.archiver.ArchivePhone(ctx, orgID, orgName, phone)
		if err != nil {
			p.logger.Error("clinicdata: archive failed, aborting purge", "error", err, "org_id", orgID, "phone", phone)
			return PurgeResult{}, fmt.Errorf("clinicdata: archive failed: %w", err)
		}
		archiveResult = result
		if result.MessagesArchived > 0 {
			p.logger.Info("clinicdata: archived phone data before purge",
				"org_id", orgID,
				"phone", phone,
				"messages", result.MessagesArchived,
				"s3_key", result.S3Key,
			)
		}
	}

	// Training archive: classify and archive for LLM training (non-blocking)
	if p.trainingArchiver != nil {
		convID := fmt.Sprintf("sms:%s:%s", orgID, digits)
		p.runTrainingArchive(ctx, orgID, e164, convID)
	}

	conversationIDDigits := fmt.Sprintf("sms:%s:%s", orgID, digits)
	conversationIDE164 := fmt.Sprintf("sms:%s:%s", orgID, e164)
	redisKeyDigits := fmt.Sprintf("conversation:%s", conversationIDDigits)
	redisKeyE164 := fmt.Sprintf("conversation:%s", conversationIDE164)
	smsKeyDigits := fmt.Sprintf("sms_transcript:%s", conversationIDDigits)
	smsKeyE164 := fmt.Sprintf("sms_transcript:%s", conversationIDE164)
	tsKeyDigits := fmt.Sprintf("time_selection:%s", conversationIDDigits)
	tsKeyE164 := fmt.Sprintf("time_selection:%s", conversationIDE164)

	tx, err := p.db.Begin(ctx)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var resp PurgeResult
	resp.OrgID = orgID
	resp.Phone = phone
	resp.PhoneDigits = digits
	resp.PhoneE164 = e164
	resp.ConversationID = conversationIDDigits

	// Conversation job records may be keyed by either digits or +E164 variants depending on provider.
	resp.Deleted.ConversationJobs, err = execRowsAffected(ctx, tx, `
		DELETE FROM conversation_jobs
		WHERE conversation_id = $1 OR conversation_id = $2
	`, conversationIDDigits, conversationIDE164)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete conversation jobs: %w", err)
	}

	resp.Deleted.ConversationMessages, err = execRowsAffected(ctx, tx, `
		DELETE FROM conversation_messages
		WHERE conversation_id = $1 OR conversation_id = $2
	`, conversationIDDigits, conversationIDE164)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete conversation messages: %w", err)
	}

	resp.Deleted.Conversations, err = execRowsAffected(ctx, tx, `
		DELETE FROM conversations
		WHERE org_id = $1
		  AND (conversation_id = $2 OR conversation_id = $3 OR regexp_replace(phone, '\D', '', 'g') = $4)
	`, orgID, conversationIDDigits, conversationIDE164, digits)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete conversations: %w", err)
	}

	resp.Deleted.Outbox, err = execRowsAffected(ctx, tx, `
		DELETE FROM outbox
		WHERE event_type LIKE 'payments.deposit.%'
		  AND (payload->>'lead_id') IN (
			SELECT id::text
			FROM leads
			WHERE org_id = $1
			  AND regexp_replace(phone, '\D', '', 'g') = $2
		  )
	`, orgID, digits)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete outbox: %w", err)
	}

	resp.Deleted.Payments, err = execRowsAffected(ctx, tx, `
		DELETE FROM payments
		WHERE org_id = $1
		  AND lead_id IN (
			SELECT id
			FROM leads
			WHERE org_id = $1
			  AND regexp_replace(phone, '\D', '', 'g') = $2
		  )
	`, orgID, digits)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete payments: %w", err)
	}

	resp.Deleted.Bookings, err = execRowsAffected(ctx, tx, `
		DELETE FROM bookings
		WHERE org_id = $1
		  AND lead_id IN (
			SELECT id
			FROM leads
			WHERE org_id = $1
			  AND regexp_replace(phone, '\D', '', 'g') = $2
		  )
	`, orgID, digits)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete bookings: %w", err)
	}

	resp.Deleted.CallbackPromises, err = execRowsAffected(ctx, tx, `
		DELETE FROM callback_promises
		WHERE org_id = $1
		  AND (
			regexp_replace(customer_phone, '\D', '', 'g') = $2
			OR lead_id IN (
				SELECT id
				FROM leads
				WHERE org_id = $3
				  AND regexp_replace(phone, '\D', '', 'g') = $2
			)
		  )
	`, orgUUID, digits, orgID)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete callback promises: %w", err)
	}

	resp.Deleted.Escalations, err = execRowsAffected(ctx, tx, `
		DELETE FROM escalations
		WHERE org_id = $1
		  AND (
			regexp_replace(customer_phone, '\D', '', 'g') = $2
			OR lead_id IN (
				SELECT id
				FROM leads
				WHERE org_id = $3
				  AND regexp_replace(phone, '\D', '', 'g') = $2
			)
		  )
	`, orgUUID, digits, orgID)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete escalations: %w", err)
	}

	resp.Deleted.ComplianceAuditEvents, err = execRowsAffected(ctx, tx, `
		DELETE FROM compliance_audit_events
		WHERE org_id = $1
		  AND (
			conversation_id = $2
			OR conversation_id = $3
			OR lead_id IN (
				SELECT id
				FROM leads
				WHERE org_id = $4
				  AND regexp_replace(phone, '\D', '', 'g') = $5
			)
		  )
	`, orgUUID, conversationIDDigits, conversationIDE164, orgID, digits)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete compliance events: %w", err)
	}

	resp.Deleted.Leads, err = execRowsAffected(ctx, tx, `
		DELETE FROM leads
		WHERE org_id = $1
		  AND regexp_replace(phone, '\D', '', 'g') = $2
	`, orgID, digits)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete leads: %w", err)
	}

	resp.Deleted.Messages, err = execRowsAffected(ctx, tx, `
		DELETE FROM messages
		WHERE clinic_id = $1
		  AND (from_e164 = $2 OR to_e164 = $2)
	`, orgUUID, e164)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete messages: %w", err)
	}

	resp.Deleted.Unsubscribes, err = execRowsAffected(ctx, tx, `
		DELETE FROM unsubscribes
		WHERE clinic_id = $1
		  AND recipient_e164 = $2
	`, orgUUID, e164)
	if err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: delete unsubscribes: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return PurgeResult{}, fmt.Errorf("clinicdata: commit purge: %w", err)
	}

	if p.redis != nil {
		keys := []string{redisKeyDigits}
		if redisKeyE164 != redisKeyDigits {
			keys = append(keys, redisKeyE164)
		}
		keys = append(keys, smsKeyDigits)
		if smsKeyE164 != smsKeyDigits {
			keys = append(keys, smsKeyE164)
		}
		keys = append(keys, tsKeyDigits)
		if tsKeyE164 != tsKeyDigits {
			keys = append(keys, tsKeyE164)
		}
		res := p.redis.Del(ctx, keys...)
		if err := res.Err(); err != nil {
			p.logger.Warn("clinicdata purge: redis DEL failed", "error", err, "key", redisKeyDigits, "org_id", orgID)
		} else {
			resp.RedisDeleted = res.Val()
		}
	}

	resp.Archived = archiveResult
	return resp, nil
}

func execRowsAffected(ctx context.Context, tx pgx.Tx, query string, args ...any) (int64, error) {
	tag, err := tx.Exec(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func sanitizeDigits(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// normalizeUSDigits converts 10-digit US numbers to E.164 digits by prefixing "1".
func normalizeUSDigits(digits string) string {
	if len(digits) == 10 {
		return "1" + digits
	}
	return digits
}

// runTrainingArchiveOrg archives all conversations for an org for LLM training.
func (p *Purger) runTrainingArchiveOrg(ctx context.Context, orgID string) {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		p.logger.Error("training archive org: begin tx", "error", err)
		return
	}
	defer tx.Rollback(ctx)

	pattern := "sms:" + orgID + ":%"
	rows, err := tx.Query(ctx, `SELECT DISTINCT conversation_id FROM conversation_messages WHERE conversation_id LIKE $1`, pattern)
	if err != nil {
		p.logger.Error("training archive org: query conversations", "error", err)
		return
	}
	defer rows.Close()

	var convIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		convIDs = append(convIDs, id)
	}

	for _, convID := range convIDs {
		// Extract phone from conversation ID: sms:orgID:digits
		parts := strings.Split(convID, ":")
		phone := ""
		if len(parts) >= 3 {
			phone = "+" + parts[2]
		}
		p.runTrainingArchive(ctx, orgID, phone, convID)
	}
}

// runTrainingArchive fetches conversation messages and archives them for LLM training.
// Errors are logged but never propagated to avoid blocking the purge.
func (p *Purger) runTrainingArchive(ctx context.Context, orgID, phone, conversationID string) {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		p.logger.Error("training archive: begin tx", "error", err)
		return
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT role, content, created_at
		FROM conversation_messages
		WHERE conversation_id = $1
		ORDER BY created_at ASC
	`, conversationID)
	if err != nil {
		p.logger.Error("training archive: query messages", "error", err, "conversation_id", conversationID)
		return
	}
	defer rows.Close()

	var msgs []archive.Message
	for rows.Next() {
		var m archive.Message
		if err := rows.Scan(&m.Role, &m.Content, &m.Timestamp); err != nil {
			p.logger.Error("training archive: scan message", "error", err)
			return
		}
		msgs = append(msgs, m)
	}

	if len(msgs) == 0 {
		return
	}

	// Try to get lead context
	var outcome string
	var depositPaid bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM payments pa
			JOIN leads l ON l.id = pa.lead_id
			WHERE l.org_id = $1 AND regexp_replace(l.phone, '\D', '', 'g') = $2 AND pa.status = 'completed')
	`, orgID, strings.TrimPrefix(phone, "+")).Scan(&depositPaid)
	if err != nil {
		depositPaid = false
	}

	if depositPaid {
		outcome = "booking_completed"
	} else {
		outcome = "purged"
	}

	p.trainingArchiver.Archive(ctx, archive.TrainingArchiveInput{
		ConversationID:   conversationID,
		OrgID:            orgID,
		Phone:            phone,
		Messages:         msgs,
		Outcome:          outcome,
		PaymentCompleted: depositPaid,
	})
}
