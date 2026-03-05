// Package moxie provides a GraphQL client for the Moxie booking platform API.
// It supports querying provider availability and creating appointments through
// Moxie's public-facing GraphQL endpoint.
package moxie

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

const (
	// defaultGraphQLEndpoint is the production Moxie GraphQL API URL.
	defaultGraphQLEndpoint = "https://graphql.joinmoxie.com/v1/graphql"
	// defaultTimeout is the HTTP client timeout for API requests.
	defaultTimeout = 15 * time.Second
)

// Client is a Moxie GraphQL API client that handles HTTP transport,
// request serialization, and response parsing.
type Client struct {
	endpoint   string
	httpClient *http.Client
	logger     *logging.Logger
	dryRun     bool // When true, CreateAppointment logs but doesn't actually create
}

// Option is a functional option for configuring a Client.
type Option func(*Client)

// WithDryRun enables dry-run mode — CreateAppointment will log the request
// but return a fake success without calling Moxie's API.
func WithDryRun(dryRun bool) Option {
	return func(c *Client) {
		c.dryRun = dryRun
	}
}

// NewClient creates a new Moxie API client with the given logger and options.
func NewClient(logger *logging.Logger, opts ...Option) *Client {
	c := &Client{
		endpoint: defaultGraphQLEndpoint,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		logger: logger,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// graphqlRequest is the generic GraphQL request envelope sent to Moxie's API.
type graphqlRequest struct {
	OperationName string      `json:"operationName"`
	Variables     interface{} `json:"variables"`
	Query         string      `json:"query"`
}

// doRequest executes a GraphQL request against Moxie's API and unmarshals
// the JSON response into result.
func (c *Client) doRequest(ctx context.Context, operationName string, variables interface{}, query string, result interface{}) error {
	body := graphqlRequest{
		OperationName: operationName,
		Variables:     variables,
		Query:         query,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://app.joinmoxie.com")
	req.Header.Set("Referer", "https://app.joinmoxie.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("moxie API returned %d: %s", resp.StatusCode, string(respBody[:min(200, len(respBody))]))
	}

	if err := json.Unmarshal(respBody, result); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}

	return nil
}
