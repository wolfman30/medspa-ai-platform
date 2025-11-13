package langchain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// Config describes how to reach the LangChain orchestrator.
type Config struct {
	BaseURL string
	APIKey  string
	Timeout time.Duration
}

// Client proxies conversation + RAG requests to the orchestrator service.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewClient validates the configuration and returns a ready-to-use client.
func NewClient(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("langchain: base URL required")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		http: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

// GenerateMetadata captures request-scoped identifiers that the orchestrator can use.
type GenerateMetadata struct {
	ConversationID string
	ClinicID       string
	OrgID          string
	LeadID         string
	Channel        string
	Metadata       map[string]string
}

// GenerateRequest shares the full conversation context with the orchestrator.
type GenerateRequest struct {
	Metadata    GenerateMetadata
	History     []openai.ChatCompletionMessage
	LatestInput string
}

// GenerateResponse is a simplified representation of the orchestrator result.
type GenerateResponse struct {
	Message   string   `json:"message"`
	Contexts  []string `json:"contexts,omitempty"`
	LatencyMS int64    `json:"latency_ms,omitempty"`
}

// Generate sends the conversation history to the orchestrator and returns the reply.
func (c *Client) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	if req.Metadata.ClinicID == "" {
		return nil, errors.New("langchain: clinic id required")
	}
	if len(req.History) == 0 {
		return nil, errors.New("langchain: history required")
	}

	payload := map[string]any{
		"conversation_id": req.Metadata.ConversationID,
		"clinic_id":       req.Metadata.ClinicID,
		"org_id":          req.Metadata.OrgID,
		"lead_id":         req.Metadata.LeadID,
		"channel":         req.Metadata.Channel,
		"metadata":        req.Metadata.Metadata,
		"latest_input":    req.LatestInput,
		"history":         toWireMessages(req.History),
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/v1/conversations/respond", payload)
	if err != nil {
		return nil, err
	}

	var out GenerateResponse
	if err := json.Unmarshal(resp, &out); err != nil {
		return nil, fmt.Errorf("langchain: decode response failed: %w", err)
	}
	return &out, nil
}

// AddKnowledge enqueues documents for Astra DB ingestion.
func (c *Client) AddKnowledge(ctx context.Context, clinicID string, docs []string) error {
	if clinicID == "" {
		return errors.New("langchain: clinic id required")
	}
	if len(docs) == 0 {
		return nil
	}
	payload := map[string]any{
		"documents": docs,
	}
	_, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/v1/knowledge/%s", clinicID), payload)
	return err
}

func (c *Client) doRequest(ctx context.Context, method, path string, payload any) ([]byte, error) {
	var body *bytes.Buffer
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("langchain: failed to encode payload: %w", err)
		}
		body = bytes.NewBuffer(data)
	} else {
		body = bytes.NewBuffer(nil)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("langchain: request build failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(c.apiKey) != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("langchain: request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("langchain: read response failed: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("langchain: %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	return data, nil
}

type wireMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func toWireMessages(history []openai.ChatCompletionMessage) []wireMessage {
	out := make([]wireMessage, 0, len(history))
	for _, msg := range history {
		out = append(out, wireMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}
	return out
}
