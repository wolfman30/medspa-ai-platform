package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

// ToolHandler routes tool calls to the appropriate handler.
type ToolHandler struct {
	logger *slog.Logger
	orgID  string
	from   string // caller phone (E.164)
}

// NewToolHandler creates a tool handler for a specific call session.
func NewToolHandler(orgID, from string, logger *slog.Logger) *ToolHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ToolHandler{
		logger: logger,
		orgID:  orgID,
		from:   from,
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

// ── Placeholder tool implementations (Phase 1: mock data) ────────────────────

func (h *ToolHandler) checkAvailability(_ context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Service            string `json:"service"`
		PreferredDays      string `json:"preferred_days"`
		PreferredTimes     string `json:"preferred_times"`
		ProviderPreference string `json:"provider_preference"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}

	h.logger.Info("voice-tool: check_availability (mock)",
		"service", params.Service,
		"days", params.PreferredDays,
		"times", params.PreferredTimes,
	)

	// Phase 1: return mock availability
	return `{"available_slots": [
		{"date": "Tuesday, February 25", "time": "10:00 AM", "provider": "Dr. Smith"},
		{"date": "Wednesday, February 26", "time": "2:30 PM", "provider": "Dr. Smith"},
		{"date": "Thursday, February 27", "time": "11:00 AM", "provider": "Dr. Johnson"}
	]}`, nil
}

func (h *ToolHandler) getClinicInfo(_ context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}

	h.logger.Info("voice-tool: get_clinic_info (mock)", "query", params.Query)

	// Phase 1: return mock clinic info
	return `{"clinic_name": "Brilliant Aesthetics", "hours": "Mon-Fri 9am-6pm, Sat 10am-4pm", "services": ["Botox", "Lip Filler", "Microneedling", "Chemical Peel", "Laser Hair Removal"]}`, nil
}

func (h *ToolHandler) sendSMS(_ context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}

	h.logger.Info("voice-tool: send_sms (mock)",
		"to", h.from,
		"message", params.Message,
	)

	// Phase 1: log and return success (Phase 2: use Telnyx SMS API)
	return fmt.Sprintf(`{"status": "sent", "to": "%s"}`, h.from), nil
}

func (h *ToolHandler) saveQualification(_ context.Context, input json.RawMessage) (string, error) {
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

	h.logger.Info("voice-tool: save_qualification (mock)",
		"name", params.Name,
		"patient_type", params.PatientType,
		"service", params.Service,
	)

	// Phase 1: log and return success (Phase 2: persist to leads repo)
	return `{"status": "saved"}`, nil
}
