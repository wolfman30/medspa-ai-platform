package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/emr/moxie"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/payments"
)

// CheckoutLinkCreator creates payment checkout links.
type CheckoutLinkCreator interface {
	CreatePaymentLink(ctx context.Context, params payments.CheckoutParams) (*payments.CheckoutResponse, error)
}

// ToolDeps holds shared service dependencies for tool handlers.
type ToolDeps struct {
	MoxieClient     *moxie.Client
	Messenger       conversation.ReplyMessenger
	ClinicStore     *clinic.Store
	LeadsRepo       leads.Repository
	CheckoutService CheckoutLinkCreator
}

// ToolHandler routes tool calls to the appropriate handler.
type ToolHandler struct {
	logger      *slog.Logger
	orgID       string
	from        string // caller phone (E.164)
	calledPhone string // clinic number dialed by caller (voice "to" number)
	deps        *ToolDeps
}

// NewToolHandler creates a tool handler for a specific call session.
func NewToolHandler(orgID, from, calledPhone string, deps *ToolDeps, logger *slog.Logger) *ToolHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ToolHandler{
		logger:      logger,
		orgID:       orgID,
		from:        from,
		calledPhone: calledPhone,
		deps:        deps,
	}
}

// Handle dispatches a tool call and returns the result.
func (h *ToolHandler) Handle(ctx context.Context, call ToolCall) ToolResult {
	h.logger.Info("voice-tool: executing",
		"tool", call.Name,
		"tool_use_id", call.ToolUseID,
		"org_id", h.orgID,
		"from", h.from,
		"input", string(call.Input),
	)

	var result string
	var err error

	switch call.Name {
	case "check_availability":
		result, err = h.checkAvailability(ctx, call.Input)
	case "get_clinic_info":
		result, err = h.getClinicInfo(ctx, call.Input)
	case "send_sms":
		result, err = h.sendSMS(ctx, call.Input)
	case "save_qualification":
		result, err = h.saveQualification(ctx, call.Input)
	default:
		return ToolResult{
			ToolUseID: call.ToolUseID,
			Content:   fmt.Sprintf("Unknown tool: %s", call.Name),
			IsError:   true,
		}
	}

	if err != nil {
		h.logger.Error("voice-tool: error", "tool", call.Name, "error", err)
		return ToolResult{
			ToolUseID: call.ToolUseID,
			Content:   fmt.Sprintf("Error: %v", err),
			IsError:   true,
		}
	}

	h.logger.Info("voice-tool: result", "tool", call.Name, "result", result)
	return ToolResult{
		ToolUseID: call.ToolUseID,
		Content:   result,
	}
}

// getClinicInfo handles the "get_clinic_info" tool call by returning clinic
// details (name, services, pricing, policies) from the config store.
func (h *ToolHandler) getClinicInfo(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("getClinicInfo: parse input: %w", err)
	}

	if h.deps == nil || h.deps.ClinicStore == nil {
		return `{"message": "Clinic information is not available right now."}`, nil
	}

	cfg, err := h.deps.ClinicStore.Get(ctx, h.orgID)
	if err != nil {
		return "", fmt.Errorf("getClinicInfo: get clinic config: %w", err)
	}

	info := map[string]interface{}{
		"clinic_name": cfg.Name,
		"phone":       cfg.Phone,
		"address":     cfg.Address,
		"services":    cfg.Services,
		"website":     cfg.WebsiteURL,
	}
	if cfg.ServicePriceText != nil {
		info["pricing"] = cfg.ServicePriceText
	}
	if cfg.BookingPolicies != nil {
		info["policies"] = cfg.BookingPolicies
	}
	info["timezone"] = cfg.Timezone

	result, _ := json.Marshal(info)
	return string(result), nil
}

// sendSMS handles the "send_sms" tool call by sending an SMS message
// to the caller via the configured messaging provider.
func (h *ToolHandler) sendSMS(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("sendSMS: parse input: %w", err)
	}

	if h.deps == nil || h.deps.Messenger == nil || h.deps.ClinicStore == nil {
		h.logger.Warn("voice-tool: send_sms — no messenger available")
		return `{"status": "unavailable", "message": "SMS sending is not configured"}`, nil
	}

	cfg, err := h.deps.ClinicStore.Get(ctx, h.orgID)
	if err != nil {
		return "", fmt.Errorf("sendSMS: get clinic config: %w", err)
	}

	fromNumber := h.resolveSMSFromNumber(cfg)

	err = h.deps.Messenger.SendReply(ctx, conversation.OutboundReply{
		OrgID: h.orgID,
		To:    h.from,
		From:  fromNumber,
		Body:  params.Message,
		Metadata: map[string]string{
			"source": "voice_ai",
		},
	})
	if err != nil {
		h.logger.Error("voice-tool: send_sms failed", "error", err, "to", h.from)
		return fmt.Sprintf(`{"status": "error", "message": "%s"}`, err.Error()), nil
	}

	h.logger.Info("voice-tool: SMS sent", "to", h.from, "from", fromNumber)
	return fmt.Sprintf(`{"status": "sent", "to": "%s"}`, h.from), nil
}

