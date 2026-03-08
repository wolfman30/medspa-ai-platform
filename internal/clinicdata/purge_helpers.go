package clinicdata

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/wolfman30/medspa-ai-platform/internal/archive"
)

// execRowsAffected executes a SQL statement within a transaction and returns
// the number of rows affected.
func execRowsAffected(ctx context.Context, tx pgx.Tx, query string, args ...any) (int64, error) {
	tag, err := tx.Exec(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// sanitizeDigits strips all non-digit characters from a string.
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
