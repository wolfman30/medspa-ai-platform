package telnyxclient

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"log/slog"
	"path/filepath"
	"runtime"
)

func TestSendMessage(t *testing.T) {
	payload := mustLoadFixture(t, "send_message_success.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
			t.Fatalf("missing auth header")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if !strings.Contains(string(body), "\"text\"") {
			t.Fatalf("expected text field, got %s", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write(payload)
	}))
	defer server.Close()

	client := newTestClient(t, server, Config{})
	resp, err := client.SendMessage(context.Background(), SendMessageRequest{
		From: "+15553334444",
		To:   "+15552223333",
		Body: "hello patient",
	})
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	if resp.ID != "msg_01J123ABC" || resp.Status != "queued" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestNewClientDefaultsAndValidation(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatalf("expected api key validation error")
	}
	client, err := New(Config{APIKey: "key"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if client.baseURL != defaultBaseURL {
		t.Fatalf("expected default base url, got %s", client.baseURL)
	}
	if client.httpClient == nil || client.httpClient.Timeout != 10*time.Second {
		t.Fatalf("expected default timeout")
	}
	if client.maxRetries != 0 {
		t.Fatalf("expected retries to default to 0")
	}
	if client.logger == nil {
		t.Fatalf("expected default logger")
	}
}

func TestGetHostedOrder(t *testing.T) {
	payload := mustLoadFixture(t, "hosted_order_response.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method %s", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/hosted_messaging/orders/") {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(payload)
	}))
	defer server.Close()

	client := newTestClient(t, server, Config{})
	order, err := client.GetHostedOrder(context.Background(), "hno_01JHOSTED")
	if err != nil {
		t.Fatalf("get hosted order: %v", err)
	}
	if order.Status != "verifying" {
		t.Fatalf("unexpected order: %#v", order)
	}
}

func TestRetryOnServerError(t *testing.T) {
	var calls int32
	payload := mustLoadFixture(t, "send_message_success.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&calls, 1)
		if current == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"title":"server error"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write(payload)
	}))
	defer server.Close()

	client := newTestClient(t, server, Config{MaxRetries: 2, Backoff: 5 * time.Millisecond})
	if _, err := client.SendMessage(context.Background(), SendMessageRequest{
		From: "+100",
		To:   "+200",
		Body: "retry",
	}); err != nil {
		t.Fatalf("send message after retry: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 attempts, got %d", calls)
	}
}

func TestUploadDocumentMultipart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		docType := r.FormValue("document_type")
		if docType != string(DocumentTypeLOA) {
			t.Fatalf("unexpected document type %s", docType)
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("form file: %v", err)
		}
		data, _ := io.ReadAll(file)
		if string(data) != "hello" {
			t.Fatalf("unexpected file data %s", string(data))
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := newTestClient(t, server, Config{})
	if err := client.UploadDocument(context.Background(), "hno_1", DocumentTypeLOA, "loa.pdf", bytes.NewBufferString("hello")); err != nil {
		t.Fatalf("upload document: %v", err)
	}
}

func TestVerifyWebhookSignature(t *testing.T) {
	payload := mustLoadFixture(t, "webhook_event.json")
	secret := "topsecret"
	now := time.Now().UTC()
	ts := strconv.FormatInt(now.Unix(), 10)
	unsigned := ts + "." + string(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(unsigned))
	signature := hex.EncodeToString(mac.Sum(nil))

	client := newTestClient(t, nil, Config{WebhookSecret: secret, MaxSkew: time.Hour})
	if err := client.VerifyWebhookSignature(ts, signature, payload); err != nil {
		t.Fatalf("verify signature: %v", err)
	}
	if err := client.VerifyWebhookSignature("100", signature, payload); err == nil {
		t.Fatalf("expected skew error")
	}
}

func TestCheckHostedEligibility(t *testing.T) {
	var capturedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"phone_number":"+15551112222","eligible":true}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server, Config{})
	resp, err := client.CheckHostedEligibility(context.Background(), "+15551112222")
	if err != nil {
		t.Fatalf("check eligibility: %v", err)
	}
	if !resp.Eligible || !strings.Contains(capturedQuery, "phone_number=%2B15551112222") {
		t.Fatalf("unexpected eligibility response: %#v query=%s", resp, capturedQuery)
	}
	if _, err := client.CheckHostedEligibility(context.Background(), ""); err == nil {
		t.Fatalf("expected validation error for blank input")
	}
}

func TestCheckHostedEligibilityHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	client := newTestClient(t, server, Config{MaxRetries: 0})
	if _, err := client.CheckHostedEligibility(context.Background(), "+1555"); err == nil {
		t.Fatalf("expected http error")
	}
}

func TestCreateHostedOrder(t *testing.T) {
	payload := mustLoadFixture(t, "hosted_order_response.json")
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.Header().Set("Content-Type", "application/json")
		w.Write(payload)
	}))
	defer server.Close()

	client := newTestClient(t, server, Config{})
	order, err := client.CreateHostedOrder(context.Background(), HostedOrderRequest{
		ClinicID:          "clinic-1",
		PhoneNumber:       "+15550007777",
		BillingNumber:     "+15550001111",
		AuthorizedContact: "Alice",
		AuthorizedEmail:   "ops@example.com",
		AuthorizedPhone:   "+15556667777",
	})
	if err != nil {
		t.Fatalf("create hosted order: %v", err)
	}
	if order.ID != "hno_01JHOSTED" || !strings.Contains(body, "clinic-1") {
		t.Fatalf("unexpected order response: %#v body=%s", order, body)
	}
}

func TestSubmitOwnershipVerification(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestClient(t, server, Config{})
	if err := client.SubmitOwnershipVerification(context.Background(), "ord_1", "4321"); err != nil {
		t.Fatalf("submit verification: %v", err)
	}
	if !strings.Contains(body, "4321") {
		t.Fatalf("expected verification code payload")
	}
	if err := client.SubmitOwnershipVerification(context.Background(), "", ""); err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestCreateHostedOrderHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"detail":"try later"}`))
	}))
	defer server.Close()
	client := newTestClient(t, server, Config{MaxRetries: 0})
	_, err := client.CreateHostedOrder(context.Background(), HostedOrderRequest{
		ClinicID: "c", PhoneNumber: "+1", AuthorizedContact: "Alice",
	})
	if err == nil {
		t.Fatalf("expected http error")
	}
}

func TestCreateBrandAndCampaign(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/10dlc/brands":
			w.Write([]byte(`{"data":{"id":"brand_internal","brand_id":"B123","status":"approved","clinic_id":"clinic-1","created_at":"2024-10-01T00:00:00Z"}}`))
		case "/10dlc/campaigns":
			w.Write([]byte(`{"data":{"id":"cmp_internal","campaign_id":"C321","brand_id":"brand_internal","status":"active","use_case":"notifications","sample_messages":["hi"],"created_at":"2024-10-01T00:00:00Z"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server, Config{})
	brand, err := client.CreateBrand(context.Background(), BrandRequest{
		ClinicID:     "clinic-1",
		LegalName:    "Clinic",
		Website:      "https://clinic",
		AddressLine:  "1 Main",
		City:         "SF",
		State:        "CA",
		PostalCode:   "94105",
		Country:      "US",
		ContactName:  "Alice",
		ContactEmail: "ops@example.com",
		ContactPhone: "+15556667777",
		Vertical:     "healthcare",
	})
	if err != nil {
		t.Fatalf("create brand: %v", err)
	}
	if brand.BrandID != "B123" {
		t.Fatalf("unexpected brand: %#v", brand)
	}

	campaign, err := client.CreateCampaign(context.Background(), CampaignRequest{
		BrandID:        brand.ID,
		UseCase:        "notifications",
		SampleMessages: []string{"hello"},
		HelpMessage:    "reply HELP",
		StopMessage:    "reply STOP",
	})
	if err != nil {
		t.Fatalf("create campaign: %v", err)
	}
	if campaign.CampaignID != "C321" {
		t.Fatalf("unexpected campaign %#v", campaign)
	}
}

func TestCreateBrandHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("fail"))
	}))
	defer server.Close()
	client := newTestClient(t, server, Config{MaxRetries: 0})
	_, err := client.CreateBrand(context.Background(), BrandRequest{
		ClinicID:     "c",
		LegalName:    "Clinic",
		Website:      "https://clinic",
		AddressLine:  "1",
		City:         "SF",
		State:        "CA",
		PostalCode:   "94105",
		Country:      "US",
		ContactName:  "Alice",
		ContactEmail: "ops@example.com",
		ContactPhone: "+1555",
	})
	if err == nil {
		t.Fatalf("expected brand http error")
	}
}

func TestCreateCampaignHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("fail"))
	}))
	defer server.Close()
	client := newTestClient(t, server, Config{MaxRetries: 0})
	_, err := client.CreateCampaign(context.Background(), CampaignRequest{
		BrandID:        "b",
		UseCase:        "alerts",
		SampleMessages: []string{"hi"},
		HelpMessage:    "help",
		StopMessage:    "stop",
	})
	if err == nil {
		t.Fatalf("expected campaign http error")
	}
}

func TestUploadDocumentHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	client := newTestClient(t, server, Config{MaxRetries: 0})
	if err := client.UploadDocument(context.Background(), "ord_1", DocumentTypeLOA, "loa.pdf", strings.NewReader("data")); err == nil {
		t.Fatalf("expected upload error")
	}
}

func TestUploadDocumentCopyError(t *testing.T) {
	client := newTestClient(t, nil, Config{})
	if err := client.UploadDocument(context.Background(), "ord_1", DocumentTypeLOA, "loa.pdf", failingReader{}); err == nil {
		t.Fatalf("expected copy error")
	}
}

func TestPayloadValidationErrors(t *testing.T) {
	if err := (SendMessageRequest{}).validate(); err == nil {
		t.Fatalf("expected send validation error")
	}
	if err := (SendMessageRequest{From: "+1", To: "+2"}).validate(); err == nil {
		t.Fatalf("expected body/media validation error")
	}
	validSend := SendMessageRequest{From: "+1", To: "+2", MediaURLs: []string{"https://example.com/1.jpg"}}
	if err := validSend.validate(); err != nil {
		t.Fatalf("unexpected send validation failure: %v", err)
	}
	if err := (HostedOrderRequest{}).validate(); err == nil {
		t.Fatalf("expected hosted order validation error")
	}
	if err := (HostedOrderRequest{ClinicID: "c"}).validate(); err == nil {
		t.Fatalf("expected phone validation error")
	}
	if err := (HostedOrderRequest{ClinicID: "c", PhoneNumber: "+1"}).validate(); err == nil {
		t.Fatalf("expected contact validation error")
	}
	if err := (BrandRequest{}).validate(); err == nil {
		t.Fatalf("expected brand validation error")
	}
	if err := (BrandRequest{ClinicID: "c"}).validate(); err == nil {
		t.Fatalf("expected brand legal name validation error")
	}
	if err := (BrandRequest{ClinicID: "c", LegalName: "Clinic"}).validate(); err == nil {
		t.Fatalf("expected brand website validation error")
	}
	if err := (BrandRequest{ClinicID: "c", LegalName: "Clinic", Website: "https://clinic", AddressLine: "1", City: "SF", State: "CA", PostalCode: "94105"}).validate(); err == nil {
		t.Fatalf("expected brand contact validation error")
	}
	validBrand := BrandRequest{
		ClinicID:     "c",
		LegalName:    "Clinic",
		Website:      "https://clinic",
		AddressLine:  "1",
		City:         "SF",
		State:        "CA",
		PostalCode:   "94105",
		Country:      "US",
		ContactName:  "Alice",
		ContactEmail: "ops@example.com",
		ContactPhone: "+1555",
		Vertical:     "healthcare",
	}
	if err := validBrand.validate(); err != nil {
		t.Fatalf("expected valid brand request, got %v", err)
	}
	if err := (CampaignRequest{}).validate(); err == nil {
		t.Fatalf("expected campaign validation error")
	}
	if err := (CampaignRequest{BrandID: "b"}).validate(); err == nil {
		t.Fatalf("expected use case validation error")
	}
	if err := (CampaignRequest{BrandID: "b", UseCase: "alerts"}).validate(); err == nil {
		t.Fatalf("expected sample messages validation error")
	}
	if err := (CampaignRequest{BrandID: "b", UseCase: "alerts", SampleMessages: []string{"hi"}}).validate(); err == nil {
		t.Fatalf("expected help/stop validation error")
	}
	if err := (CampaignRequest{BrandID: "b", UseCase: "alerts", SampleMessages: []string{"hi"}, HelpMessage: "help"}).validate(); err == nil {
		t.Fatalf("expected stop message validation error")
	}
	validCampaign := CampaignRequest{
		BrandID:        "b",
		UseCase:        "alerts",
		SampleMessages: []string{"hi"},
		HelpMessage:    "reply HELP",
		StopMessage:    "reply STOP",
	}
	if err := validCampaign.validate(); err != nil {
		t.Fatalf("expected valid campaign request, got %v", err)
	}
}

