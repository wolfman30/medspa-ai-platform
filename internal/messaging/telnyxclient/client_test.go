package telnyxclient

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

func TestPayloadValidationErrors(t *testing.T) {
	if err := (SendMessageRequest{}).validate(); err == nil {
		t.Fatalf("expected send validation error")
	}
	if err := (HostedOrderRequest{}).validate(); err == nil {
		t.Fatalf("expected hosted order validation error")
	}
	if err := (BrandRequest{}).validate(); err == nil {
		t.Fatalf("expected brand validation error")
	}
	if err := (CampaignRequest{}).validate(); err == nil {
		t.Fatalf("expected campaign validation error")
	}
}

func TestVerifyWebhookSignatureMismatch(t *testing.T) {
	client := newTestClient(t, nil, Config{WebhookSecret: "secret"})
	if err := client.VerifyWebhookSignature(strconv.FormatInt(time.Now().Unix(), 10), "deadbeef", []byte("{}")); err == nil {
		t.Fatalf("expected signature mismatch")
	}
}

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
