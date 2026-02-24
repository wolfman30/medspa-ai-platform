package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/moxie"
)

// ──────────────────────────────────────────────────────────────────────────────
// Tool definitions for Nova Sonic voice AI.
// These are the tools the model can invoke during a voice conversation.
// Phase 1: placeholder handlers that log and return mock data.
// Phase 2: wire to real Moxie API, Telnyx SMS, etc.
// ──────────────────────────────────────────────────────────────────────────────

// DefaultTools returns the standard tool definitions for MedSpa voice AI.
func DefaultTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "check_availability",
			Description: "Check available appointment times for a service at the clinic",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"service": {"type": "string", "description": "Service name (e.g., Botox, Lip Filler)"},
					"preferred_days": {"type": "string", "description": "Preferred days of the week"},
					"preferred_times": {"type": "string", "description": "Preferred time of day (morning, afternoon, evening)"},
					"provider_preference": {"type": "string", "description": "Preferred provider name"}
				},
				"required": ["service"]
			}`),
		},
		{
			Name:        "get_clinic_info",
			Description: "Get clinic information: services, pricing, policies, providers",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {"type": "string", "description": "What information to look up"}
				},
				"required": ["query"]
			}`),
		},
		{
			Name:        "send_sms",
			Description: "Send an SMS to the caller with booking link, time slots, or other info",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"message": {"type": "string", "description": "SMS content to send"}
				},
				"required": ["message"]
			}`),
		},
		{
			Name:        "save_qualification",
			Description: "Save patient qualification data (name, patient type, preferences)",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"name": {"type": "string", "description": "Patient full name"},
					"patient_type": {"type": "string", "enum": ["new", "returning"], "description": "New or returning patient"},
					"service": {"type": "string", "description": "Service of interest"},
					"preferred_days": {"type": "string"},
					"preferred_times": {"type": "string"},
					"provider_preference": {"type": "string"}
				}
			}`),
		},
	}
}

// ToolDeps holds shared service dependencies for tool handlers.
type ToolDeps struct {
	MoxieClient *moxie.Client
	Messenger   conversation.ReplyMessenger
	ClinicStore *clinic.Store
	LeadsRepo   leads.Repository
}

// ToolHandler routes tool calls to the appropriate handler.
type ToolHandler struct {
	logger *slog.Logger
	orgID  string
	from   string // caller phone (E.164)
	deps   *ToolDeps
}

