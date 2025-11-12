package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	pgxmock "github.com/pashagolub/pgxmock/v4"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/compliance"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/telnyxclient"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestAdminSendMessageSuccess(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := messaging.NewStore(mock)
	handler := NewAdminMessagingHandler(AdminMessagingConfig{
		Store:            store,
		Logger:           logging.Default(),
		Telnyx:           &testTelnyxClient{sendResp: &telnyxclient.MessageResponse{ID: "msg_1", Status: "queued"}},
		MessagingProfile: "profile",
		RetryBaseDelay:   time.Minute,
	})

	clinicID := uuid.New()
	mock.ExpectQuery("SELECT 1 FROM unsubscribes").
		WithArgs(clinicID, "+15555550100").
		WillReturnError(pgx.ErrNoRows)
	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO messages").
		WithArgs(clinicID, "+1999", "+15555550100", "outbound", "hello", pgxmock.AnyArg(), "queued", "msg_1", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectExec("INSERT INTO outbox").
		WithArgs(pgxmock.AnyArg(), "clinic:"+clinicID.String(), "messaging.message.sent.v1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	body := []byte(`{"clinic_id":"` + clinicID.String() + `","from":"+1999","to":"+1 (555) 555-0100","body":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/messages:send", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.SendMessage(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestAdminSendMessageSuppressedByOptOut(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := messaging.NewStore(mock)
	handler := NewAdminMessagingHandler(AdminMessagingConfig{
		Store:            store,
		Logger:           logging.Default(),
		Telnyx:           &testTelnyxClient{},
		MessagingProfile: "profile",
		RetryBaseDelay:   time.Minute,
	})

	clinicID := uuid.New()
	mock.ExpectQuery("SELECT 1 FROM unsubscribes").
		WithArgs(clinicID, "+15555550100").
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow(1))
	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO messages").
		WithArgs(clinicID, "+1999", "+15555550100", "outbound", "hello", pgxmock.AnyArg(), "suppressed", "", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectExec("INSERT INTO outbox").
		WithArgs(pgxmock.AnyArg(), "clinic:"+clinicID.String(), "messaging.message.sent.v1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	body := []byte(`{"clinic_id":"` + clinicID.String() + `","from":"+1999","to":"+1 (555) 555-0100","body":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/messages:send", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.SendMessage(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestStartHostedOrderSuccess(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := messaging.NewStore(mock)
	telnyx := &testTelnyxClient{}
	handler := NewAdminMessagingHandler(AdminMessagingConfig{
		Store:  store,
		Logger: logging.Default(),
		Telnyx: telnyx,
	})
	clinicID := uuid.New()
	mock.ExpectExec("INSERT INTO hosted_number_orders").
		WithArgs(pgxmock.AnyArg(), clinicID, "+15550001234", "pending", "", "hno_test").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	body := []byte(`{"clinic_id":"` + clinicID.String() + `","phone_number":"+15550001234","billing_number":"+1888","contact_name":"Alice","contact_email":"ops@example.com","contact_phone":"+1222"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/hosted/orders", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.StartHostedOrder(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestStartHostedOrderEligibilityError(t *testing.T) {
	mock, _ := pgxmock.NewPool()
	defer mock.Close()
	store := messaging.NewStore(mock)
	telnyx := &testTelnyxClient{checkErr: errors.New("not eligible")}
	handler := NewAdminMessagingHandler(AdminMessagingConfig{
		Store:  store,
		Logger: logging.Default(),
		Telnyx: telnyx,
	})
	body := []byte(`{"clinic_id":"` + uuid.NewString() + `","phone_number":"+1555","contact_name":"Alice","contact_email":"ops@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/hosted/orders", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.StartHostedOrder(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateBrandPersistError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := messaging.NewStore(mock)
	handler := NewAdminMessagingHandler(AdminMessagingConfig{
		Store:  store,
		Logger: logging.Default(),
		Telnyx: &testTelnyxClient{},
	})
	clinicID := uuid.New()
	mock.ExpectExec("INSERT INTO ten_dlc_brands").
		WithArgs(pgxmock.AnyArg(), clinicID, "Clinic", "B123", "approved", "Alice", "ops@example.com", "+1555").
		WillReturnError(errors.New("db down"))

	body := []byte(`{"clinic_id":"` + clinicID.String() + `","legal_name":"Clinic","website":"https://clinic","address_line":"1","city":"SF","state":"CA","postal_code":"94105","country":"US","contact_name":"Alice","contact_email":"ops@example.com","contact_phone":"+1555","vertical":"hc"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/10dlc/brands", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.CreateBrand(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestCreateCampaignTelnyxError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := messaging.NewStore(mock)
	telnyx := &testTelnyxClient{campaignErr: errors.New("telnyx down")}
	handler := NewAdminMessagingHandler(AdminMessagingConfig{
		Store:  store,
		Logger: logging.Default(),
		Telnyx: telnyx,
	})
	body := []byte(`{"brand_internal_id":"` + uuid.NewString() + `","use_case":"alerts","sample_messages":["hi"],"help_message":"help","stop_message":"stop"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/10dlc/campaigns", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.CreateCampaign(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
}

func TestSendMessageTemplateError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := messaging.NewStore(mock)
	handler := NewAdminMessagingHandler(AdminMessagingConfig{
		Store:            store,
		Logger:           logging.Default(),
		Telnyx:           &testTelnyxClient{},
		MessagingProfile: "profile",
	})
	req := httptest.NewRequest(http.MethodPost, "/admin/messages:send", bytes.NewReader([]byte(`{"clinic_id":"`+uuid.NewString()+`","from":"+1","to":"+1","template":"{{"}`)))
	rec := httptest.NewRecorder()
	handler.SendMessage(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSendMessageQuietHoursSuppression(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := messaging.NewStore(mock)
	now := time.Now().UTC()
	start := now.Add(-2 * time.Minute).Format("15:04")
	end := now.Add(2 * time.Minute).Format("15:04")
	qh, _ := compliance.ParseQuietHours(start, end, "UTC")
	clinicID := uuid.New()
	mock.ExpectQuery("SELECT 1 FROM unsubscribes").
		WithArgs(clinicID, "+15555550100").
		WillReturnError(pgx.ErrNoRows)
	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO messages").
		WithArgs(clinicID, "+1999", "+15555550100", "outbound", "hi", pgxmock.AnyArg(), "suppressed", "", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectExec("INSERT INTO outbox").
		WithArgs(pgxmock.AnyArg(), "clinic:"+clinicID.String(), "messaging.message.sent.v1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	handler := NewAdminMessagingHandler(AdminMessagingConfig{
		Store:             store,
		Logger:            logging.Default(),
		Telnyx:            &testTelnyxClient{},
		MessagingProfile:  "profile",
		QuietHours:        qh,
		QuietHoursEnabled: true,
	})
	body := []byte(`{"clinic_id":"` + clinicID.String() + `","from":"+1999","to":"+1 (555) 555-0100","body":"hi"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/messages:send", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.SendMessage(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if resp["suppressed_reason"] != "quiet_hours" {
		t.Fatalf("expected quiet_hours suppression, got %v", resp["suppressed_reason"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestSendMessageSchedulesRetryOnTelnyxError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := messaging.NewStore(mock)
	clinicID := uuid.New()
	mock.ExpectQuery("SELECT 1 FROM unsubscribes").
		WithArgs(clinicID, "+15555550100").
		WillReturnError(pgx.ErrNoRows)
	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO messages").
		WithArgs(clinicID, "+1999", "+15555550100", "outbound", "hi", pgxmock.AnyArg(), "retry_pending", "", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(uuid.New()))
	mock.ExpectExec("INSERT INTO outbox").
		WithArgs(pgxmock.AnyArg(), "clinic:"+clinicID.String(), "messaging.message.sent.v1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	handler := NewAdminMessagingHandler(AdminMessagingConfig{
		Store:            store,
		Logger:           logging.Default(),
		Telnyx:           &testTelnyxClient{sendErr: errors.New("telnyx down")},
		MessagingProfile: "profile",
		RetryBaseDelay:   time.Minute,
	})
	body := []byte(`{"clinic_id":"` + clinicID.String() + `","from":"+1999","to":"+1 (555) 555-0100","body":"hi"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/messages:send", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.SendMessage(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestSendMessageUnsubscribeCheckError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()
	store := messaging.NewStore(mock)
	handler := NewAdminMessagingHandler(AdminMessagingConfig{
		Store:            store,
		Logger:           logging.Default(),
		Telnyx:           &testTelnyxClient{},
		MessagingProfile: "profile",
	})
	clinicID := uuid.New()
	mock.ExpectQuery("SELECT 1 FROM unsubscribes").
		WithArgs(clinicID, "+15555550100").
		WillReturnError(errors.New("db down"))

	body := []byte(`{"clinic_id":"` + clinicID.String() + `","from":"+1999","to":"+1 (555) 555-0100","body":"hi"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/messages:send", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.SendMessage(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}
