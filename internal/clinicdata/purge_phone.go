package clinicdata

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

type db interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Purger deletes demo/test data for a phone number within a clinic/org.
// This is intended for development/sandbox environments only.
type Purger struct {
	db     db
	redis  *redis.Client
	logger *logging.Logger
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
}

// PurgeOrg deletes ALL data for an organization. Use with caution!
func (p *Purger) PurgeOrg(ctx context.Context, orgID string) (PurgeResult, error) {
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

	// Delete ALL outbox events for this org
	resp.Deleted.Outbox, err = execRowsAffected(ctx, tx, `
		DELETE FROM outbox WHERE org_id = $1
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

	// Clear Redis keys for this org
	if p.redis != nil {
		pattern := fmt.Sprintf("*%s*", orgID)
		keys, err := p.redis.Keys(ctx, pattern).Result()
		if err == nil && len(keys) > 0 {
			res := p.redis.Del(ctx, keys...)
			if err := res.Err(); err != nil {
				p.logger.Warn("clinicdata purge: redis DEL failed", "error", err, "org_id", orgID)
			} else {
				resp.RedisDeleted = res.Val()
			}
		}
	}

	return resp, nil
}

func (p *Purger) PurgePhone(ctx context.Context, orgID string, phone string) (PurgeResult, error) {
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

	conversationIDDigits := fmt.Sprintf("sms:%s:%s", orgID, digits)
	conversationIDE164 := fmt.Sprintf("sms:%s:%s", orgID, e164)
	redisKeyDigits := fmt.Sprintf("conversation:%s", conversationIDDigits)
	redisKeyE164 := fmt.Sprintf("conversation:%s", conversationIDE164)
	smsKeyDigits := fmt.Sprintf("sms_transcript:%s", conversationIDDigits)
	smsKeyE164 := fmt.Sprintf("sms_transcript:%s", conversationIDE164)

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
		res := p.redis.Del(ctx, keys...)
		if err := res.Err(); err != nil {
			p.logger.Warn("clinicdata purge: redis DEL failed", "error", err, "key", redisKeyDigits, "org_id", orgID)
		} else {
			resp.RedisDeleted = res.Val()
		}
	}

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
