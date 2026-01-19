package tests

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	pgxmock "github.com/pashagolub/pgxmock/v4"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	httphandlers "github.com/wolfman30/medspa-ai-platform/internal/http/handlers"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/telnyxclient"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestRegression_TwilioSMSFlow(t *testing.T) {
	logger := logging.New("error")
	leadsRepo := leads.NewInMemoryRepository()
	resolver := messaging.NewStaticOrgResolver(map[string]string{
		"+15550001111": "org-1",
	})
	publisher := &stubPublisher{}
	smsSender := &stubMessenger{}
	handler := messaging.NewHandler("", publisher, resolver, smsSender, leadsRepo, logger)

	form := url.Values{}
	form.Set("MessageSid", "SM123")
	form.Set("AccountSid", "AC123")
	form.Set("From", "+1 (555) 123-4567")
	form.Set("To", "+15550001111")
	form.Set("Body", "Hi there")

	req := httptest.NewRequest(http.MethodPost, "/messaging/twilio/webhook", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.TwilioWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/xml" {
		t.Fatalf("expected XML response, got %q", ct)
	}
	if publisher.messageCalls != 1 {
		t.Fatalf("expected message to be enqueued once, got %d", publisher.messageCalls)
	}
	expectedConversationID := "sms:org-1:15551234567"
	if publisher.lastMessage.ConversationID != expectedConversationID {
		t.Fatalf("unexpected conversation id %q", publisher.lastMessage.ConversationID)
	}
	if publisher.lastMessage.OrgID != "org-1" {
		t.Fatalf("unexpected org id %q", publisher.lastMessage.OrgID)
	}
	if publisher.lastMessage.Message != "Hi there" {
		t.Fatalf("unexpected message %q", publisher.lastMessage.Message)
	}
	if smsSender.calls != 1 {
		t.Fatalf("expected sms ack to be sent")
	}
	if !messaging.IsSmsAckMessage(smsSender.last.Body) {
		t.Fatalf("expected ack body, got %q", smsSender.last.Body)
	}
	if smsSender.last.ConversationID != expectedConversationID {
		t.Fatalf("unexpected ack conversation id %q", smsSender.last.ConversationID)
	}
}

func TestRegression_TwilioVoiceMissedCallFlow(t *testing.T) {
	logger := logging.New("error")
	leadsRepo := leads.NewInMemoryRepository()
	resolver := messaging.NewStaticOrgResolver(map[string]string{
		"+15550001111": "org-1",
	})
	publisher := &stubPublisher{}
	smsSender := &stubMessenger{}
	handler := messaging.NewHandler("", publisher, resolver, smsSender, leadsRepo, logger)

	form := url.Values{}
	form.Set("CallSid", "CA123")
	form.Set("CallStatus", "busy")
	form.Set("From", "+1 (555) 123-4567")
	form.Set("To", "+15550001111")

	req := httptest.NewRequest(http.MethodPost, "/webhooks/twilio/voice", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.TwilioVoiceWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/xml" {
		t.Fatalf("expected XML response, got %q", ct)
	}
	if publisher.startCalls != 1 {
		t.Fatalf("expected start to be enqueued once, got %d", publisher.startCalls)
	}
	expectedConversationID := "sms:org-1:15551234567"
	if publisher.lastStart.ConversationID != expectedConversationID {
		t.Fatalf("unexpected conversation id %q", publisher.lastStart.ConversationID)
	}
	if publisher.lastStart.OrgID != "org-1" {
		t.Fatalf("unexpected org id %q", publisher.lastStart.OrgID)
	}
	if publisher.lastStart.Channel != conversation.ChannelSMS {
		t.Fatalf("unexpected channel %q", publisher.lastStart.Channel)
	}
	if !publisher.lastStart.Silent {
		t.Fatalf("expected silent start for missed-call flow")
	}
	if publisher.lastStart.AckMessage == "" {
		t.Fatalf("expected ack message to be set")
	}
	if smsSender.calls != 1 {
		t.Fatalf("expected missed-call ack SMS to be sent")
	}
	if smsSender.last.Body != publisher.lastStart.AckMessage {
		t.Fatalf("expected ack body to match start request")
	}
}