// saveQualification handles the "save_qualification" tool call by persisting
// patient qualification data (name, type, preferences) to the leads repository.
func (h *ToolHandler) saveQualification(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Name               string `json:"name"`
		PatientType        string `json:"patient_type"`
		Service            string `json:"service"`
		PreferredDays      string `json:"preferred_days"`
		PreferredTimes     string `json:"preferred_times"`
		ProviderPreference string `json:"provider_preference"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("saveQualification: parse input: %w", err)
	}

	if h.deps == nil || h.deps.LeadsRepo == nil {
		h.logger.Warn("voice-tool: save_qualification — no leads repo")
		return `{"status": "saved"}`, nil
	}

	// Get or create lead by phone
	lead, err := h.deps.LeadsRepo.GetOrCreateByPhone(ctx, h.orgID, h.from, "voice_call", params.Name)
	if err != nil {
		h.logger.Error("voice-tool: get/create lead failed", "error", err)
		return "", fmt.Errorf("saveQualification: get/create lead: %w", err)
	}

	// Update scheduling preferences
	err = h.deps.LeadsRepo.UpdateSchedulingPreferences(ctx, lead.ID, leads.SchedulingPreferences{
		Name:               params.Name,
		ServiceInterest:    params.Service,
		PatientType:        params.PatientType,
		PreferredDays:      params.PreferredDays,
		PreferredTimes:     params.PreferredTimes,
		ProviderPreference: params.ProviderPreference,
	})
	if err != nil {
		h.logger.Error("voice-tool: update lead prefs failed", "error", err)
		return "", fmt.Errorf("saveQualification: update lead prefs: %w", err)
	}

	h.logger.Info("voice-tool: qualification saved",
		"lead_id", lead.ID,
		"name", params.Name,
		"service", params.Service,
	)
	return fmt.Sprintf(`{"status": "saved", "lead_id": "%s"}`, lead.ID), nil
}

// resolveSMSFromNumber picks the sender number for voice-initiated SMS.
// Priority: called voice number -> clinic SMS number -> clinic phone.
func (h *ToolHandler) resolveSMSFromNumber(cfg *clinic.Config) string {
	if h.calledPhone != "" {
		return h.calledPhone
	}
	if cfg != nil && cfg.SMSPhoneNumber != "" {
		return cfg.SMSPhoneNumber
	}
	if cfg != nil {
		return cfg.Phone
	}
	return ""
}

// SendDepositSMS sends a deposit link SMS to the caller during an active voice call.
// This is called by the bridge when it detects Lauren mentioning a deposit link.
func (h *ToolHandler) SendDepositSMS(ctx context.Context, orgID, callerPhone string) error {
	if h.deps == nil || h.deps.Messenger == nil || h.deps.ClinicStore == nil {
		return fmt.Errorf("SendDepositSMS: messenger or clinic store not configured")
	}

	cfg, err := h.deps.ClinicStore.Get(ctx, orgID)
	if err != nil {
		return fmt.Errorf("SendDepositSMS: get clinic config: %w", err)
	}

	clinicName := "our office"
	if cfg.Name != "" {
		clinicName = cfg.Name
	}

	depositAmount := 50
	if cfg.DepositAmountCents > 0 {
		depositAmount = cfg.DepositAmountCents / 100
	}

	fromNumber := h.resolveSMSFromNumber(cfg)

	// Build deposit link via real Stripe Checkout Session.
	var depositURL string
	if h.deps.CheckoutService != nil {
		intentID := uuid.New()
		checkoutResp, err := h.deps.CheckoutService.CreatePaymentLink(ctx, payments.CheckoutParams{
			OrgID:           orgID,
			AmountCents:     int32(cfg.DepositAmountCents),
			BookingIntentID: intentID,
			Description:     fmt.Sprintf("Deposit – %s", clinicName),
			FromNumber:      callerPhone,
			// TODO: populate LeadID and ScheduledFor once available in voice context
		})
		if err != nil {
			return fmt.Errorf("SendDepositSMS: create checkout session: %w", err)
		}
		depositURL = checkoutResp.URL
	} else {
		h.logger.Warn("voice-tool: CheckoutService not configured, using fallback test link")
		depositURL = "https://buy.stripe.com/test_7sY4gBa7Z4Cg1DSfil7N600"
	}

	body := fmt.Sprintf(
		"Hi! This is Lauren from %s 😊\n\n"+
			"Here's your $%d deposit link to secure your appointment:\n%s\n\n"+
			"Once you complete the payment, I'll confirm your booking. "+
			"If you have any questions, just reply to this text!",
		clinicName, depositAmount, depositURL,
	)

	err = h.deps.Messenger.SendReply(ctx, conversation.OutboundReply{
		OrgID: orgID,
		To:    callerPhone,
		From:  fromNumber,
		Body:  body,
		Metadata: map[string]string{
			"source": "voice_ai_deposit",
		},
	})
	if err != nil {
		return fmt.Errorf("SendDepositSMS: send deposit SMS: %w", err)
	}

	h.logger.Info("voice-tool: deposit SMS sent",
		"to", callerPhone,
		"from", fromNumber,
		"clinic", clinicName,
		"deposit", depositAmount,
	)
	return nil
}
