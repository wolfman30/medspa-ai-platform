package conversation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewTelnyxVoiceClient_MissingAPIKey(t *testing.T) {
	_, err := NewTelnyxVoiceClient(TelnyxVoiceClientConfig{
		TexmlAppID: "app_123",
	})
	if err == nil {
		t.Error("expected error for missing API key")
	}
}

func TestNewTelnyxVoiceClient_MissingTexmlAppID(t *testing.T) {
	_, err := NewTelnyxVoiceClient(TelnyxVoiceClientConfig{
		APIKey: "key_123",
	})
	if err == nil {
		t.Error("expected error for missing TeXML app ID")
	}
}

func TestNewTelnyxVoiceClient_Success(t *testing.T) {
	client, err := NewTelnyxVoiceClient(TelnyxVoiceClientConfig{
		APIKey:     "key_123",
		TexmlAppID: "app_456",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestInitiateCallback_MissingPhones(t *testing.T) {
	client, _ := NewTelnyxVoiceClient(TelnyxVoiceClientConfig{
		APIKey:     "key_123",
		TexmlAppID: "app_456",
	})

	tests := []struct {
		name string
		req  OutboundCallRequest
	}{
		{"missing from", OutboundCallRequest{To: "+15551234567", AIAssistantID: "ast_1"}},
		{"missing to", OutboundCallRequest{From: "+15559876543", AIAssistantID: "ast_1"}},
		{"missing assistant", OutboundCallRequest{From: "+15559876543", To: "+15551234567"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.InitiateCallback(context.Background(), tt.req)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestInitiateCallback_Success(t *testing.T) {
	// Mock Telnyx API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("method: got %s, want POST", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test_key" {
			t.Errorf("auth: got %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type: got %q", r.Header.Get("Content-Type"))
		}

		// Verify body
		var req OutboundCallRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if req.From != "+15559876543" {
			t.Errorf("From: got %q", req.From)
		}
		if req.To != "+15551234567" {
			t.Errorf("To: got %q", req.To)
		}
		if req.AIAssistantID != "ast_test" {
			t.Errorf("AIAssistantID: got %q", req.AIAssistantID)
		}
		if req.MachineDetection != "Enable" {
			t.Errorf("MachineDetection: got %q", req.MachineDetection)
		}

		// Return success
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(telnyxCallAPIResponse{
			Data: OutboundCallResponse{
				CallControlID: "cc_123",
				CallLegID:     "cl_456",
				CallSessionID: "cs_789",
				IsAlive:       true,
			},
		})
	}))
	defer server.Close()

	client, err := NewTelnyxVoiceClient(TelnyxVoiceClientConfig{
		APIKey:     "test_key",
		TexmlAppID: "app_test",
		BaseURL:    server.URL,
	})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	resp, err := client.InitiateCallback(context.Background(), OutboundCallRequest{
		From:          "+15559876543",
		To:            "+15551234567",
		AIAssistantID: "ast_test",
	})
	if err != nil {
		t.Fatalf("InitiateCallback: %v", err)
	}
	if resp.CallControlID != "cc_123" {
		t.Errorf("CallControlID: got %q", resp.CallControlID)
	}
	if !resp.IsAlive {
		t.Error("expected IsAlive=true")
	}
}

func TestInitiateCallback_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"errors":[{"title":"Unauthorized"}]}`))
	}))
	defer server.Close()

	client, _ := NewTelnyxVoiceClient(TelnyxVoiceClientConfig{
		APIKey:     "bad_key",
		TexmlAppID: "app_test",
		BaseURL:    server.URL,
	})

	_, err := client.InitiateCallback(context.Background(), OutboundCallRequest{
		From:          "+15559876543",
		To:            "+15551234567",
		AIAssistantID: "ast_test",
	})
	if err == nil {
		t.Error("expected error for 401")
	}
}

func TestOutboundCallRequest_JSON(t *testing.T) {
	req := OutboundCallRequest{
		From:             "+15559876543",
		To:               "+15551234567",
		AIAssistantID:    "ast_abc",
		MachineDetection: "Enable",
		AsyncAmd:         true,
		DetectionMode:    "Premium",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify Telnyx expected field names (PascalCase)
	var raw map[string]any
	json.Unmarshal(data, &raw)

	if _, ok := raw["From"]; !ok {
		t.Error("missing 'From' field")
	}
	if _, ok := raw["To"]; !ok {
		t.Error("missing 'To' field")
	}
	if _, ok := raw["AIAssistantId"]; !ok {
		t.Error("missing 'AIAssistantId' field")
	}
}
