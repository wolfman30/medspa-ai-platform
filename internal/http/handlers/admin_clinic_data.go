package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

type adminClinicDataDB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type AdminClinicDataConfig struct {
	DB    adminClinicDataDB
	Redis *redis.Client
	Logger *logging.Logger
}

// AdminClinicDataHandler provides privileged endpoints for dev/demo data maintenance.
type AdminClinicDataHandler struct {
	db     adminClinicDataDB
	redis  *redis.Client
	logger *logging.Logger
}

func NewAdminClinicDataHandler(cfg AdminClinicDataConfig) *AdminClinicDataHandler {
	if cfg.Logger == nil {
		cfg.Logger = logging.Default()
	}
	return &AdminClinicDataHandler{
		db:     cfg.DB,
		redis:  cfg.Redis,
		logger: cfg.Logger,
	}
}

type PurgePhoneResponse struct {
	OrgID          string `json:"org_id"`
	Phone          string `json:"phone"`
	PhoneDigits    string `json:"phone_digits"`
	PhoneE164      string `json:"phone_e164"`
	ConversationID string `json:"conversation_id"`
	Deleted        struct {
		ConversationJobs int64 `json:"conversation_jobs"`
		Outbox           int64 `json:"outbox"`
		Payments         int64 `json:"payments"`
		Bookings         int64 `json:"bookings"`
		Leads            int64 `json:"leads"`
		Messages         int64 `json:"messages"`
		Unsubscribes     int64 `json:"unsubscribes"`
	} `json:"deleted"`
	RedisDeleted int64 `json:"redis_deleted"`
}

// PurgePhone deletes demo data for a phone number within a clinic/org.
// Route: DELETE /admin/clinics/{orgID}/phones/{phone}
func (h *AdminClinicDataHandler) PurgePhone(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.db == nil {
		http.Error(w, "database not configured", http.StatusServiceUnavailable)
		return
	}

	orgID := strings.TrimSpace(chi.URLParam(r, "orgID"))
	phone := strings.TrimSpace(chi.URLParam(r, "phone"))
	if orgID == "" || phone == "" {
		http.Error(w, "missing orgID or phone", http.StatusBadRequest)
		return
	}

	orgUUID, err := uuid.Parse(orgID)
	if err != nil {
		http.Error(w, "orgID must be a UUID", http.StatusBadRequest)
		return
	}

	digits := sanitizeDigits(phone)
	if digits == "" {
		http.Error(w, "invalid phone", http.StatusBadRequest)
		return
	}
	digits = normalizeUSDigits(digits)
	e164 := "+" + digits
	conversationID := fmt.Sprintf("sms:%s:%s", orgID, digits)
	redisKey := fmt.Sprintf("conversation:%s", conversationID)

	ctx := r.Context()
	tx, err := h.db.Begin(ctx)
	if err != nil {
		h.logger.Error("admin purge: begin tx failed", "error", err)
		http.Error(w, "failed to start transaction", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	var resp PurgePhoneResponse
	resp.OrgID = orgID
	resp.Phone = phone
	resp.PhoneDigits = digits
	resp.PhoneE164 = e164
	resp.ConversationID = conversationID

	resp.Deleted.ConversationJobs, err = execRowsAffected(ctx, tx, `
		DELETE FROM conversation_jobs
		WHERE conversation_id = $1
	`, conversationID)
	if err != nil {
		h.logger.Error("admin purge: delete conversation_jobs failed", "error", err, "org_id", orgID)
		http.Error(w, "failed to purge conversation jobs", http.StatusInternalServerError)
		return
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
		h.logger.Error("admin purge: delete outbox failed", "error", err, "org_id", orgID)
		http.Error(w, "failed to purge outbox", http.StatusInternalServerError)
		return
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
		h.logger.Error("admin purge: delete payments failed", "error", err, "org_id", orgID)
		http.Error(w, "failed to purge payments", http.StatusInternalServerError)
		return
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
		h.logger.Error("admin purge: delete bookings failed", "error", err, "org_id", orgID)
		http.Error(w, "failed to purge bookings", http.StatusInternalServerError)
		return
	}

	resp.Deleted.Leads, err = execRowsAffected(ctx, tx, `
		DELETE FROM leads
		WHERE org_id = $1
		  AND regexp_replace(phone, '\D', '', 'g') = $2
	`, orgID, digits)
	if err != nil {
		h.logger.Error("admin purge: delete leads failed", "error", err, "org_id", orgID)
		http.Error(w, "failed to purge leads", http.StatusInternalServerError)
		return
	}

	resp.Deleted.Messages, err = execRowsAffected(ctx, tx, `
		DELETE FROM messages
		WHERE clinic_id = $1
		  AND (from_e164 = $2 OR to_e164 = $2)
	`, orgUUID, e164)
	if err != nil {
		h.logger.Error("admin purge: delete messages failed", "error", err, "org_id", orgID)
		http.Error(w, "failed to purge messages", http.StatusInternalServerError)
		return
	}

	resp.Deleted.Unsubscribes, err = execRowsAffected(ctx, tx, `
		DELETE FROM unsubscribes
		WHERE clinic_id = $1
		  AND recipient_e164 = $2
	`, orgUUID, e164)
	if err != nil {
		h.logger.Error("admin purge: delete unsubscribes failed", "error", err, "org_id", orgID)
		http.Error(w, "failed to purge unsubscribes", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		h.logger.Error("admin purge: commit failed", "error", err, "org_id", orgID)
		http.Error(w, "failed to commit purge", http.StatusInternalServerError)
		return
	}

	if h.redis != nil {
		res := h.redis.Del(ctx, redisKey)
		if err := res.Err(); err != nil {
			h.logger.Warn("admin purge: redis DEL failed", "error", err, "key", redisKey)
		} else {
			resp.RedisDeleted = res.Val()
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
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

