package payments

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

type orderMetadataFetcher interface {
	FetchMetadata(ctx context.Context, orderID string) (map[string]string, error)
}

type squareOrdersClient struct {
	accessToken string
	baseURL     string
	httpClient  *http.Client
	logger      *logging.Logger
}

func NewSquareOrdersClient(accessToken, baseURL string, logger *logging.Logger) *squareOrdersClient {
	if logger == nil {
		logger = logging.Default()
	}
	if baseURL == "" {
		baseURL = "https://connect.squareup.com"
	}
	return &squareOrdersClient{
		accessToken: accessToken,
		baseURL:     strings.TrimRight(baseURL, "/"),
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		logger:      logger,
	}
}

func (c *squareOrdersClient) FetchMetadata(ctx context.Context, orderID string) (map[string]string, error) {
	if c == nil || c.accessToken == "" || orderID == "" {
		return nil, fmt.Errorf("square orders: missing token or order id")
	}
	url := fmt.Sprintf("%s/v2/orders/%s", c.baseURL, orderID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("square orders: request build: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("square orders: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusMultipleChoices {
		var body struct {
			Errors []struct {
				Detail string `json:"detail"`
			} `json:"errors"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return nil, fmt.Errorf("square orders: status %d: %+v", resp.StatusCode, body.Errors)
	}

	var payload struct {
		Order struct {
			Metadata map[string]string `json:"metadata"`
		} `json:"order"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("square orders: decode: %w", err)
	}
	return payload.Order.Metadata, nil
}

