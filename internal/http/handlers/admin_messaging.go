package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/compliance"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/telnyxclient"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/templates"
	observemetrics "github.com/wolfman30/medspa-ai-platform/internal/observability/metrics"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// AdminMessagingHandler hosts privileged endpoints for messaging operations.
type AdminMessagingHandler struct {
	store             messagingStore
	logger            *logging.Logger
	telnyx            telnyxClient
	quietHours        compliance.QuietHours
	quietHoursEnabled bool
	renderer          templates.Renderer
	detector          *compliance.Detector
	messagingProfile  string
	stopAck           string
	helpAck           string
	metrics           *observemetrics.MessagingMetrics
	retryBaseDelay    time.Duration
}

type AdminMessagingConfig struct {
	Store             messagingStore
	Logger            *logging.Logger
	Telnyx            telnyxClient
	QuietHours        compliance.QuietHours
	QuietHoursEnabled bool
	MessagingProfile  string
	StopAck           string
	HelpAck           string
	RetryBaseDelay    time.Duration
	Metrics           *observemetrics.MessagingMetrics
}

func NewAdminMessagingHandler(cfg AdminMessagingConfig) *AdminMessagingHandler {
	if cfg.Logger == nil {
		cfg.Logger = logging.Default()
	}
	if cfg.RetryBaseDelay <= 0 {
		cfg.RetryBaseDelay = 5 * time.Minute
	}
	return &AdminMessagingHandler{
		store:             cfg.Store,
		logger:            cfg.Logger,
		telnyx:            cfg.Telnyx,
		quietHours:        cfg.QuietHours,
		quietHoursEnabled: cfg.QuietHoursEnabled,
		renderer:          templates.Renderer{},
		detector:          compliance.NewDetector(),
		messagingProfile:  cfg.MessagingProfile,
		stopAck:           defaultString(cfg.StopAck, "You have been opted out. Reply HELP for info."),
		helpAck:           defaultString(cfg.HelpAck, "Reply STOP to opt out or contact support@medspa.ai."),
		retryBaseDelay:    cfg.RetryBaseDelay,
		metrics:           cfg.Metrics,
	}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

type createHostedOrderRequest struct {
	ClinicID      string `json:"clinic_id"`
	PhoneNumber   string `json:"phone_number"`
	BillingNumber string `json:"billing_number"`
	ContactName   string `json:"contact_name"`
	ContactEmail  string `json:"contact_email"`
	ContactPhone  string `json:"contact_phone"`
}

func (h *AdminMessagingHandler) StartHostedOrder(w http.ResponseWriter, r *http.Request) {
	var req createHostedOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	clinicID, err := uuid.Parse(req.ClinicID)
	if err != nil {
		http.Error(w, "invalid clinic_id", http.StatusBadRequest)
		return
	}
	if req.PhoneNumber == "" || req.ContactEmail == "" || req.ContactName == "" {
		http.Error(w, "missing required fields", http.StatusBadRequest)
		return
	}
	if _, err := h.telnyx.CheckHostedEligibility(r.Context(), req.PhoneNumber); err != nil {
		h.logger.Error("eligibility check failed", "error", err)
		http.Error(w, "eligibility check failed", http.StatusBadRequest)
		return
	}
	order, err := h.telnyx.CreateHostedOrder(r.Context(), telnyxclient.HostedOrderRequest{
		ClinicID:          clinicID.String(),
		PhoneNumber:       req.PhoneNumber,
		BillingNumber:     req.BillingNumber,
		AuthorizedContact: req.ContactName,
		AuthorizedEmail:   req.ContactEmail,
		AuthorizedPhone:   req.ContactPhone,
	})
	if err != nil {
		h.logger.Error("create hosted order failed", "error", err)
		http.Error(w, "failed to create order", http.StatusBadGateway)
		return
	}
	record := messaging.HostedOrderRecord{
		ClinicID:        clinicID,
		E164Number:      req.PhoneNumber,
		Status:          order.Status,
		LastError:       order.LastError,
		ProviderOrderID: order.ID,
	}
	if err := h.store.UpsertHostedOrder(r.Context(), nil, record); err != nil {
		h.logger.Error("persist hosted order failed", "error", err)
		http.Error(w, "failed to persist order", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, order)
}

type createBrandRequest struct {
	ClinicID     string `json:"clinic_id"`
	LegalName    string `json:"legal_name"`
	EIN          string `json:"ein"`
	Website      string `json:"website"`
	AddressLine  string `json:"address_line"`
	City         string `json:"city"`
	State        string `json:"state"`
	PostalCode   string `json:"postal_code"`
	Country      string `json:"country"`
	ContactName  string `json:"contact_name"`
	ContactEmail string `json:"contact_email"`
	ContactPhone string `json:"contact_phone"`
	Vertical     string `json:"vertical"`
}

func (h *AdminMessagingHandler) CreateBrand(w http.ResponseWriter, r *http.Request) {
	var req createBrandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	clinicID, err := uuid.Parse(req.ClinicID)
	if err != nil {
		http.Error(w, "invalid clinic_id", http.StatusBadRequest)
		return
	}
	brand, err := h.telnyx.CreateBrand(r.Context(), telnyxclient.BrandRequest{
		ClinicID:     req.ClinicID,
		LegalName:    req.LegalName,
		EIN:          req.EIN,
		Website:      req.Website,
		AddressLine:  req.AddressLine,
		City:         req.City,
		State:        req.State,
		PostalCode:   req.PostalCode,
		Country:      req.Country,
		ContactName:  req.ContactName,
		ContactEmail: req.ContactEmail,
		ContactPhone: req.ContactPhone,
		Vertical:     req.Vertical,
	})
	if err != nil {
		h.logger.Error("create brand failed", "error", err)
		http.Error(w, "failed to create brand", http.StatusBadGateway)
		return
	}
	record := messaging.BrandRecord{
		ClinicID:     clinicID,
		LegalName:    req.LegalName,
		BrandID:      brand.BrandID,
		Status:       brand.Status,
		Contact:      req.ContactName,
		ContactEmail: req.ContactEmail,
		ContactPhone: req.ContactPhone,
	}
	if err := h.store.InsertBrand(r.Context(), nil, record); err != nil {
		h.logger.Error("persist brand failed", "error", err)
		http.Error(w, "persist brand failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, brand)
}

type createCampaignRequest struct {
	BrandID        string   `json:"brand_internal_id"`
	UseCase        string   `json:"use_case"`
	SampleMessages []string `json:"sample_messages"`
	HelpMessage    string   `json:"help_message"`
	StopMessage    string   `json:"stop_message"`
}

func (h *AdminMessagingHandler) CreateCampaign(w http.ResponseWriter, r *http.Request) {
	var req createCampaignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	brandUUID, err := uuid.Parse(req.BrandID)
	if err != nil {
		http.Error(w, "invalid brand_internal_id", http.StatusBadRequest)
		return
	}
	campaign, err := h.telnyx.CreateCampaign(r.Context(), telnyxclient.CampaignRequest{
		BrandID:        req.BrandID,
		UseCase:        req.UseCase,
		SampleMessages: req.SampleMessages,
		HelpMessage:    req.HelpMessage,
		StopMessage:    req.StopMessage,
	})
	if err != nil {
		h.logger.Error("create campaign failed", "error", err)
		http.Error(w, "failed to create campaign", http.StatusBadGateway)
		return
	}
	record := messaging.CampaignRecord{
		BrandID:    brandUUID,
		CampaignID: campaign.CampaignID,
		Status:     campaign.Status,
		UseCase:    campaign.UseCase,
		Samples:    req.SampleMessages,
		HelpText:   req.HelpMessage,
		StopText:   req.StopMessage,
	}
	if err := h.store.InsertCampaign(r.Context(), nil, record); err != nil {
		h.logger.Error("persist campaign failed", "error", err)
		http.Error(w, "persist campaign failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, campaign)
}

type sendMessageRequest struct {
	ClinicID      string         `json:"clinic_id"`
	From          string         `json:"from"`
	To            string         `json:"to"`
	Body          string         `json:"body"`
	MediaURLs     []string       `json:"media_urls"`
	Template      string         `json:"template"`
	TemplateData  map[string]any `json:"template_data"`
	Purpose       string         `json:"purpose"`
	CorrelationID string         `json:"correlation_id"`
}

func (h *AdminMessagingHandler) SendMessage(w http.ResponseWriter, r *http.Request) {
	var req sendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	clinicID, err := uuid.Parse(req.ClinicID)
	if err != nil {
		http.Error(w, "invalid clinic_id", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.From) == "" || strings.TrimSpace(req.To) == "" {
		http.Error(w, "from/to required", http.StatusBadRequest)
		return
	}
	body := strings.TrimSpace(req.Body)
	if req.Template != "" {
		rendered, err := h.renderer.Render("body", req.Template, req.TemplateData)
		if err != nil {
			http.Error(w, fmt.Sprintf("template error: %v", err), http.StatusBadRequest)
			return
		}
		body = rendered
	}
	if body == "" && len(req.MediaURLs) == 0 {
		http.Error(w, "body or media required", http.StatusBadRequest)
		return
	}

	normalizedTo := messaging.NormalizeE164(req.To)
	suppressedReason := ""
	if unsub, err := h.store.IsUnsubscribed(r.Context(), clinicID, normalizedTo); err != nil {
		h.logger.Error("unsubscribe check failed", "error", err)
		http.Error(w, "unsubscribe check failed", http.StatusInternalServerError)
		return
	} else if unsub {
		suppressedReason = "opt_out"
	}
	if suppressedReason == "" && h.quietHoursEnabled {
		purpose := compliance.Purpose(req.Purpose)
		if purpose == "" {
			purpose = compliance.PurposeMarketing
		}
		if h.quietHours.Suppress(time.Now(), purpose) {
			suppressedReason = "quiet_hours"
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	tx, err := h.store.Begin(ctx)
	if err != nil {
		h.logger.Error("begin tx failed", "error", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	msgRecord := messaging.MessageRecord{
		ClinicID:       clinicID,
		From:           req.From,
		To:             normalizedTo,
		Direction:      "outbound",
		Body:           body,
		Media:          req.MediaURLs,
		ProviderStatus: "suppressed",
	}

	if suppressedReason == "" {
		resp, err := h.telnyx.SendMessage(r.Context(), telnyxclient.SendMessageRequest{
			From:               req.From,
			To:                 normalizedTo,
			Body:               body,
			MediaURLs:          req.MediaURLs,
			MessagingProfileID: h.messagingProfile,
		})
		if err != nil {
			now := time.Now().UTC()
			msgRecord.ProviderStatus = "retry_pending"
			msgRecord.SendAttempts = 1
			msgRecord.LastAttemptAt = &now
			next := now.Add(h.retryBaseDelay)
			msgRecord.NextRetryAt = &next
			suppressedReason = "retry_scheduled"
		} else {
			msgRecord.ProviderStatus = resp.Status
			msgRecord.ProviderMessageID = resp.ID
		}
	}

	msgID, err := h.store.InsertMessage(ctx, tx, msgRecord)
	if err != nil {
		h.logger.Error("insert message failed", "error", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	event := events.MessageSentV1{
		MessageID:            msgID.String(),
		ClinicID:             clinicID.String(),
		FromE164:             req.From,
		ToE164:               normalizedTo,
		Body:                 body,
		MediaURLs:            req.MediaURLs,
		Provider:             "telnyx",
		SentAt:               time.Now().UTC(),
		QuietHoursSuppressed: suppressedReason == "quiet_hours",
		OptOutSuppressed:     suppressedReason == "opt_out",
		ProviderMessageID:    msgRecord.ProviderMessageID,
	}
	if _, err := events.AppendCanonicalEvent(ctx, tx, "clinic:"+clinicID.String(), req.CorrelationID, event); err != nil {
		h.logger.Error("append event failed", "error", err)
		http.Error(w, "event error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		h.logger.Error("commit failed", "error", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	if h.metrics != nil {
		h.metrics.ObserveOutbound(msgRecord.ProviderStatus, suppressedReason != "")
	}

	response := map[string]any{
		"message_id":        msgID,
		"provider_status":   msgRecord.ProviderStatus,
		"suppressed_reason": suppressedReason,
	}
	writeJSON(w, http.StatusAccepted, response)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
