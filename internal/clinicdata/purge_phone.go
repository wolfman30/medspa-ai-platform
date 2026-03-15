package clinicdata

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type db interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// PurgePhone deletes all data for a specific phone number within an org.
// If an archiver is configured, data is archived to S3 before deletion.
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

	// Delete conversation_messages for ALL conversations that match — including
	// those found by phone-digit regex. Without this, the wider conversations
	// DELETE hits a foreign key constraint because messages still reference them.
	resp.Deleted.ConversationMessages, err = execRowsAffected(ctx, tx, `
		DELETE FROM conversation_messages
		WHERE conversation_id IN (
			SELECT conversation_id FROM conversations
			WHERE org_id = $1
			  AND (conversation_id = $2 OR conversation_id = $3 OR regexp_replace(phone, '\D', '', 'g') = $4)
		)
	`, orgID, conversationIDDigits, conversationIDE164, digits)
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
