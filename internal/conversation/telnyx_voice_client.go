package conversation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

const (
	defaultTelnyxBaseURL = "https://api.telnyx.com/v2"
	telnyxCallTimeout    = 15 * time.Second
)

// TelnyxVoiceClient initiates outbound calls via the Telnyx Voice AI API.
type TelnyxVoiceClient struct {
	apiKey     string
	texmlAppID string
	baseURL    string
	httpClient *http.Client
	logger     *logging.Logger
}

// TelnyxVoiceClientConfig configures the outbound voice client.
type TelnyxVoiceClientConfig struct {
	// APIKey is the Telnyx API key (Bearer token).
	APIKey string
	// TexmlAppID is the Telnyx TeXML Application ID for the voice channel.
	TexmlAppID string
	// BaseURL overrides the Telnyx API base URL (for testing).
	BaseURL string
	// HTTPClient overrides the default HTTP client.
	HTTPClient *http.Client
	Logger     *logging.Logger
}

// NewTelnyxVoiceClient creates a client for initiating outbound AI voice calls.
func NewTelnyxVoiceClient(cfg TelnyxVoiceClientConfig) (*TelnyxVoiceClient, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("telnyx voice client: API key required")
	}
	if strings.TrimSpace(cfg.TexmlAppID) == "" {
		return nil, fmt.Errorf("telnyx voice client: TeXML app ID required")
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultTelnyxBaseURL
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: telnyxCallTimeout}
	}
	logger := cfg.Logger
	if logger == nil {
		logger = logging.Default()
	}
	return &TelnyxVoiceClient{
		apiKey:     cfg.APIKey,
		texmlAppID: cfg.TexmlAppID,
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
		logger:     logger,
	}, nil
}

// OutboundCallRequest contains the parameters for initiating an outbound AI call.
type OutboundCallRequest struct {
	// From is the clinic's Telnyx phone number (E.164).
	From string `json:"From"`
	// To is the patient's phone number (E.164).
	To string `json:"To"`
	// AIAssistantID is the Telnyx AI Assistant ID to use for the call.
	AIAssistantID string `json:"AIAssistantId"`
	// MachineDetection enables AMD to detect voicemails.
	MachineDetection string `json:"MachineDetection,omitempty"`
	// AsyncAmd enables asynchronous answering machine detection.
	AsyncAmd bool `json:"AsyncAmd,omitempty"`
	// DetectionMode sets the AMD detection quality.
	DetectionMode string `json:"DetectionMode,omitempty"`
}

// OutboundCallResponse is the Telnyx API response for call initiation.
type OutboundCallResponse struct {
	CallControlID string `json:"call_control_id"`
	CallLegID     string `json:"call_leg_id"`
	CallSessionID string `json:"call_session_id"`
	IsAlive       bool   `json:"is_alive"`
}

// telnyxCallAPIResponse wraps the Telnyx response envelope.
type telnyxCallAPIResponse struct {
	Data OutboundCallResponse `json:"data"`
}

// InitiateCallback starts an outbound AI voice call to the patient.
func (c *TelnyxVoiceClient) InitiateCallback(ctx context.Context, req OutboundCallRequest) (*OutboundCallResponse, error) {
	if req.From == "" || req.To == "" {
		return nil, fmt.Errorf("telnyx voice: from and to phone numbers required")
	}
	if req.AIAssistantID == "" {
		return nil, fmt.Errorf("telnyx voice: AI assistant ID required")
	}

	// Default: enable answering machine detection
	if req.MachineDetection == "" {
		req.MachineDetection = "Enable"
	}
	if req.DetectionMode == "" {
		req.DetectionMode = "Premium"
	}
	req.AsyncAmd = true

	url := fmt.Sprintf("%s/texml/ai_calls/%s", c.baseURL, c.texmlAppID)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("telnyx voice: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("telnyx voice: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	c.logger.Info("telnyx voice: initiating outbound call",
		"from", maskPhone(req.From),
		"to", maskPhone(req.To),
		"assistant_id", req.AIAssistantID,
	)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("telnyx voice: http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("telnyx voice: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.logger.Error("telnyx voice: API error",
			"status", resp.StatusCode,
			"body", string(respBody),
		)
		return nil, fmt.Errorf("telnyx voice: API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp telnyxCallAPIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("telnyx voice: decode response: %w", err)
	}

	c.logger.Info("telnyx voice: outbound call initiated",
		"call_control_id", apiResp.Data.CallControlID,
		"to", maskPhone(req.To),
	)

	return &apiResp.Data, nil
}
