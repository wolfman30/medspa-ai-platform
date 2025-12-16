package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

type config struct {
	upstreamBaseURL string
	upstreamTimeout time.Duration
}

func loadConfig() (config, error) {
	baseURL := strings.TrimSpace(os.Getenv("UPSTREAM_BASE_URL"))
	if baseURL == "" {
		return config{}, errors.New("UPSTREAM_BASE_URL is required")
	}

	timeout := 5 * time.Second
	if raw := strings.TrimSpace(os.Getenv("UPSTREAM_TIMEOUT")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return config{}, fmt.Errorf("invalid UPSTREAM_TIMEOUT: %w", err)
		}
		timeout = parsed
	}

	return config{
		upstreamBaseURL: strings.TrimRight(baseURL, "/"),
		upstreamTimeout: timeout,
	}, nil
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		panic(err)
	}

	client := &http.Client{Timeout: cfg.upstreamTimeout}
	lambda.Start(func(ctx context.Context, evt events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
		return handle(ctx, cfg, client, evt)
	})
}

func handle(ctx context.Context, cfg config, client *http.Client, evt events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	method := strings.ToUpper(strings.TrimSpace(evt.RequestContext.HTTP.Method))
	path := strings.TrimSpace(evt.RawPath)
	if path == "" {
		path = strings.TrimSpace(evt.RequestContext.HTTP.Path)
	}

	if path == "/health" || path == "/_health" {
		return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusOK, Body: "ok"}, nil
	}

	if method != http.MethodPost {
		return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusMethodNotAllowed}, nil
	}

	switch path {
	case "/webhooks/twilio/voice", "/webhooks/telnyx/voice":
	default:
		return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusNotFound}, nil
	}

	body, err := decodeBody(evt)
	if err != nil {
		return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusBadRequest, Body: "invalid body"}, nil
	}

	upstreamURL := cfg.upstreamBaseURL + path
	if qs := strings.TrimSpace(evt.RawQueryString); qs != "" {
		upstreamURL += "?" + qs
	}

	reqCtx, cancel := context.WithTimeout(ctx, cfg.upstreamTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusInternalServerError}, nil
	}

	if ct := headerValue(evt.Headers, "content-type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}

	// Preserve provider signature headers so the upstream API can validate them.
	copyHeader(req.Header, evt.Headers, "x-twilio-signature")
	copyHeader(req.Header, evt.Headers, "telnyx-timestamp")
	copyHeader(req.Header, evt.Headers, "telnyx-signature")

	// Preserve the original public URL host/proto for Twilio signature validation.
	originalHost := strings.TrimSpace(evt.RequestContext.DomainName)
	if originalHost == "" {
		originalHost = strings.TrimSpace(headerValue(evt.Headers, "host"))
	}
	originalProto := strings.TrimSpace(headerValue(evt.Headers, "x-forwarded-proto"))
	if originalProto == "" {
		originalProto = "https"
	}
	if originalHost != "" {
		req.Header.Set("X-Forwarded-Host", originalHost)
	}
	if originalProto != "" {
		req.Header.Set("X-Forwarded-Proto", originalProto)
	}

	resp, err := client.Do(req)
	if err != nil {
		return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusBadGateway, Body: "upstream error"}, nil
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	out := events.APIGatewayV2HTTPResponse{
		StatusCode: resp.StatusCode,
		Body:       string(respBody),
		Headers:    map[string]string{},
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		out.Headers["content-type"] = ct
	}
	return out, nil
}

func decodeBody(evt events.APIGatewayV2HTTPRequest) ([]byte, error) {
	if !evt.IsBase64Encoded {
		return []byte(evt.Body), nil
	}
	decoded, err := base64.StdEncoding.DecodeString(evt.Body)
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

func headerValue(headers map[string]string, key string) string {
	for k, v := range headers {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

func copyHeader(dst http.Header, src map[string]string, header string) {
	if value := strings.TrimSpace(headerValue(src, header)); value != "" {
		dst.Set(header, value)
	}
}
