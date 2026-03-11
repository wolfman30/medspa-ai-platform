// Package nextech implements the emr.Client interface for Nextech EMR
// using FHIR-based REST APIs and OAuth 2.0 client credentials authentication.
package nextech

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client implements the emr.Client interface for Nextech EMR.
type Client struct {
	baseURL      string
	clientID     string
	clientSecret string
	httpClient   *http.Client

	// OAuth 2.0 token management
	accessToken string
	tokenExpiry time.Time
}

// Config holds configuration for the Nextech client.
type Config struct {
	BaseURL      string        // e.g., "https://api.nextech.com" or sandbox URL
	ClientID     string        // OAuth 2.0 client ID
	ClientSecret string        // OAuth 2.0 client secret
	Timeout      time.Duration // HTTP client timeout; defaults to 30s
}

// defaultHTTPTimeout is the default timeout for the underlying HTTP client.
const defaultHTTPTimeout = 30 * time.Second

// tokenExpiryBuffer is subtracted from the token expiry to ensure
// we refresh before the token actually expires.
const tokenExpiryBuffer = 5 * time.Minute

// New creates a new Nextech EMR client.
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("nextech: BaseURL is required")
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("nextech: ClientID is required")
	}
	if cfg.ClientSecret == "" {
		return nil, fmt.Errorf("nextech: ClientSecret is required")
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultHTTPTimeout
	}

	client := &Client{
		baseURL:      strings.TrimSuffix(cfg.BaseURL, "/"),
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}

	return client, nil
}

// ensureAuthenticated ensures we have a valid access token.
func (c *Client) ensureAuthenticated(ctx context.Context) error {
	if c.accessToken != "" && time.Now().Add(tokenExpiryBuffer).Before(c.tokenExpiry) {
		return nil
	}
	return c.authenticate(ctx)
}

// authenticate performs OAuth 2.0 client credentials authentication.
func (c *Client) authenticate(ctx context.Context) error {
	tokenURL := c.baseURL + "/connect/token"

	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", c.clientID)
	data.Set("client_secret", c.clientSecret)
	data.Set("scope", "patient/*.read patient/*.write appointment/*.read appointment/*.write slot/*.read")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("nextech: failed to create auth request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nextech: auth request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("nextech: auth failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("nextech: failed to decode auth response: %w", err)
	}

	c.accessToken = tokenResp.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	return nil
}
