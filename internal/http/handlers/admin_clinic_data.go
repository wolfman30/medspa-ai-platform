package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/wolfman30/medspa-ai-platform/internal/clinicdata"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

type adminClinicDataDB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type AdminClinicDataConfig struct {
	DB     adminClinicDataDB
	Redis  *redis.Client
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

// PurgeOrg deletes ALL demo data for a clinic/org.
// Route: DELETE /admin/clinics/{orgID}/data
func (h *AdminClinicDataHandler) PurgeOrg(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.db == nil {
		http.Error(w, "database not configured", http.StatusServiceUnavailable)
		return
	}

	orgID := strings.TrimSpace(chi.URLParam(r, "orgID"))
	if orgID == "" {
		http.Error(w, "missing orgID", http.StatusBadRequest)
		return
	}

	result, err := clinicdata.NewPurger(h.db, h.redis, h.logger).PurgeOrg(r.Context(), orgID)
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "missing orgID") || strings.Contains(msg, "orgID must be a UUID"):
			http.Error(w, msg, http.StatusBadRequest)
		case strings.Contains(msg, "database not configured"):
			http.Error(w, msg, http.StatusServiceUnavailable)
		default:
			h.logger.Error("admin purge org failed", "error", err, "org_id", orgID)
			http.Error(w, "failed to purge org data", http.StatusInternalServerError)
		}
		return
	}

	resp := PurgePhoneResponse{
		OrgID:          result.OrgID,
		Phone:          "ALL",
		ConversationID: result.ConversationID,
		RedisDeleted:   result.RedisDeleted,
	}
	resp.Deleted.ConversationJobs = result.Deleted.ConversationJobs
	resp.Deleted.Outbox = result.Deleted.Outbox
	resp.Deleted.Payments = result.Deleted.Payments
	resp.Deleted.Bookings = result.Deleted.Bookings
	resp.Deleted.Leads = result.Deleted.Leads
	resp.Deleted.Messages = result.Deleted.Messages
	resp.Deleted.Unsubscribes = result.Deleted.Unsubscribes

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// PurgePhone deletes demo data for a phone number within a clinic/org.
// Route: DELETE /admin/clinics/{orgID}/phones/{phone}
func (h *AdminClinicDataHandler) PurgePhone(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.db == nil {
		http.Error(w, "database not configured", http.StatusServiceUnavailable)
		return
	}

	orgID := strings.TrimSpace(chi.URLParam(r, "orgID"))
	phoneParam := chi.URLParam(r, "phone")
	phone, err := url.PathUnescape(phoneParam)
	if err != nil {
		http.Error(w, "invalid phone encoding", http.StatusBadRequest)
		return
	}
	phone = strings.TrimSpace(phone)
	if orgID == "" || phone == "" {
		http.Error(w, "missing orgID or phone", http.StatusBadRequest)
		return
	}

	result, err := clinicdata.NewPurger(h.db, h.redis, h.logger).PurgePhone(r.Context(), orgID, phone)
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "missing orgID") || strings.Contains(msg, "invalid phone") || strings.Contains(msg, "orgID must be a UUID"):
			http.Error(w, msg, http.StatusBadRequest)
		case strings.Contains(msg, "database not configured"):
			http.Error(w, msg, http.StatusServiceUnavailable)
		default:
			h.logger.Error("admin purge failed", "error", err, "org_id", orgID)
			http.Error(w, "failed to purge phone", http.StatusInternalServerError)
		}
		return
	}

	resp := PurgePhoneResponse{
		OrgID:          result.OrgID,
		Phone:          result.Phone,
		PhoneDigits:    result.PhoneDigits,
		PhoneE164:      result.PhoneE164,
		ConversationID: result.ConversationID,
		RedisDeleted:   result.RedisDeleted,
	}
	resp.Deleted.ConversationJobs = result.Deleted.ConversationJobs
	resp.Deleted.Outbox = result.Deleted.Outbox
	resp.Deleted.Payments = result.Deleted.Payments
	resp.Deleted.Bookings = result.Deleted.Bookings
	resp.Deleted.Leads = result.Deleted.Leads
	resp.Deleted.Messages = result.Deleted.Messages
	resp.Deleted.Unsubscribes = result.Deleted.Unsubscribes

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
