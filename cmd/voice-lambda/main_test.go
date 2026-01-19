package main

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
)

func TestHandleHealth(t *testing.T) {
	cfg := config{upstreamBaseURL: "http://example.com", upstreamTimeout: time.Second}
	client := &http.Client{Timeout: time.Second}

	evt := events.APIGatewayV2HTTPRequest{
		RawPath: "/health",
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
				Method: http.MethodGet,
				Path:   "/health",
			},
		},
	}

	resp, err := handle(context.Background(), cfg, client, evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	if resp.Body != "ok" {
		t.Fatalf("expected ok body, got %q", resp.Body)
	}
}

func TestHandleRejectsNonPost(t *testing.T) {
	cfg := config{upstreamBaseURL: "http://example.com", upstreamTimeout: time.Second}
	client := &http.Client{Timeout: time.Second}

	evt := events.APIGatewayV2HTTPRequest{
		RawPath: "/webhooks/telnyx/voice",
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
				Method: http.MethodGet,
				Path:   "/webhooks/telnyx/voice",
			},
		},
	}

	resp, err := handle(context.Background(), cfg, client, evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d", http.StatusMethodNotAllowed, resp.StatusCode)
	}
}

func TestHandleRejectsUnknownPath(t *testing.T) {
	cfg := config{upstreamBaseURL: "http://example.com", upstreamTimeout: time.Second}
	client := &http.Client{Timeout: time.Second}

	evt := events.APIGatewayV2HTTPRequest{
		RawPath: "/webhooks/unknown",
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
				Method: http.MethodPost,
				Path:   "/webhooks/unknown",
			},
		},
	}

	resp, err := handle(context.Background(), cfg, client, evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, resp.StatusCode)
	}
}

func TestHandleInvalidBase64Body(t *testing.T) {
	cfg := config{upstreamBaseURL: "http://example.com", upstreamTimeout: time.Second}
	client := &http.Client{Timeout: time.Second}

	evt := events.APIGatewayV2HTTPRequest{
		RawPath:         "/webhooks/telnyx/voice",
		Body:            "not-base64",
		IsBase64Encoded: true,
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
				Method: http.MethodPost,
				Path:   "/webhooks/telnyx/voice",
			},
		},
	}

	resp, err := handle(context.Background(), cfg, client, evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.StatusCode)
	}
	if resp.Body != "invalid body" {
		t.Fatalf("expected invalid body response, got %q", resp.Body)
	}
}

func TestHandleForwardsVoiceWebhook(t *testing.T) {
	type captured struct {
		method  string
		path    string
		query   string
		headers http.Header
		body    string
	}
	reqCh := make(chan captured, 1)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		reqCh <- captured{
			method:  r.Method,
			path:    r.URL.Path,
			query:   r.URL.RawQuery,
			headers: r.Header.Clone(),
			body:    string(body),
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("<ok/>"))
	}))
	defer upstream.Close()

	client := upstream.Client()
	client.Timeout = time.Second
	cfg := config{upstreamBaseURL: upstream.URL, upstreamTimeout: time.Second}

	evt := events.APIGatewayV2HTTPRequest{
		RawPath:         "/webhooks/telnyx/voice",
		RawQueryString:  "foo=bar",
		Body:            "payload",
		IsBase64Encoded: false,
		Headers: map[string]string{
			"content-type":      "application/x-www-form-urlencoded",
			"telnyx-signature":  "sig",
			"telnyx-timestamp":  "ts",
			"x-forwarded-proto": "http",
		},
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			DomainName: "voice.example.com",
			HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
				Method: http.MethodPost,
				Path:   "/webhooks/telnyx/voice",
			},
		},
	}

	resp, err := handle(context.Background(), cfg, client, evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, resp.StatusCode)
	}
	if resp.Body != "<ok/>" {
		t.Fatalf("expected upstream body, got %q", resp.Body)
	}
	if ct := resp.Headers["content-type"]; ct != "application/xml" {
		t.Fatalf("expected content-type to be forwarded, got %q", ct)
	}

	select {
	case got := <-reqCh:
		if got.method != http.MethodPost {
			t.Fatalf("expected method POST, got %s", got.method)
		}
		if got.path != "/webhooks/telnyx/voice" {
			t.Fatalf("expected path /webhooks/telnyx/voice, got %s", got.path)
		}
		if got.query != "foo=bar" {
			t.Fatalf("expected query foo=bar, got %s", got.query)
		}
		if got.body != "payload" {
			t.Fatalf("expected body payload, got %q", got.body)
		}
		if got.headers.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Fatalf("expected content type to be forwarded, got %q", got.headers.Get("Content-Type"))
		}
		if got.headers.Get("Telnyx-Signature") != "sig" {
			t.Fatalf("expected telnyx signature to be forwarded, got %q", got.headers.Get("Telnyx-Signature"))
		}
		if got.headers.Get("Telnyx-Timestamp") != "ts" {
			t.Fatalf("expected telnyx timestamp to be forwarded, got %q", got.headers.Get("Telnyx-Timestamp"))
		}
		if got.headers.Get("X-Forwarded-Host") != "voice.example.com" {
			t.Fatalf("expected forwarded host, got %q", got.headers.Get("X-Forwarded-Host"))
		}
		if got.headers.Get("X-Forwarded-Proto") != "http" {
			t.Fatalf("expected forwarded proto, got %q", got.headers.Get("X-Forwarded-Proto"))
		}
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for upstream request")
	}
}

func TestDecodeBodyBase64(t *testing.T) {
	raw := []byte("hello")
	evt := events.APIGatewayV2HTTPRequest{
		Body:            base64.StdEncoding.EncodeToString(raw),
		IsBase64Encoded: true,
	}

	decoded, err := decodeBody(evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(decoded) != "hello" {
		t.Fatalf("expected decoded body, got %q", string(decoded))
	}
}
