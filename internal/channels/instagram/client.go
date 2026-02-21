package instagram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	defaultGraphAPIBase = "https://graph.facebook.com/v18.0"
	defaultHTTPTimeout  = 10 * time.Second
)

// Client sends messages via the Instagram/Meta Graph API.
type Client struct {
	pageAccessToken string
	graphAPIBase    string
	httpClient      *http.Client
}

// NewClient creates a new Graph API client.
func NewClient(pageAccessToken string) *Client {
	return &Client{
		pageAccessToken: pageAccessToken,
		graphAPIBase:    defaultGraphAPIBase,
		httpClient:      &http.Client{Timeout: defaultHTTPTimeout},
	}
}

// SetGraphAPIBase overrides the Graph API base URL (useful for testing).
func (c *Client) SetGraphAPIBase(base string) {
	c.graphAPIBase = base
}

// SendTextMessage sends a plain text message to the given recipient.
func (c *Client) SendTextMessage(ctx context.Context, recipientID, text string) (*SendResponse, error) {
	req := SendRequest{
		Recipient: SendRecipient{ID: recipientID},
		Message:   SendMessage{Text: text},
	}
	return c.send(ctx, req)
}

// SendButtonMessage sends a button template message.
func (c *Client) SendButtonMessage(ctx context.Context, recipientID, text string, buttons []Button) (*SendResponse, error) {
	req := SendRequest{
		Recipient: SendRecipient{ID: recipientID},
		Message: SendMessage{
			Attachment: &Attachment{
				Type: "template",
				Payload: Payload{
					TemplateType: "button",
					Text:         text,
					Buttons:      buttons,
				},
			},
		},
	}
	return c.send(ctx, req)
}

func (c *Client) send(ctx context.Context, req SendRequest) (*SendResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("instagram: marshal send request: %w", err)
	}

	url := fmt.Sprintf("%s/me/messages?access_token=%s", c.graphAPIBase, c.pageAccessToken)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("instagram: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("instagram: send message: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("instagram: read response: %w", err)
	}

	var sendResp SendResponse
	if err := json.Unmarshal(respBody, &sendResp); err != nil {
		return nil, fmt.Errorf("instagram: unmarshal response: %w", err)
	}

	if sendResp.Error != nil {
		return &sendResp, fmt.Errorf("instagram: API error %d: %s", sendResp.Error.Code, sendResp.Error.Message)
	}

	if resp.StatusCode != http.StatusOK {
		return &sendResp, fmt.Errorf("instagram: unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	return &sendResp, nil
}