func TestVerifyWebhookSignatureMismatch(t *testing.T) {
	client := newTestClient(t, nil, Config{WebhookSecret: "secret"})
	if err := client.VerifyWebhookSignature(strconv.FormatInt(time.Now().Unix(), 10), "deadbeef", []byte("{}")); err == nil {
		t.Fatalf("expected signature mismatch")
	}
}

func TestVerifyWebhookSignatureMissingTimestamp(t *testing.T) {
	client := newTestClient(t, nil, Config{WebhookSecret: "secret"})
	if err := client.VerifyWebhookSignature("", "abc", []byte("{}")); err == nil {
		t.Fatalf("expected timestamp error")
	}
}

func TestUploadDocumentValidationErrors(t *testing.T) {
	client := newTestClient(t, nil, Config{})
	if err := client.UploadDocument(context.Background(), "", DocumentTypeLOA, "loa.pdf", strings.NewReader("data")); err == nil {
		t.Fatalf("expected order id validation error")
	}
	if err := client.UploadDocument(context.Background(), "ord_1", "", "loa.pdf", strings.NewReader("data")); err == nil {
		t.Fatalf("expected document type validation error")
	}
	if err := client.UploadDocument(context.Background(), "ord_1", DocumentTypeLOA, "loa.pdf", nil); err == nil {
		t.Fatalf("expected reader validation error")
	}
}

func TestShouldRetryLogic(t *testing.T) {
	if !shouldRetry(0, timeoutErr{}) {
		t.Fatalf("expected timeout errors to retry")
	}
	if shouldRetry(0, context.Canceled) {
		t.Fatalf("context cancel should not retry")
	}
	if !shouldRetry(http.StatusTooManyRequests, nil) {
		t.Fatalf("429 should retry")
	}
	if !shouldRetry(http.StatusBadGateway, nil) {
		t.Fatalf("5xx should retry")
	}
	if shouldRetry(http.StatusBadRequest, nil) {
		t.Fatalf("4xx (except 429) should not retry")
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestClientValidationShortCircuits(t *testing.T) {
	client := newTestClient(t, nil, Config{})
	if _, err := client.SendMessage(context.Background(), SendMessageRequest{}); err == nil {
		t.Fatalf("expected send validation error")
	}
	if _, err := client.CreateHostedOrder(context.Background(), HostedOrderRequest{}); err == nil {
		t.Fatalf("expected hosted order validation error")
	}
	if _, err := client.CreateBrand(context.Background(), BrandRequest{}); err == nil {
		t.Fatalf("expected brand validation error")
	}
	if _, err := client.CreateCampaign(context.Background(), CampaignRequest{}); err == nil {
		t.Fatalf("expected campaign validation error")
	}
	if _, err := client.GetHostedOrder(context.Background(), ""); err == nil {
		t.Fatalf("expected get hosted order validation error")
	}
}

func TestGetHostedOrderDecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":`))
	}))
	defer server.Close()
	client := newTestClient(t, server, Config{})
	if _, err := client.GetHostedOrder(context.Background(), "order"); err == nil {
		t.Fatalf("expected decode error")
	}
}

func TestGetHostedOrderHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"detail":"missing"}`))
	}))
	defer server.Close()
	client := newTestClient(t, server, Config{MaxRetries: 0})
	if _, err := client.GetHostedOrder(context.Background(), "order"); err == nil {
		t.Fatalf("expected http error")
	}
}

func TestSendMessageHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"detail":"bad payload"}`))
	}))
	defer server.Close()
	client := newTestClient(t, server, Config{MaxRetries: 0})
	_, err := client.SendMessage(context.Background(), SendMessageRequest{
		From: "+1", To: "+2", Body: "hi",
	})
	if err == nil || !strings.Contains(err.Error(), "bad payload") {
		t.Fatalf("expected api error, got %v", err)
	}
}

func TestSendMessageHTTPErrorFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("gateway exploded"))
	}))
	defer server.Close()
	client := newTestClient(t, server, Config{MaxRetries: 0})
	if _, err := client.SendMessage(context.Background(), SendMessageRequest{From: "+1", To: "+2", Body: "hi"}); err == nil {
		t.Fatalf("expected api error")
	}
}

