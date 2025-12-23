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
	ConversationJobs int64
	Outbox           int64
	Payments         int64
	Bookings         int64
	Leads            int64
	Messages         int64
	Unsubscribes     int64
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

