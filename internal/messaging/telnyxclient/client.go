package telnyxclient

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"log/slog"
)

const (
	defaultBaseURL   = "https://api.telnyx.com/v2"
	defaultUserAgent = "medspa-messaging-acl/0.1"
)

// Config controls how the Telnyx client behaves.
type Config struct {
	BaseURL       string
	APIKey        string
	WebhookSecret string
	Timeout       time.Duration
	MaxRetries    int
	Backoff       time.Duration
	MaxSkew       time.Duration
	HTTPClient    *http.Client
	Logger        *slog.Logger
	UserAgent     string
}

// Client wraps Telnyx REST endpoints relevant to the messaging ACL.
type Client struct {
	apiKey        string
	baseURL       string
	webhookSecret string
	httpClient    *http.Client
	maxRetries    int
	backoff       time.Duration
	maxSkew       time.Duration
	logger        *slog.Logger
	userAgent     string
}

// New creates a configured Client with sane defaults.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("telnyxclient: API key is required")
	}
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		httpClient = &http.Client{Timeout: timeout}
	}
	maxRetries := cfg.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	backoff := cfg.Backoff
	if backoff <= 0 {
		backoff = 250 * time.Millisecond
	}
	maxSkew := cfg.MaxSkew
	if maxSkew <= 0 {
		maxSkew = 5 * time.Minute
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	userAgent := strings.TrimSpace(cfg.UserAgent)
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	return &Client{
		apiKey:        cfg.APIKey,
		baseURL:       baseURL,
		webhookSecret: cfg.WebhookSecret,
		httpClient:    httpClient,
		maxRetries:    maxRetries,
		backoff:       backoff,
		maxSkew:       maxSkew,
		logger:        logger,
		userAgent:     userAgent,
	}, nil
}

// SendMessage triggers an SMS/MMS send request.
func (c *Client) SendMessage(ctx context.Context, req SendMessageRequest) (*MessageResponse, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(struct {
		From               string   `json:"from"`
		To                 string   `json:"to"`
		Text               string   `json:"text"`
		MediaURLs          []string `json:"media_urls,omitempty"`
		MessagingProfileID string   `json:"messaging_profile_id,omitempty"`
	}{
		From:               req.From,
		To:                 req.To,
		Text:               req.Body,
		MediaURLs:          req.MediaURLs,
		MessagingProfileID: req.MessagingProfileID,
	})
	if err != nil {
		return nil, fmt.Errorf("telnyxclient: marshal send body: %w", err)
	}
	data, err := c.invoke(ctx, http.MethodPost, "/messages", nil, body, "application/json")
	if err != nil {
		return nil, err
	}
	return decodeDataWrapper[MessageResponse](data)
}

// CheckHostedEligibility verifies whether a number can be hosted for SMS/MMS.
func (c *Client) CheckHostedEligibility(ctx context.Context, number string) (*HostedEligibilityResponse, error) {
	if strings.TrimSpace(number) == "" {
		return nil, errors.New("telnyxclient: phone number required")
	}
	q := url.Values{}
	q.Set("phone_number", number)
	data, err := c.invoke(ctx, http.MethodGet, "/hosted_messaging/eligibility", q, nil, "")
	if err != nil {
		return nil, err
	}
	return decodeDataWrapper[HostedEligibilityResponse](data)
}

// CreateHostedOrder starts the hosted messaging onboarding process.
func (c *Client) CreateHostedOrder(ctx context.Context, req HostedOrderRequest) (*HostedOrder, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("telnyxclient: marshal hosted order request: %w", err)
	}
	data, err := c.invoke(ctx, http.MethodPost, "/hosted_messaging/orders", nil, body, "application/json")
	if err != nil {
		return nil, err
	}
	return decodeDataWrapper[HostedOrder](data)
}

// SubmitOwnershipVerification submits the verification code back to Telnyx.
func (c *Client) SubmitOwnershipVerification(ctx context.Context, orderID, code string) error {
	if strings.TrimSpace(orderID) == "" || strings.TrimSpace(code) == "" {
		return errors.New("telnyxclient: order id and code required")
	}
	body, err := json.Marshal(map[string]string{"verification_code": code})
	if err != nil {
		return fmt.Errorf("telnyxclient: marshal verification payload: %w", err)
	}
	_, err = c.invoke(ctx, http.MethodPost, fmt.Sprintf("/hosted_messaging/orders/%s/actions/verify", orderID), nil, body, "application/json")
	return err
}

// UploadDocument attaches LOA/invoice proof to a hosted messaging order.
func (c *Client) UploadDocument(ctx context.Context, orderID string, docType DocumentType, fileName string, r io.Reader) error {
	if strings.TrimSpace(orderID) == "" {
		return errors.New("telnyxclient: order id required")
	}
	if docType == "" {
		return errors.New("telnyxclient: document type required")
	}
	if r == nil {
		return errors.New("telnyxclient: document reader required")
	}
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.WriteField("document_type", string(docType)); err != nil {
		return fmt.Errorf("telnyxclient: write field: %w", err)
	}
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return fmt.Errorf("telnyxclient: create form file: %w", err)
	}
	if _, err := io.Copy(part, r); err != nil {
		return fmt.Errorf("telnyxclient: copy document: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("telnyxclient: close multipart writer: %w", err)
	}
	_, err = c.invoke(ctx, http.MethodPost, fmt.Sprintf("/hosted_messaging/orders/%s/documents", orderID), nil, buf.Bytes(), writer.FormDataContentType())
	return err
}