// NewToolHandler creates a tool handler for a specific call session.
func NewToolHandler(orgID, from string, deps *ToolDeps, logger *slog.Logger) *ToolHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ToolHandler{
		logger: logger,
		orgID:  orgID,
		from:   from,
		deps:   deps,
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

// ── Real tool implementations ────────────────────────────────────────────────

func (h *ToolHandler) checkAvailability(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Service            string `json:"service"`
		PreferredDays      string `json:"preferred_days"`
		PreferredTimes     string `json:"preferred_times"`
		ProviderPreference string `json:"provider_preference"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}

	if h.deps == nil || h.deps.MoxieClient == nil || h.deps.ClinicStore == nil {
		h.logger.Warn("voice-tool: check_availability — no moxie client or clinic store, returning fallback")
		return `{"message": "I don't have access to the scheduling system right now. Let me take your preferences and someone will call you back to confirm."}`, nil
	}

	cfg, err := h.deps.ClinicStore.Get(ctx, h.orgID)
	if err != nil {
		return "", fmt.Errorf("get clinic config: %w", err)
	}

	if cfg.MoxieConfig == nil {
		return `{"message": "Online scheduling is not configured for this clinic. I'll take your preferences and have someone call you back."}`, nil
	}

	// Resolve service to Moxie service menu item ID
	serviceMenuItemID := ""
	if cfg.MoxieConfig.ServiceMenuItems != nil {
		normalized := strings.ToLower(strings.TrimSpace(params.Service))
		serviceMenuItemID = cfg.MoxieConfig.ServiceMenuItems[normalized]
		// Also try aliases
		if serviceMenuItemID == "" && cfg.ServiceAliases != nil {
			if alias, ok := cfg.ServiceAliases[normalized]; ok {
				serviceMenuItemID = cfg.MoxieConfig.ServiceMenuItems[strings.ToLower(alias)]
			}
		}
	}
	if serviceMenuItemID == "" {
		return fmt.Sprintf(`{"message": "I couldn't find the service '%s' in our booking system. Could you tell me more about what you're looking for?"}`, params.Service), nil
	}

	// Query next 14 days of availability
	now := time.Now()
	if cfg.Timezone != "" {
		if loc, err := time.LoadLocation(cfg.Timezone); err == nil {
			now = now.In(loc)
		}
	}
	startDate := now.Format("2006-01-02")
	endDate := now.AddDate(0, 0, 14).Format("2006-01-02")

	result, err := h.deps.MoxieClient.GetAvailableSlots(ctx, cfg.MoxieConfig.MedspaID, startDate, endDate, serviceMenuItemID, true)
	if err != nil {
		h.logger.Error("voice-tool: moxie availability error", "error", err)
		return `{"message": "I'm having trouble checking availability right now. Let me take your preferences and have someone follow up with you."}`, nil
	}

	// Format slots as readable text
	var slots []string
	for _, ds := range result.Dates {
		for _, slot := range ds.Slots {
			t, err := time.Parse(time.RFC3339, slot.Start)
			if err != nil {
				continue
			}
			// Filter by preferred times if specified
			hour := t.Hour()
			pref := strings.ToLower(params.PreferredTimes)
			if pref == "morning" && hour >= 12 {
				continue
			}
			if pref == "afternoon" && (hour < 12 || hour >= 17) {
				continue
			}
			if pref == "evening" && hour < 17 {
				continue
			}
			slots = append(slots, t.Format("Monday, January 2 at 3:04 PM"))
			if len(slots) >= 5 {
				break
			}
		}
		if len(slots) >= 5 {
			break
		}
	}

	if len(slots) == 0 {
		return `{"message": "I don't see any available slots matching your preferences in the next two weeks. Would you like me to check different days or times?"}`, nil
	}

	slotsJSON, _ := json.Marshal(slots)
	return fmt.Sprintf(`{"available_slots": %s}`, string(slotsJSON)), nil
}

func (h *ToolHandler) getClinicInfo(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}

	if h.deps == nil || h.deps.ClinicStore == nil {
		return `{"message": "Clinic information is not available right now."}`, nil
	}

	cfg, err := h.deps.ClinicStore.Get(ctx, h.orgID)
	if err != nil {
		return "", fmt.Errorf("get clinic config: %w", err)
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
	// Format business hours
	info["timezone"] = cfg.Timezone

	result, _ := json.Marshal(info)
	return string(result), nil
}

func (h *ToolHandler) sendSMS(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}

	if h.deps == nil || h.deps.Messenger == nil || h.deps.ClinicStore == nil {
		h.logger.Warn("voice-tool: send_sms — no messenger available")
		return `{"status": "unavailable", "message": "SMS sending is not configured"}`, nil
	}

	cfg, err := h.deps.ClinicStore.Get(ctx, h.orgID)
	if err != nil {
		return "", fmt.Errorf("get clinic config: %w", err)
	}

	fromNumber := cfg.SMSPhoneNumber
	if fromNumber == "" {
		fromNumber = cfg.Phone
	}

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
		return "", fmt.Errorf("parse input: %w", err)
	}

	if h.deps == nil || h.deps.LeadsRepo == nil {
		h.logger.Warn("voice-tool: save_qualification — no leads repo")
		return `{"status": "saved"}`, nil
	}

	// Get or create lead by phone
	lead, err := h.deps.LeadsRepo.GetOrCreateByPhone(ctx, h.orgID, h.from, "voice_call", params.Name)
	if err != nil {
		h.logger.Error("voice-tool: get/create lead failed", "error", err)
		return "", fmt.Errorf("get/create lead: %w", err)
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
		return "", fmt.Errorf("update lead prefs: %w", err)
	}

	h.logger.Info("voice-tool: qualification saved",
		"lead_id", lead.ID,
		"name", params.Name,
		"service", params.Service,
	)
	return fmt.Sprintf(`{"status": "saved", "lead_id": "%s"}`, lead.ID), nil
}