func TestRegression_TelnyxVoiceMissedCallFlow(t *testing.T) {
	logger := logging.New("error")
	clinicID := uuid.New()
	store := &stubMessagingStore{clinicID: clinicID}
	processed := &stubProcessedTracker{}
	telnyx := &stubTelnyxClient{}
	publisher := &stubPublisher{}
	leadsRepo := leads.NewInMemoryRepository()
	handler := httphandlers.NewTelnyxWebhookHandler(httphandlers.TelnyxWebhookConfig{
		Store:            store,
		Processed:        processed,
		Telnyx:           telnyx,
		Conversation:     publisher,
		Leads:            leadsRepo,
		Logger:           logger,
		MessagingProfile: "profile",
		VoiceAck:         "TEST ACK",
	})

	payload := `{"data":{"id":"evt_voice_1","event_type":"call.hangup","occurred_at":"2025-01-01T00:00:00Z","payload":{"id":"call_123","status":"no_answer","hangup_cause":"originator_cancel","from_number":"+15551234567","to_number":"+15550001111"}}}`
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/voice", strings.NewReader(payload))
	req.Header.Set("Telnyx-Timestamp", "123")
	req.Header.Set("Telnyx-Signature", "sig")
	rec := httptest.NewRecorder()

	handler.HandleVoice(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if publisher.startCalls != 1 {
		t.Fatalf("expected start to be enqueued once, got %d", publisher.startCalls)
	}
	if publisher.lastStart.Source != "telnyx_voice" {
		t.Fatalf("expected source telnyx_voice, got %q", publisher.lastStart.Source)
	}
	if publisher.lastStart.Channel != conversation.ChannelSMS {
		t.Fatalf("unexpected channel %q", publisher.lastStart.Channel)
	}
	if !publisher.lastStart.Silent {
		t.Fatalf("expected silent start for missed-call flow")
	}
	if publisher.lastStart.AckMessage != "TEST ACK" {
		t.Fatalf("unexpected ack message %q", publisher.lastStart.AckMessage)
	}
	if publisher.lastStart.LeadID == "" {
		t.Fatalf("expected lead id to be set")
	}
	expectedConversationID := "sms:" + clinicID.String() + ":15551234567"
	if publisher.lastStart.ConversationID != expectedConversationID {
		t.Fatalf("unexpected conversation id %q", publisher.lastStart.ConversationID)
	}
	if publisher.lastStart.OrgID != clinicID.String() {
		t.Fatalf("unexpected org id %q", publisher.lastStart.OrgID)
	}
	if telnyx.lastSend == nil {
		t.Fatalf("expected telnyx send to be called")
	}
	if telnyx.lastSend.From != "+15550001111" || telnyx.lastSend.To != "+15551234567" {
		t.Fatalf("unexpected telnyx send from/to: %+v", telnyx.lastSend)
	}
	if telnyx.lastSend.Body != "TEST ACK" {
		t.Fatalf("unexpected telnyx send body %q", telnyx.lastSend.Body)
	}
	if store.lastLookup != "+15550001111" {
		t.Fatalf("expected clinic lookup by number, got %q", store.lastLookup)
	}
	if processed.markCalls != 1 {
		t.Fatalf("expected processed marker to be called")
	}
}

func TestRegression_TelnyxInboundSMSFlow(t *testing.T) {
	logger := logging.New("error")
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := messaging.NewStore(mock)
	processed := &stubProcessedTracker{}
	telnyx := &stubTelnyxClient{}
	publisher := &stubPublisher{}
	leadsRepo := leads.NewInMemoryRepository()
	handler := httphandlers.NewTelnyxWebhookHandler(httphandlers.TelnyxWebhookConfig{
		Store:            store,
		Processed:        processed,
		Telnyx:           telnyx,
		Conversation:     publisher,
		Leads:            leadsRepo,
		Logger:           logger,
		MessagingProfile: "profile",
	})

	clinicID := uuid.New()
	mock.ExpectQuery("SELECT clinic_id").
		WithArgs("+15559998888").
		WillReturnRows(pgxmock.NewRows([]string{"clinic_id"}).AddRow(clinicID))
	mock.ExpectQuery("SELECT 1 FROM messages").
		WithArgs(clinicID, "+15550001111", "+15559998888").
		WillReturnRows(pgxmock.NewRows([]string{"exists"}))
	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO messages").
		WithArgs(clinicID, "+15550001111", "+15559998888", "inbound", "Need info", pgxmock.AnyArg(), "received", "msg_inbound", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectExec("INSERT INTO outbox").
		WithArgs(pgxmock.AnyArg(), "clinic:"+clinicID.String(), "messaging.message.received.v1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectQuery("SELECT 1 FROM unsubscribes").
		WithArgs(clinicID, "+15550001111").
		WillReturnRows(pgxmock.NewRows([]string{"exists"}))
	mock.ExpectCommit()

	payload := loadRegressionFixture(t, "telnyx_inbound_message.json")
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/messages", strings.NewReader(string(payload)))
	req.Header.Set("Telnyx-Timestamp", "123")
	req.Header.Set("Telnyx-Signature", "sig")
	rec := httptest.NewRecorder()

	handler.HandleMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if publisher.messageCalls != 1 {
		t.Fatalf("expected message to be enqueued once, got %d", publisher.messageCalls)
	}
	if publisher.lastMessage.OrgID != clinicID.String() {
		t.Fatalf("unexpected org id %q", publisher.lastMessage.OrgID)
	}
	if publisher.lastMessage.Message != "Need info" {
		t.Fatalf("unexpected message %q", publisher.lastMessage.Message)
	}
	if telnyx.lastSend == nil {
		t.Fatalf("expected telnyx send to be called")
	}
	if !messaging.IsSmsAckMessage(telnyx.lastSend.Body) {
		t.Fatalf("expected ack body, got %q", telnyx.lastSend.Body)
	}
	if processed.markCalls != 2 {
		t.Fatalf("expected processed marker to be called twice, got %d", processed.markCalls)
	}
	if processed.lastEvent != "evt_message" {
		t.Fatalf("unexpected last processed event %q", processed.lastEvent)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

type stubPublisher struct {
	messageCalls int
	startCalls   int
	lastMessage  conversation.MessageRequest
	lastStart    conversation.StartRequest
}

func (s *stubPublisher) EnqueueStart(ctx context.Context, jobID string, req conversation.StartRequest, opts ...conversation.PublishOption) error {
	s.startCalls++
	s.lastStart = req
	return nil
}

func (s *stubPublisher) EnqueueMessage(ctx context.Context, jobID string, req conversation.MessageRequest, opts ...conversation.PublishOption) error {
	s.messageCalls++
	s.lastMessage = req
	return nil
}

type stubMessenger struct {
	calls int
	last  conversation.OutboundReply
}

func (s *stubMessenger) SendReply(ctx context.Context, reply conversation.OutboundReply) error {
	s.calls++
	s.last = reply
	return nil
}

var errNotImplemented = errors.New("not implemented")

type stubMessagingStore struct {
	clinicID   uuid.UUID
	lastLookup string
}

func (s *stubMessagingStore) Begin(ctx context.Context) (pgx.Tx, error) {
	return nil, errNotImplemented
}

func (s *stubMessagingStore) InsertMessage(ctx context.Context, q messaging.Querier, rec messaging.MessageRecord) (uuid.UUID, error) {
	return uuid.Nil, errNotImplemented
}

func (s *stubMessagingStore) InsertBrand(ctx context.Context, q messaging.Querier, rec messaging.BrandRecord) error {
	return errNotImplemented
}

func (s *stubMessagingStore) InsertCampaign(ctx context.Context, q messaging.Querier, rec messaging.CampaignRecord) error {
	return errNotImplemented
}

func (s *stubMessagingStore) UpsertHostedOrder(ctx context.Context, q messaging.Querier, record messaging.HostedOrderRecord) error {
	return errNotImplemented
}

func (s *stubMessagingStore) HasInboundMessage(ctx context.Context, clinicID uuid.UUID, from string, to string) (bool, error) {
	return false, errNotImplemented
}

func (s *stubMessagingStore) IsUnsubscribed(ctx context.Context, clinicID uuid.UUID, recipient string) (bool, error) {
	return false, errNotImplemented
}

func (s *stubMessagingStore) InsertUnsubscribe(ctx context.Context, q messaging.Querier, clinicID uuid.UUID, recipient string, source string) error {
	return errNotImplemented
}

func (s *stubMessagingStore) DeleteUnsubscribe(ctx context.Context, q messaging.Querier, clinicID uuid.UUID, recipient string) error {
	return errNotImplemented
}

func (s *stubMessagingStore) LookupClinicByNumber(ctx context.Context, number string) (uuid.UUID, error) {
	s.lastLookup = number
	return s.clinicID, nil
}

func (s *stubMessagingStore) UpdateMessageStatus(ctx context.Context, providerMessageID, status string, deliveredAt, failedAt *time.Time) error {
	return errNotImplemented
}

func (s *stubMessagingStore) ScheduleRetry(ctx context.Context, q messaging.Querier, id uuid.UUID, status string, nextRetry time.Time) error {
	return errNotImplemented
}

func (s *stubMessagingStore) ListRetryCandidates(ctx context.Context, limit int, maxAttempts int) ([]messaging.MessageRecord, error) {
	return nil, errNotImplemented
}

func (s *stubMessagingStore) PendingHostedOrders(ctx context.Context, limit int) ([]messaging.HostedOrderRecord, error) {
	return nil, errNotImplemented
}

type stubProcessedTracker struct {
	seen      map[string]bool
	markCalls int
	lastEvent string
}

func (s *stubProcessedTracker) AlreadyProcessed(ctx context.Context, provider, eventID string) (bool, error) {
	if s.seen == nil {
		return false, nil
	}
	return s.seen[eventID], nil
}

func (s *stubProcessedTracker) MarkProcessed(ctx context.Context, provider, eventID string) (bool, error) {
	if s.seen == nil {
		s.seen = make(map[string]bool)
	}
	s.seen[eventID] = true
	s.markCalls++
	s.lastEvent = eventID
	return true, nil
}

type stubTelnyxClient struct {
	lastSend  *telnyxclient.SendMessageRequest
	verifyErr error
}

func (s *stubTelnyxClient) CheckHostedEligibility(ctx context.Context, number string) (*telnyxclient.HostedEligibilityResponse, error) {
	return nil, errNotImplemented
}

func (s *stubTelnyxClient) CreateHostedOrder(ctx context.Context, req telnyxclient.HostedOrderRequest) (*telnyxclient.HostedOrder, error) {
	return nil, errNotImplemented
}

func (s *stubTelnyxClient) CreateBrand(ctx context.Context, req telnyxclient.BrandRequest) (*telnyxclient.Brand, error) {
	return nil, errNotImplemented
}

func (s *stubTelnyxClient) CreateCampaign(ctx context.Context, req telnyxclient.CampaignRequest) (*telnyxclient.Campaign, error) {
	return nil, errNotImplemented
}

func (s *stubTelnyxClient) SendMessage(ctx context.Context, req telnyxclient.SendMessageRequest) (*telnyxclient.MessageResponse, error) {
	s.lastSend = &req
	return &telnyxclient.MessageResponse{ID: "msg_1", Status: "sent"}, nil
}

func (s *stubTelnyxClient) VerifyWebhookSignature(timestamp, signature string, payload []byte) error {
	return s.verifyErr
}

func (s *stubTelnyxClient) GetHostedOrder(ctx context.Context, orderID string) (*telnyxclient.HostedOrder, error) {
	return nil, errNotImplemented
}

func loadRegressionFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "internal", "http", "handlers", "testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}
