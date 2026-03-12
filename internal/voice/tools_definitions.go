package voice

import "encoding/json"

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
