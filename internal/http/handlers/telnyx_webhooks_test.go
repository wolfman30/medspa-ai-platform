package handlers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	pgxmock "github.com/pashagolub/pgxmock/v4"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestTelnyxInboundStop(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := messaging.NewStore(mock)
	processed := &stubProcessedTracker{}
	telnyxStub := &testTelnyxClient{}
	conversationStub := &stubConversationPublisher{}
	handler := NewTelnyxWebhookHandler(TelnyxWebhookConfig{
		Store:            store,
		Processed:        processed,
		Telnyx:           telnyxStub,
		Conversation:     conversationStub,
		Logger:           logging.Default(),
		MessagingProfile: "profile",
		StopAck:          "STOP ACK",
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
		WithArgs(clinicID, "+15550001111", "+15559998888", "inbound", "STOP", pgxmock.AnyArg(), "received", "msg_inbound", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectExec("INSERT INTO outbox").
		WithArgs(pgxmock.AnyArg(), "clinic:"+clinicID.String(), "messaging.message.received.v1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("INSERT INTO unsubscribes").
		WithArgs(clinicID, "+15550001111", "STOP").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	req := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/messages", bytes.NewReader(loadFixture(t, "telnyx_inbound_stop.json")))
	req.Header.Set("Telnyx-Timestamp", "123")
	req.Header.Set("Telnyx-Signature", "abc")
	rec := httptest.NewRecorder()

	handler.HandleMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if telnyxStub.lastSendReq == nil {
		t.Fatalf("expected auto reply to be sent")
	}
	if conversationStub.calls != 0 {
		t.Fatalf("expected conversation not to be enqueued on STOP")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestTelnyxInboundEnqueuesConversation(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := messaging.NewStore(mock)
	conv := &stubConversationPublisher{}
	leadRepo := &stubLeadsRepo{lead: &leads.Lead{ID: "lead-abc", OrgID: "org-x"}}
	handler := NewTelnyxWebhookHandler(TelnyxWebhookConfig{
		Store:            store,
		Processed:        &stubProcessedTracker{},
		Telnyx:           &testTelnyxClient{},
		Conversation:     conv,
		Leads:            leadRepo,
		Logger:           logging.Default(),
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

	req := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/messages", bytes.NewReader(loadFixture(t, "telnyx_inbound_message.json")))
	req.Header.Set("Telnyx-Timestamp", "123")
	req.Header.Set("Telnyx-Signature", "abc")
	rec := httptest.NewRecorder()

	handler.HandleMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if conv.calls != 1 {
		t.Fatalf("expected conversation publisher to be invoked once, got %d", conv.calls)
	}
	if !leadRepo.called {
		t.Fatalf("expected lead repo to be invoked")
	}
	if conv.last.LeadID != "lead-abc" {
		t.Fatalf("expected lead id from repo to propagate, got %s", conv.last.LeadID)
	}
	if conv.last.Metadata["telnyx_message_id"] != "msg_inbound" {
		t.Fatalf("expected telnyx metadata to propagate")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestTelnyxInboundDuplicateProviderMessageIsIgnored(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := messaging.NewStore(mock)
	conv := &stubConversationPublisher{}
	telnyxStub := &testTelnyxClient{}
	handler := NewTelnyxWebhookHandler(TelnyxWebhookConfig{
		Store:            store,
		Processed:        &stubProcessedTracker{},
		Telnyx:           telnyxStub,
		Conversation:     conv,
		Logger:           logging.Default(),
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
		WillReturnError(&pgconn.PgError{Code: "23505", ConstraintName: "idx_messages_provider_message"})
	mock.ExpectRollback()

	req := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/messages", bytes.NewReader(loadFixture(t, "telnyx_inbound_message.json")))
	req.Header.Set("Telnyx-Timestamp", "123")
	req.Header.Set("Telnyx-Signature", "abc")
	rec := httptest.NewRecorder()

	handler.HandleMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if conv.calls != 0 {
		t.Fatalf("expected conversation not to be enqueued on duplicate, got %d", conv.calls)
	}
	if telnyxStub.lastSendReq != nil {
		t.Fatalf("expected no auto reply on duplicate")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestTelnyxFirstContactAck_DemoMode(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := messaging.NewStore(mock)
	conv := &stubConversationPublisher{}
	telnyxStub := &testTelnyxClient{}

	handler := NewTelnyxWebhookHandler(TelnyxWebhookConfig{
		Store:            store,
		Processed:        &stubProcessedTracker{},
		Telnyx:           telnyxStub,
		Conversation:     conv,
		Logger:           logging.Default(),
		MessagingProfile: "profile",
		FirstContactAck:  "FIRST CONTACT",
		DemoMode:         true,
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

	req := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/messages", bytes.NewReader(loadFixture(t, "telnyx_inbound_message.json")))
	req.Header.Set("Telnyx-Timestamp", "123")
	req.Header.Set("Telnyx-Signature", "abc")
	rec := httptest.NewRecorder()

	handler.HandleMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if telnyxStub.lastSendReq == nil || telnyxStub.lastSendReq.Body != "FIRST CONTACT" {
		t.Fatalf("expected demo first-contact ack, got %#v", telnyxStub.lastSendReq)
	}
	if conv.calls != 1 {
		t.Fatalf("expected conversation to be enqueued, got %d", conv.calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestTelnyxYesOptIn_DemoMode(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := messaging.NewStore(mock)
	processed := &stubProcessedTracker{}
	telnyxStub := &testTelnyxClient{}
	conv := &stubConversationPublisher{}

	handler := NewTelnyxWebhookHandler(TelnyxWebhookConfig{
		Store:            store,
		Processed:        processed,
		Telnyx:           telnyxStub,
		Conversation:     conv,
		Logger:           logging.Default(),
		MessagingProfile: "profile",
		StartAck:         "START ACK",
		DemoMode:         true,
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
		WithArgs(clinicID, "+15550001111", "+15559998888", "inbound", "YES", pgxmock.AnyArg(), "received", "msg_inbound", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectExec("INSERT INTO outbox").
		WithArgs(pgxmock.AnyArg(), "clinic:"+clinicID.String(), "messaging.message.received.v1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("DELETE FROM unsubscribes").
		WithArgs(clinicID, "+15550001111").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectCommit()

	payload := bytes.ReplaceAll(loadFixture(t, "telnyx_inbound_stop.json"), []byte(`"STOP"`), []byte(`"YES"`))
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/messages", bytes.NewReader(payload))
	req.Header.Set("Telnyx-Timestamp", "123")
	req.Header.Set("Telnyx-Signature", "abc")
	rec := httptest.NewRecorder()

	handler.HandleMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if telnyxStub.lastSendReq == nil || telnyxStub.lastSendReq.Body != "START ACK" {
		t.Fatalf("expected opt-in ack, got %#v", telnyxStub.lastSendReq)
	}
	if conv.calls != 0 {
		t.Fatalf("expected conversation not to be enqueued on YES opt-in, got %d", conv.calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestTelnyxVoiceMissedCallStartsConversation(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := messaging.NewStore(mock)
	conv := &stubConversationPublisher{}
	leadRepo := &stubLeadsRepo{lead: &leads.Lead{ID: "lead-voice", OrgID: "org-x"}}
	telnyxStub := &testTelnyxClient{}
	handler := NewTelnyxWebhookHandler(TelnyxWebhookConfig{
		Store:            store,
		Processed:        &stubProcessedTracker{},
		Telnyx:           telnyxStub,
		Conversation:     conv,
		Leads:            leadRepo,
		Logger:           logging.Default(),
		MessagingProfile: "profile",
	})

	clinicID := uuid.New()
	mock.ExpectQuery("SELECT clinic_id").
		WithArgs("+15559998888").
		WillReturnRows(pgxmock.NewRows([]string{"clinic_id"}).AddRow(clinicID))

	req := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/voice", bytes.NewReader(loadFixture(t, "telnyx_voice_missed.json")))
	req.Header.Set("Telnyx-Timestamp", "123")
	req.Header.Set("Telnyx-Signature", "abc")
	rec := httptest.NewRecorder()

	handler.HandleVoice(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if conv.startCalls != 1 {
		t.Fatalf("expected start conversation to be enqueued once, got %d", conv.startCalls)
	}
	if conv.lastStart.Source != "telnyx_voice" {
		t.Fatalf("expected source telnyx_voice, got %s", conv.lastStart.Source)
	}
	if conv.lastStart.LeadID != "lead-voice" {
		t.Fatalf("expected lead id from repo, got %s", conv.lastStart.LeadID)
	}
	if telnyxStub.lastSendReq == nil || telnyxStub.lastSendReq.To != "+15550002222" || telnyxStub.lastSendReq.From != "+15559998888" {
		t.Fatalf("expected missed-call ack to be sent via telnyx")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestTelnyxDeliveryStatus(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := messaging.NewStore(mock)
	handler := NewTelnyxWebhookHandler(TelnyxWebhookConfig{
		Store:     store,
		Processed: &stubProcessedTracker{},
		Telnyx:    &testTelnyxClient{},
		Logger:    logging.Default(),
	})

	mock.ExpectExec("UPDATE messages").
		WithArgs("msg_outbound", "delivered", pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	req := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/messages", bytes.NewReader(loadFixture(t, "telnyx_dlr.json")))
	req.Header.Set("Telnyx-Timestamp", "123")
	req.Header.Set("Telnyx-Signature", "abc")
	rec := httptest.NewRecorder()

	handler.HandleMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestTelnyxHostedActivated(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := messaging.NewStore(mock)
	handler := NewTelnyxWebhookHandler(TelnyxWebhookConfig{
		Store:     store,
		Processed: &stubProcessedTracker{},
		Telnyx:    &testTelnyxClient{},
		Logger:    logging.Default(),
	})

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO hosted_number_orders").
		WithArgs(pgxmock.AnyArg(), uuid.MustParse("11111111-2222-3333-4444-555555555555"), "+15559998888", pgxmock.AnyArg(), pgxmock.AnyArg(), "hno_123").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("INSERT INTO outbox").
		WithArgs(pgxmock.AnyArg(), "clinic:11111111-2222-3333-4444-555555555555", "messaging.hosted_order.activated.v1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	req := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/hosted", bytes.NewReader(loadFixture(t, "telnyx_hosted.json")))
	req.Header.Set("Telnyx-Timestamp", "123")
	req.Header.Set("Telnyx-Signature", "abc")
	rec := httptest.NewRecorder()

	handler.HandleHosted(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestTelnyxWebhookInvalidSignature(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	handler := NewTelnyxWebhookHandler(TelnyxWebhookConfig{
		Store:     messaging.NewStore(mock),
		Processed: &stubProcessedTracker{},
		Telnyx:    &testTelnyxClient{verifyErr: errors.New("bad sig")},
		Logger:    logging.Default(),
	})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/messages", bytes.NewReader(loadFixture(t, "telnyx_inbound_stop.json")))
	rec := httptest.NewRecorder()
	handler.HandleMessages(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestTelnyxWebhookAlreadyProcessed(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	defer mock.Close()
	store := messaging.NewStore(mock)
	tracker := &stubProcessedTracker{seen: map[string]bool{"evt_inbound": true}}
	handler := NewTelnyxWebhookHandler(TelnyxWebhookConfig{
		Store:     store,
		Processed: tracker,
		Telnyx:    &testTelnyxClient{},
		Logger:    logging.Default(),
	})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/messages", bytes.NewReader(loadFixture(t, "telnyx_inbound_stop.json")))
	rec := httptest.NewRecorder()
	handler.HandleMessages(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for already processed, got %d", rec.Code)
	}
}

func TestTelnyxWebhookClinicNotFound(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := messaging.NewStore(mock)
	handler := NewTelnyxWebhookHandler(TelnyxWebhookConfig{
		Store:     store,
		Processed: &stubProcessedTracker{},
		Telnyx:    &testTelnyxClient{},
		Logger:    logging.Default(),
	})
	mock.ExpectQuery("SELECT clinic_id").
		WithArgs("+15559998888").
		WillReturnError(pgx.ErrNoRows)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/messages", bytes.NewReader(loadFixture(t, "telnyx_inbound_stop.json")))
	req.Header.Set("Telnyx-Timestamp", "123")
	req.Header.Set("Telnyx-Signature", "abc")
	rec := httptest.NewRecorder()
	handler.HandleMessages(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestTelnyxDeliveryStatusUpdateError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := messaging.NewStore(mock)
	handler := NewTelnyxWebhookHandler(TelnyxWebhookConfig{
		Store:     store,
		Processed: &stubProcessedTracker{},
		Telnyx:    &testTelnyxClient{},
		Logger:    logging.Default(),
	})
	mock.ExpectExec("UPDATE messages").
		WithArgs("msg_outbound", "delivered", pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(errors.New("db down"))
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/messages", bytes.NewReader(loadFixture(t, "telnyx_dlr.json")))
	req.Header.Set("Telnyx-Timestamp", "123")
	req.Header.Set("Telnyx-Signature", "abc")
	rec := httptest.NewRecorder()
	handler.HandleMessages(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestTelnyxHelpAutoReply(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := messaging.NewStore(mock)
	telnyxStub := &testTelnyxClient{}
	handler := NewTelnyxWebhookHandler(TelnyxWebhookConfig{
		Store:            store,
		Processed:        &stubProcessedTracker{},
		Telnyx:           telnyxStub,
		Logger:           logging.Default(),
		MessagingProfile: "profile",
		HelpAck:          "HELP ACK",
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
		WithArgs(clinicID, "+15550001111", "+15559998888", "inbound", "HELP", pgxmock.AnyArg(), "received", "msg_inbound", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectExec("INSERT INTO outbox").
		WithArgs(pgxmock.AnyArg(), "clinic:"+clinicID.String(), "messaging.message.received.v1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectQuery("SELECT 1 FROM unsubscribes").
		WithArgs(clinicID, "+15550001111").
		WillReturnRows(pgxmock.NewRows([]string{"exists"}))
	mock.ExpectCommit()

	payload := bytes.ReplaceAll(loadFixture(t, "telnyx_inbound_stop.json"), []byte(`"STOP"`), []byte(`"HELP"`))
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/messages", bytes.NewReader(payload))
	req.Header.Set("Telnyx-Timestamp", "123")
	req.Header.Set("Telnyx-Signature", "abc")
	rec := httptest.NewRecorder()
	handler.HandleMessages(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if telnyxStub.lastSendReq == nil || telnyxStub.lastSendReq.Body != "HELP ACK" {
		t.Fatalf("expected help auto reply")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestTelnyxAppendTranscriptPersistsToConversationStore(t *testing.T) {
	store := &stubConversationStore{}
	handler := NewTelnyxWebhookHandler(TelnyxWebhookConfig{
		Logger:            logging.Default(),
		ConversationStore: store,
	})

	handler.appendTranscript(context.Background(), "sms:org-1:15551234567", conversation.SMSTranscriptMessage{
		Role: "user",
		Body: "Hello",
	})

	if !store.appended {
		t.Fatalf("expected conversation store append to be called")
	}
	if store.lastID != "sms:org-1:15551234567" {
		t.Fatalf("unexpected conversation id: %s", store.lastID)
	}
	if store.lastMsg.Role != "user" || store.lastMsg.Body != "Hello" {
		t.Fatalf("unexpected transcript message: %#v", store.lastMsg)
	}
}

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}