func TestAPIErrorFormatting(t *testing.T) {
	errWithTitle := &apiError{Title: "bad", StatusCode: 400}
	if !strings.Contains(errWithTitle.Error(), "bad") {
		t.Fatalf("expected title in error string")
	}
	errWithDetail := &apiError{Detail: "oops", StatusCode: 422}
	if !strings.Contains(errWithDetail.Error(), "oops") {
		t.Fatalf("expected detail in error string")
	}
	errFallback := &apiError{StatusCode: 500}
	if !strings.Contains(errFallback.Error(), "500") {
		t.Fatalf("expected fallback message")
	}
}

func TestDecodeAPIErrorFallback(t *testing.T) {
	err := decodeAPIError(500, []byte("broken json"))
	apiErr, ok := err.(*apiError)
	if !ok || apiErr.Detail != "broken json" {
		t.Fatalf("expected fallback detail, got %#v", err)
	}
}

func TestDecodeDataWrapperError(t *testing.T) {
	if _, err := decodeDataWrapper[struct{}]([]byte("nope")); err == nil {
		t.Fatalf("expected decode error")
	}
}

func TestLogRetryWithoutLogger(t *testing.T) {
	client := &Client{}
	client.logRetry("/x", 0, 500, errors.New("boom"))
}

func TestVerifyWebhookSignatureRequiresSecret(t *testing.T) {
	client := newTestClient(t, nil, Config{WebhookSecret: ""})
	if err := client.VerifyWebhookSignature("", "", []byte("{}")); err != nil {
		t.Fatalf("expected nil error when secret is empty, got %v", err)
	}
}

func TestInvokeContextCancellation(t *testing.T) {
	client := newTestClient(t, nil, Config{})
	client.httpClient = &http.Client{Transport: cancelOnContextTransport{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.invoke(ctx, http.MethodGet, "/test", nil, nil, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled error, got %v", err)
	}
}

func TestInvokeSleepCancellation(t *testing.T) {
	client := newTestClient(t, nil, Config{MaxRetries: 1, Backoff: 50 * time.Millisecond})
	client.httpClient = &http.Client{Transport: retryTransport{}}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	if _, err := client.invoke(ctx, http.MethodGet, "/retry", nil, nil, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation during sleep, got %v", err)
	}
}

func TestInvokeNonRetryableError(t *testing.T) {
	client := newTestClient(t, nil, Config{MaxRetries: 1})
	client.httpClient = &http.Client{Transport: cancelErrorTransport{}}
	if _, err := client.invoke(context.Background(), http.MethodGet, "/nr", nil, nil, ""); err == nil || !strings.Contains(err.Error(), "http error") {
		t.Fatalf("expected non-retryable http error, got %v", err)
	}
}

func TestInvokeReadError(t *testing.T) {
	client := newTestClient(t, nil, Config{})
	client.httpClient = &http.Client{Transport: readErrorTransport{}}
	if _, err := client.invoke(context.Background(), http.MethodGet, "/read", nil, nil, ""); err == nil || !strings.Contains(err.Error(), "read response") {
		t.Fatalf("expected read error, got %v", err)
	}
}

type cancelOnContextTransport struct{}

func (cancelOnContextTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}

type retryTransport struct{}

func (retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       io.NopCloser(strings.NewReader("{}")),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

type cancelErrorTransport struct{}

func (cancelErrorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, context.Canceled
}

type readErrorTransport struct{}

func (readErrorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       errBody{},
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

type errBody struct{}

func (errBody) Read(p []byte) (int, error) { return 0, errors.New("body read fail") }
func (errBody) Close() error               { return nil }

type failingReader struct{}

func (failingReader) Read(p []byte) (int, error) { return 0, errors.New("copy fail") }

func newTestClient(t *testing.T, server *httptest.Server, cfg Config) *Client {
	t.Helper()
	if server != nil {
		cfg.BaseURL = server.URL
	}
	cfg.APIKey = "test"
	cfg.Timeout = 2 * time.Second
	cfg.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	client, err := New(cfg)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client
}

func mustLoadFixture(t *testing.T, name string) []byte {
	t.Helper()
	_, filename, _, _ := runtime.Caller(0)
	base := filepath.Dir(filename)
	path := filepath.Join(base, "testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}
