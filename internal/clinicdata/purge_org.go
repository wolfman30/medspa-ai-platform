package clinicdata

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

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

	conversationPattern := "%:" + orgID + ":%"

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
