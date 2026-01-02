package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	pgxmock "github.com/pashagolub/pgxmock/v4"

	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestTierA_CI01_MissedCallIntroSMS(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()

	store := messaging.NewStore(mock)
	processed := &stubProcessedTracker{}
	telnyxStub := &testTelnyxClient{}
	conv := &stubConversationPublisher{}
	leadsRepo := leads.NewInMemoryRepository()

	handler := NewTelnyxWebhookHandler(TelnyxWebhookConfig{
		Store:            store,
		Processed:        processed,
		Telnyx:           telnyxStub,
		Conversation:     conv,
		Leads:            leadsRepo,
		Logger:           logging.Default(),
		MessagingProfile: "profile",
	})

	clinicID := uuid.New()
	mock.ExpectQuery("SELECT clinic_id").
		WithArgs("+15559998888").
		WillReturnRows(pgxmock.NewRows([]string{"clinic_id"}).AddRow(clinicID))

	payload := loadTelnyxVoiceFixture(t, "telnyx_voice_missed.json", telnyxVoiceOverrides{
		EventID: "evt-ci01",
		CallID:  "call-ci01",
		From:    "+15550001111",
	})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/voice", bytes.NewReader(payload))
	req.Header.Set("Telnyx-Timestamp", "123")
	req.Header.Set("Telnyx-Signature", "abc")
	rec := httptest.NewRecorder()

	handler.HandleVoice(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if conv.startCalls != 1 {
		t.Fatalf("expected conversation start enqueued once, got %d", conv.startCalls)
	}
	if telnyxStub.sendCalls != 1 {
		t.Fatalf("expected exactly one outbound SMS, got %d", telnyxStub.sendCalls)
	}
	if telnyxStub.lastSendReq == nil || telnyxStub.lastSendReq.Body != messaging.InstantAckMessage {
		t.Fatalf("expected missed-call intro SMS, got %#v", telnyxStub.lastSendReq)
	}
	list, err := leadsRepo.ListByOrg(context.Background(), clinicID.String(), leads.ListLeadsFilter{})
	if err != nil {
		t.Fatalf("list leads: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 lead, got %d", len(list))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestTierA_CI02_InboundSMS_LeadAndAI(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()

	store := messaging.NewStore(mock)
	processed := &stubProcessedTracker{}
	telnyxStub := &testTelnyxClient{}
	leadsRepo := leads.NewInMemoryRepository()

	queue := conversation.NewMemoryQueue(8)
	jobs := &stubJobStore{}
	publisher := conversation.NewPublisher(queue, jobs, logging.Default())

	ai := &scriptedConversationService{reply: "stubbed-ai-reply"}
	messenger := &stubReplyMessenger{}
	worker := conversation.NewWorker(ai, queue, jobs, messenger, nil, logging.Default(), conversation.WithWorkerCount(1), conversation.WithReceiveBatchSize(1), conversation.WithReceiveWaitSeconds(0))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	handler := NewTelnyxWebhookHandler(TelnyxWebhookConfig{
		Store:            store,
		Processed:        processed,
		Telnyx:           telnyxStub,
		Conversation:     publisher,
		Leads:            leadsRepo,
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

	waitForCondition(t, 2*time.Second, func() bool {
		return messenger.callCount() == 1
	})
	cancel()
	worker.Wait()

	if ai.messageCalls() != 1 {
		t.Fatalf("expected AI processor called once, got %d", ai.messageCalls())
	}
	if messenger.last().Body != "stubbed-ai-reply" {
		t.Fatalf("unexpected outbound reply: %#v", messenger.last())
	}
	if telnyxStub.sendCalls != 1 || telnyxStub.lastSendReq == nil || !messaging.IsSmsAckMessage(telnyxStub.lastSendReq.Body) {
		t.Fatalf("expected 1 SMS ack, got calls=%d last=%#v", telnyxStub.sendCalls, telnyxStub.lastSendReq)
	}
	list, err := leadsRepo.ListByOrg(context.Background(), clinicID.String(), leads.ListLeadsFilter{})
	if err != nil {
		t.Fatalf("list leads: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 lead, got %d", len(list))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestTierA_CI11_TelcoInboundIdempotency_ByMessageID(t *testing.T) {
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
		WithArgs(clinicID, "+15550001111", "+15559998888", "inbound", "Need info", pgxmock.AnyArg(), "received", "msg-dup", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectExec("INSERT INTO outbox").
		WithArgs(pgxmock.AnyArg(), "clinic:"+clinicID.String(), "messaging.message.received.v1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectQuery("SELECT 1 FROM unsubscribes").
		WithArgs(clinicID, "+15550001111").
		WillReturnRows(pgxmock.NewRows([]string{"exists"}))
	mock.ExpectCommit()

	first := loadTelnyxInboundFixture(t, "telnyx_inbound_message.json", telnyxInboundOverrides{
		EventID:   "evt-1",
		MessageID: "msg-dup",
	})
	req1 := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/messages", bytes.NewReader(first))
	req1.Header.Set("Telnyx-Timestamp", "123")
	req1.Header.Set("Telnyx-Signature", "abc")
	rec1 := httptest.NewRecorder()
	handler.HandleMessages(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec1.Code)
	}

	// Second webhook delivery with a different event id but the same Telnyx message id should be ignored.
	second := loadTelnyxInboundFixture(t, "telnyx_inbound_message.json", telnyxInboundOverrides{
		EventID:   "evt-2",
		MessageID: "msg-dup",
	})
	req2 := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/messages", bytes.NewReader(second))
	req2.Header.Set("Telnyx-Timestamp", "123")
	req2.Header.Set("Telnyx-Signature", "abc")
	rec2 := httptest.NewRecorder()
	handler.HandleMessages(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec2.Code)
	}

	if telnyxStub.sendCalls != 1 {
		t.Fatalf("expected exactly 1 SMS sent, got %d", telnyxStub.sendCalls)
	}
	if conv.calls != 1 {
		t.Fatalf("expected exactly 1 conversation enqueue, got %d", conv.calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestTierA_CI14_PCIGuardrail(t *testing.T) {
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
		WithArgs(clinicID, "+15550001111", "+15559998888", "inbound", "My card is [REDACTED_CARD_1111]", pgxmock.AnyArg(), "received", "msg-pci", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectExec("INSERT INTO outbox").
		WithArgs(pgxmock.AnyArg(), "clinic:"+clinicID.String(), "messaging.message.received.v1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectQuery("SELECT 1 FROM unsubscribes").
		WithArgs(clinicID, "+15550001111").
		WillReturnRows(pgxmock.NewRows([]string{"exists"}))
	mock.ExpectCommit()

	pci := loadTelnyxInboundFixture(t, "telnyx_inbound_message.json", telnyxInboundOverrides{
		EventID:     "evt-pci",
		MessageID:   "msg-pci",
		MessageText: "My card is 4111 1111 1111 1111",
	})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/messages", bytes.NewReader(pci))
	req.Header.Set("Telnyx-Timestamp", "123")
	req.Header.Set("Telnyx-Signature", "abc")
	rec := httptest.NewRecorder()
	handler.HandleMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if conv.calls != 0 {
		t.Fatalf("expected no conversation dispatch on PCI guardrail, got %d", conv.calls)
	}
	if telnyxStub.sendCalls != 1 || telnyxStub.lastSendReq == nil || telnyxStub.lastSendReq.Body != messaging.PCIGuardrailMessage {
		t.Fatalf("expected PCI guardrail SMS, got calls=%d last=%#v", telnyxStub.sendCalls, telnyxStub.lastSendReq)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestTierA_CI15_CI16_StopStartCompliance(t *testing.T) {
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
		StopAck:          "STOP ACK",
	})

	clinicID := uuid.New()

	// 1) STOP -> record unsubscribe + send stop ack.
	mock.ExpectQuery("SELECT clinic_id").
		WithArgs("+15559998888").
		WillReturnRows(pgxmock.NewRows([]string{"clinic_id"}).AddRow(clinicID))
	mock.ExpectQuery("SELECT 1 FROM messages").
		WithArgs(clinicID, "+15550001111", "+15559998888").
		WillReturnRows(pgxmock.NewRows([]string{"exists"}))
	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO messages").
		WithArgs(clinicID, "+15550001111", "+15559998888", "inbound", "STOP", pgxmock.AnyArg(), "received", "msg-stop", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectExec("INSERT INTO outbox").
		WithArgs(pgxmock.AnyArg(), "clinic:"+clinicID.String(), "messaging.message.received.v1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("INSERT INTO unsubscribes").
		WithArgs(clinicID, "+15550001111", "STOP").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	stop := loadTelnyxInboundFixture(t, "telnyx_inbound_stop.json", telnyxInboundOverrides{
		EventID:   "evt-stop",
		MessageID: "msg-stop",
	})
	reqStop := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/messages", bytes.NewReader(stop))
	reqStop.Header.Set("Telnyx-Timestamp", "123")
	reqStop.Header.Set("Telnyx-Signature", "abc")
	recStop := httptest.NewRecorder()
	handler.HandleMessages(recStop, reqStop)
	if recStop.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recStop.Code)
	}
	if telnyxStub.sendCalls != 1 || telnyxStub.lastSendReq == nil || telnyxStub.lastSendReq.Body != "STOP ACK" {
		t.Fatalf("expected STOP ack SMS, got calls=%d last=%#v", telnyxStub.sendCalls, telnyxStub.lastSendReq)
	}
	if conv.calls != 0 {
		t.Fatalf("expected no conversation enqueue on STOP, got %d", conv.calls)
	}

	// 2) User is unsubscribed -> inbound message should be suppressed (no SMS, no conversation dispatch).
	mock.ExpectQuery("SELECT clinic_id").
		WithArgs("+15559998888").
		WillReturnRows(pgxmock.NewRows([]string{"clinic_id"}).AddRow(clinicID))
	mock.ExpectQuery("SELECT 1 FROM messages").
		WithArgs(clinicID, "+15550001111", "+15559998888").
		WillReturnRows(pgxmock.NewRows([]string{"exists"}))
	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO messages").
		WithArgs(clinicID, "+15550001111", "+15559998888", "inbound", "hello?", pgxmock.AnyArg(), "received", "msg-ignored", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectExec("INSERT INTO outbox").
		WithArgs(pgxmock.AnyArg(), "clinic:"+clinicID.String(), "messaging.message.received.v1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectQuery("SELECT 1 FROM unsubscribes").
		WithArgs(clinicID, "+15550001111").
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(1))
	mock.ExpectCommit()

	unsub := loadTelnyxInboundFixture(t, "telnyx_inbound_message.json", telnyxInboundOverrides{
		EventID:     "evt-unsub",
		MessageID:   "msg-ignored",
		MessageText: "hello?",
	})
	reqUnsub := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/messages", bytes.NewReader(unsub))
	reqUnsub.Header.Set("Telnyx-Timestamp", "123")
	reqUnsub.Header.Set("Telnyx-Signature", "abc")
	recUnsub := httptest.NewRecorder()
	handler.HandleMessages(recUnsub, reqUnsub)
	if recUnsub.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recUnsub.Code)
	}
	if telnyxStub.sendCalls != 1 {
		t.Fatalf("expected no new SMS while opted out, got %d total", telnyxStub.sendCalls)
	}
	if conv.calls != 0 {
		t.Fatalf("expected no conversation enqueue while opted out, got %d", conv.calls)
	}

	// 3) START -> deletes unsubscribe and sends opt-in confirmation (no conversation dispatch).
	mock.ExpectQuery("SELECT clinic_id").
		WithArgs("+15559998888").
		WillReturnRows(pgxmock.NewRows([]string{"clinic_id"}).AddRow(clinicID))
	mock.ExpectQuery("SELECT 1 FROM messages").
		WithArgs(clinicID, "+15550001111", "+15559998888").
		WillReturnRows(pgxmock.NewRows([]string{"exists"}))
	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO messages").
		WithArgs(clinicID, "+15550001111", "+15559998888", "inbound", "START", pgxmock.AnyArg(), "received", "msg-start", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectExec("INSERT INTO outbox").
		WithArgs(pgxmock.AnyArg(), "clinic:"+clinicID.String(), "messaging.message.received.v1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("DELETE FROM unsubscribes").
		WithArgs(clinicID, "+15550001111").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectCommit()

	start := loadTelnyxInboundFixture(t, "telnyx_inbound_stop.json", telnyxInboundOverrides{
		EventID:     "evt-start",
		MessageID:   "msg-start",
		MessageText: "START",
	})
	reqStart := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/messages", bytes.NewReader(start))
	reqStart.Header.Set("Telnyx-Timestamp", "123")
	reqStart.Header.Set("Telnyx-Signature", "abc")
	recStart := httptest.NewRecorder()
	handler.HandleMessages(recStart, reqStart)
	if recStart.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recStart.Code)
	}
	if telnyxStub.sendCalls != 2 || telnyxStub.lastSendReq == nil || telnyxStub.lastSendReq.Body != "You're opted back in. Reply STOP to opt out." {
		t.Fatalf("expected START opt-in ack, got calls=%d last=%#v", telnyxStub.sendCalls, telnyxStub.lastSendReq)
	}
	if conv.calls != 0 {
		t.Fatalf("expected no conversation enqueue on START, got %d", conv.calls)
	}

	// 4) After START, normal inbound message should proceed again.
	mock.ExpectQuery("SELECT clinic_id").
		WithArgs("+15559998888").
		WillReturnRows(pgxmock.NewRows([]string{"clinic_id"}).AddRow(clinicID))
	mock.ExpectQuery("SELECT 1 FROM messages").
		WithArgs(clinicID, "+15550001111", "+15559998888").
		WillReturnRows(pgxmock.NewRows([]string{"exists"}))
	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO messages").
		WithArgs(clinicID, "+15550001111", "+15559998888", "inbound", "book botox", pgxmock.AnyArg(), "received", "msg-after-start", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectExec("INSERT INTO outbox").
		WithArgs(pgxmock.AnyArg(), "clinic:"+clinicID.String(), "messaging.message.received.v1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectQuery("SELECT 1 FROM unsubscribes").
		WithArgs(clinicID, "+15550001111").
		WillReturnRows(pgxmock.NewRows([]string{"exists"}))
	mock.ExpectCommit()

	after := loadTelnyxInboundFixture(t, "telnyx_inbound_message.json", telnyxInboundOverrides{
		EventID:     "evt-after-start",
		MessageID:   "msg-after-start",
		MessageText: "book botox",
	})
	reqAfter := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/messages", bytes.NewReader(after))
	reqAfter.Header.Set("Telnyx-Timestamp", "123")
	reqAfter.Header.Set("Telnyx-Signature", "abc")
	recAfter := httptest.NewRecorder()
	handler.HandleMessages(recAfter, reqAfter)
	if recAfter.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recAfter.Code)
	}
	if conv.calls != 1 {
		t.Fatalf("expected conversation enqueue after START, got %d", conv.calls)
	}
	if telnyxStub.sendCalls != 3 || telnyxStub.lastSendReq == nil || !messaging.IsSmsAckMessage(telnyxStub.lastSendReq.Body) {
		t.Fatalf("expected ack SMS after START, got calls=%d last=%#v", telnyxStub.sendCalls, telnyxStub.lastSendReq)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestTierA_CI17_TelnyxSignatureFailure(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()

	telnyxStub := &testTelnyxClient{verifyErr: context.Canceled}
	handler := NewTelnyxWebhookHandler(TelnyxWebhookConfig{
		Store:     messaging.NewStore(mock),
		Processed: &stubProcessedTracker{},
		Telnyx:    telnyxStub,
		Logger:    logging.Default(),
	})

	req := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/messages", bytes.NewReader(loadFixture(t, "telnyx_inbound_message.json")))
	rec := httptest.NewRecorder()
	handler.HandleMessages(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if telnyxStub.sendCalls != 0 {
		t.Fatalf("expected no SMS on signature failure, got %d", telnyxStub.sendCalls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected db interactions: %v", err)
	}
}

func TestTierA_CI19_UnifiedLeadIdentity_VoiceThenSMS(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()

	store := messaging.NewStore(mock)
	processed := &stubProcessedTracker{}
	telnyxStub := &testTelnyxClient{}
	conv := &stubConversationPublisher{}
	leadsRepo := leads.NewInMemoryRepository()

	handler := NewTelnyxWebhookHandler(TelnyxWebhookConfig{
		Store:            store,
		Processed:        processed,
		Telnyx:           telnyxStub,
		Conversation:     conv,
		Leads:            leadsRepo,
		Logger:           logging.Default(),
		MessagingProfile: "profile",
	})

	clinicID := uuid.New()

	// Voice missed call creates lead.
	mock.ExpectQuery("SELECT clinic_id").
		WithArgs("+15559998888").
		WillReturnRows(pgxmock.NewRows([]string{"clinic_id"}).AddRow(clinicID))

	voice := loadTelnyxVoiceFixture(t, "telnyx_voice_missed.json", telnyxVoiceOverrides{
		EventID: "evt-voice",
		CallID:  "call-1",
		From:    "+15550003333",
	})
	reqVoice := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/voice", bytes.NewReader(voice))
	reqVoice.Header.Set("Telnyx-Timestamp", "123")
	reqVoice.Header.Set("Telnyx-Signature", "abc")
	recVoice := httptest.NewRecorder()
	handler.HandleVoice(recVoice, reqVoice)
	if recVoice.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recVoice.Code)
	}

	list, err := leadsRepo.ListByOrg(context.Background(), clinicID.String(), leads.ListLeadsFilter{})
	if err != nil {
		t.Fatalf("list leads: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 lead, got %d", len(list))
	}
	leadID := list[0].ID

	// Inbound SMS from the same number should attach to the existing lead (no duplicates).
	mock.ExpectQuery("SELECT clinic_id").
		WithArgs("+15559998888").
		WillReturnRows(pgxmock.NewRows([]string{"clinic_id"}).AddRow(clinicID))
	mock.ExpectQuery("SELECT 1 FROM messages").
		WithArgs(clinicID, "+15550003333", "+15559998888").
		WillReturnRows(pgxmock.NewRows([]string{"exists"}))
	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO messages").
		WithArgs(clinicID, "+15550003333", "+15559998888", "inbound", "Need info", pgxmock.AnyArg(), "received", "msg-unified", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectExec("INSERT INTO outbox").
		WithArgs(pgxmock.AnyArg(), "clinic:"+clinicID.String(), "messaging.message.received.v1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectQuery("SELECT 1 FROM unsubscribes").
		WithArgs(clinicID, "+15550003333").
		WillReturnRows(pgxmock.NewRows([]string{"exists"}))
	mock.ExpectCommit()

	sms := loadTelnyxInboundFixture(t, "telnyx_inbound_message.json", telnyxInboundOverrides{
		EventID:   "evt-sms",
		MessageID: "msg-unified",
		From:      "+15550003333",
	})
	reqSMS := httptest.NewRequest(http.MethodPost, "/webhooks/telnyx/messages", bytes.NewReader(sms))
	reqSMS.Header.Set("Telnyx-Timestamp", "123")
	reqSMS.Header.Set("Telnyx-Signature", "abc")
	recSMS := httptest.NewRecorder()
	handler.HandleMessages(recSMS, reqSMS)
	if recSMS.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recSMS.Code)
	}

	list2, err := leadsRepo.ListByOrg(context.Background(), clinicID.String(), leads.ListLeadsFilter{})
	if err != nil {
		t.Fatalf("list leads: %v", err)
	}
	if len(list2) != 1 {
		t.Fatalf("expected no duplicate leads, got %d", len(list2))
	}
	if conv.calls != 1 {
		t.Fatalf("expected conversation enqueue once, got %d", conv.calls)
	}
	if conv.last.LeadID != leadID {
		t.Fatalf("expected sms attached to existing lead, got %s want %s", conv.last.LeadID, leadID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

type stubJobStore struct{}

func (s *stubJobStore) PutPending(ctx context.Context, job *conversation.JobRecord) error { return nil }

func (s *stubJobStore) GetJob(ctx context.Context, jobID string) (*conversation.JobRecord, error) {
	return nil, conversation.ErrJobNotFound
}

func (s *stubJobStore) MarkCompleted(ctx context.Context, jobID string, resp *conversation.Response, conversationID string) error {
	return nil
}

func (s *stubJobStore) MarkFailed(ctx context.Context, jobID string, errMsg string) error { return nil }

type scriptedConversationService struct {
	reply string

	mu           sync.Mutex
	messageCount int
}

func (s *scriptedConversationService) StartConversation(ctx context.Context, req conversation.StartRequest) (*conversation.Response, error) {
	return &conversation.Response{}, nil
}

func (s *scriptedConversationService) ProcessMessage(ctx context.Context, req conversation.MessageRequest) (*conversation.Response, error) {
	s.mu.Lock()
	s.messageCount++
	s.mu.Unlock()
	return &conversation.Response{
		ConversationID: req.ConversationID,
		Message:        s.reply,
		Timestamp:      time.Now().UTC(),
	}, nil
}

func (s *scriptedConversationService) GetHistory(ctx context.Context, conversationID string) ([]conversation.Message, error) {
	return []conversation.Message{}, nil
}

func (s *scriptedConversationService) messageCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.messageCount
}

type stubReplyMessenger struct {
	mu    sync.Mutex
	calls int
	lastR conversation.OutboundReply
}

func (s *stubReplyMessenger) SendReply(ctx context.Context, reply conversation.OutboundReply) error {
	s.mu.Lock()
	s.calls++
	s.lastR = reply
	s.mu.Unlock()
	return nil
}

func (s *stubReplyMessenger) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *stubReplyMessenger) last() conversation.OutboundReply {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastR
}

func waitForCondition(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

type telnyxInboundOverrides struct {
	EventID     string
	MessageID   string
	MessageText string
	From        string
}

func loadTelnyxInboundFixture(t *testing.T, name string, o telnyxInboundOverrides) []byte {
	t.Helper()

	// Use the canonical wrapper shape used in fixtures.
	var wrapper struct {
		Data struct {
			ID        string `json:"id"`
			EventType string `json:"event_type"`
			Occurred  string `json:"occurred_at"`
			Payload   struct {
				ID        string   `json:"id"`
				Direction string   `json:"direction"`
				Text      string   `json:"text"`
				MediaURLs []string `json:"media_urls"`
				Status    string   `json:"status"`
				From      struct {
					PhoneNumber string `json:"phone_number"`
				} `json:"from"`
				To []struct {
					PhoneNumber string `json:"phone_number"`
				} `json:"to"`
			} `json:"payload"`
		} `json:"data"`
	}

	raw := loadFixture(t, name)
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		t.Fatalf("unmarshal telnyx fixture: %v", err)
	}
	if o.EventID != "" {
		wrapper.Data.ID = o.EventID
	}
	if o.MessageID != "" {
		wrapper.Data.Payload.ID = o.MessageID
	}
	if o.MessageText != "" {
		wrapper.Data.Payload.Text = o.MessageText
	}
	if o.From != "" {
		wrapper.Data.Payload.From.PhoneNumber = o.From
	}

	out, err := json.Marshal(wrapper)
	if err != nil {
		t.Fatalf("marshal telnyx fixture: %v", err)
	}
	return out
}

type telnyxVoiceOverrides struct {
	EventID string
	CallID  string
	From    string
}

func loadTelnyxVoiceFixture(t *testing.T, name string, o telnyxVoiceOverrides) []byte {
	t.Helper()

	var wrapper struct {
		Data struct {
			ID        string `json:"id"`
			EventType string `json:"event_type"`
			Occurred  string `json:"occurred_at"`
			Payload   struct {
				ID          string `json:"id"`
				Status      string `json:"status"`
				HangupCause string `json:"hangup_cause"`
				From        struct {
					PhoneNumber string `json:"phone_number"`
				} `json:"from"`
				To []struct {
					PhoneNumber string `json:"phone_number"`
				} `json:"to"`
			} `json:"payload"`
		} `json:"data"`
	}

	raw := loadFixture(t, name)
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		t.Fatalf("unmarshal voice fixture: %v", err)
	}
	if o.EventID != "" {
		wrapper.Data.ID = o.EventID
	}
	if o.CallID != "" {
		wrapper.Data.Payload.ID = o.CallID
	}
	if o.From != "" {
		wrapper.Data.Payload.From.PhoneNumber = o.From
	}

	out, err := json.Marshal(wrapper)
	if err != nil {
		t.Fatalf("marshal voice fixture: %v", err)
	}
	return out
}
