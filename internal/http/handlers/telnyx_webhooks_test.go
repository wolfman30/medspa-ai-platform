package handlers

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	pgxmock "github.com/pashagolub/pgxmock/v4"
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
	handler := NewTelnyxWebhookHandler(TelnyxWebhookConfig{
		Store:            store,
		Processed:        processed,
		Telnyx:           telnyxStub,
		Logger:           logging.Default(),
		MessagingProfile: "profile",
		StopAck:          "STOP ACK",
	})

	clinicID := uuid.New()
	mock.ExpectQuery("SELECT clinic_id").
		WithArgs("+15559998888").
		WillReturnRows(pgxmock.NewRows([]string{"clinic_id"}).AddRow(clinicID))
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
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
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
	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO messages").
		WithArgs(clinicID, "+15550001111", "+15559998888", "inbound", "HELP", pgxmock.AnyArg(), "received", "msg_inbound", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectExec("INSERT INTO outbox").
		WithArgs(pgxmock.AnyArg(), "clinic:"+clinicID.String(), "messaging.message.received.v1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
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
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}