// GetHostedOrder fetches the latest status for a hosted messaging order.
func (c *Client) GetHostedOrder(ctx context.Context, orderID string) (*HostedOrder, error) {
	if strings.TrimSpace(orderID) == "" {
		return nil, errors.New("telnyxclient: order id required")
	}
	data, err := c.invoke(ctx, http.MethodGet, fmt.Sprintf("/hosted_messaging/orders/%s", orderID), nil, nil, "")
	if err != nil {
		return nil, err
	}
	return decodeDataWrapper[HostedOrder](data)
}

// CreateBrand registers a 10DLC brand.
func (c *Client) CreateBrand(ctx context.Context, req BrandRequest) (*Brand, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("telnyxclient: marshal brand payload: %w", err)
	}
	data, err := c.invoke(ctx, http.MethodPost, "/10dlc/brands", nil, body, "application/json")
	if err != nil {
		return nil, err
	}
	return decodeDataWrapper[Brand](data)
}

// CreateCampaign registers a 10DLC campaign under a brand.
func (c *Client) CreateCampaign(ctx context.Context, req CampaignRequest) (*Campaign, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("telnyxclient: marshal campaign payload: %w", err)
	}
	data, err := c.invoke(ctx, http.MethodPost, "/10dlc/campaigns", nil, body, "application/json")
	if err != nil {
		return nil, err
	}
	return decodeDataWrapper[Campaign](data)
}

// VerifyWebhookSignature validates Telnyx webhook signatures.
func (c *Client) VerifyWebhookSignature(timestamp, signature string, payload []byte) error {
	if c.webhookSecret == "" {
		return errors.New("telnyxclient: webhook secret not configured")
	}
	ts := strings.TrimSpace(timestamp)
	if ts == "" {
		return errors.New("telnyxclient: missing signature timestamp")
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return fmt.Errorf("telnyxclient: invalid signature timestamp: %w", err)
	}
	sentAt := time.Unix(sec, 0)
	if diff := time.Since(sentAt); diff > c.maxSkew || diff < -c.maxSkew {
		return fmt.Errorf("telnyxclient: signature timestamp skew %s exceeds limit", diff)
	}
	unsigned := ts + "." + string(payload)
	mac := hmac.New(sha256.New, []byte(c.webhookSecret))
	mac.Write([]byte(unsigned))
	expected := hex.EncodeToString(mac.Sum(nil))
	actual := strings.ToLower(strings.TrimSpace(signature))
	if actual == "" {
		return errors.New("telnyxclient: missing signature header")
	}
	if !hmac.Equal([]byte(expected), []byte(actual)) {
		return errors.New("telnyxclient: signature mismatch")
	}
	return nil
}

func (c *Client) invoke(ctx context.Context, method, path string, query url.Values, body []byte, contentType string) ([]byte, error) {
	fullURL := c.buildURL(path, query)
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("telnyxclient: build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("User-Agent", c.userAgent)
		if body != nil {
			ct := contentType
			if ct == "" {
				ct = "application/json"
			}
			req.Header.Set("Content-Type", ct)
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if !shouldRetry(0, err) || attempt == c.maxRetries {
				return nil, fmt.Errorf("telnyxclient: http error: %w", err)
			}
			lastErr = err
			c.logRetry(path, attempt, 0, err)
			if sleepErr := c.sleep(ctx, attempt); sleepErr != nil {
				return nil, sleepErr
			}
			continue
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("telnyxclient: read response: %w", readErr)
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return data, nil
		}
		apiErr := decodeAPIError(resp.StatusCode, data)
		if attempt < c.maxRetries && shouldRetry(resp.StatusCode, nil) {
			lastErr = apiErr
			c.logRetry(path, attempt, resp.StatusCode, apiErr)
			if sleepErr := c.sleep(ctx, attempt); sleepErr != nil {
				return nil, sleepErr
			}
			continue
		}
		return nil, apiErr
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("telnyxclient: request failed without response")
}

func (c *Client) buildURL(path string, query url.Values) string {
	trimmedPath := "/" + strings.TrimLeft(path, "/")
	full := c.baseURL + trimmedPath
	if len(query) > 0 {
		full = full + "?" + query.Encode()
	}
	return full
}

func (c *Client) sleep(ctx context.Context, attempt int) error {
	delay := c.backoff * time.Duration(1<<attempt)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) logRetry(path string, attempt int, status int, err error) {
	if c.logger == nil {
		return
	}
	c.logger.Warn("telnyx retry",
		"path", path,
		"attempt", attempt+1,
		"status", status,
		"error", err,
	)
}

func shouldRetry(status int, err error) bool {
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return true
		}
		return !errors.Is(err, context.Canceled)
	}
	if status == http.StatusTooManyRequests {
		return true
	}
	if status >= 500 && status <= 599 {
		return true
	}
	return false
}

type apiError struct {
	StatusCode int             `json:"-"`
	Type       string          `json:"type,omitempty"`
	Title      string          `json:"title,omitempty"`
	Detail     string          `json:"detail,omitempty"`
	Errors     json.RawMessage `json:"errors,omitempty"`
}

func (e *apiError) Error() string {
	if e.Title != "" {
		return fmt.Sprintf("telnyxclient: %s (status=%d)", e.Title, e.StatusCode)
	}
	if e.Detail != "" {
		return fmt.Sprintf("telnyxclient: %s (status=%d)", e.Detail, e.StatusCode)
	}
	return fmt.Sprintf("telnyxclient: http status %d", e.StatusCode)
}

func decodeAPIError(status int, body []byte) error {
	var parsed apiError
	if err := json.Unmarshal(body, &parsed); err != nil {
		return &apiError{StatusCode: status, Detail: string(body)}
	}
	parsed.StatusCode = status
	return &parsed
}

func decodeDataWrapper[T any](body []byte) (*T, error) {
	var wrapper struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("telnyxclient: decode response: %w", err)
	}
	return &wrapper.Data, nil
}
