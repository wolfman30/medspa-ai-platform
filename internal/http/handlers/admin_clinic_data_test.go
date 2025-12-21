package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	pgxmock "github.com/pashagolub/pgxmock/v4"
	"github.com/redis/go-redis/v9"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestAdminClinicDataHandler_PurgePhone_DeletesRowsAndRedisKey(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()

	mr := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	orgID := uuid.New().String()
	orgUUID := uuid.MustParse(orgID)
	phone := "9378962713"
	digits := "19378962713"
	e164 := "+19378962713"
	conversationID := "sms:" + orgID + ":" + digits
	redisKey := "conversation:" + conversationID

	if err := redisClient.Set(context.Background(), redisKey, "[]", 0).Err(); err != nil {
		t.Fatalf("seed redis: %v", err)
	}

	handler := NewAdminClinicDataHandler(AdminClinicDataConfig{
		DB:     mock,
		Redis:  redisClient,
		Logger: logging.Default(),
	})

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM conversation_jobs").WithArgs(conversationID).WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec("DELETE FROM outbox").WithArgs(orgID, digits).WillReturnResult(pgxmock.NewResult("DELETE", 2))
	mock.ExpectExec("DELETE FROM payments").WithArgs(orgID, digits).WillReturnResult(pgxmock.NewResult("DELETE", 3))
	mock.ExpectExec("DELETE FROM bookings").WithArgs(orgID, digits).WillReturnResult(pgxmock.NewResult("DELETE", 4))
	mock.ExpectExec("DELETE FROM leads").WithArgs(orgID, digits).WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec("DELETE FROM messages").WithArgs(orgUUID, e164).WillReturnResult(pgxmock.NewResult("DELETE", 5))
	mock.ExpectExec("DELETE FROM unsubscribes").WithArgs(orgUUID, e164).WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectCommit()

	req := httptest.NewRequest(http.MethodDelete, "/admin/clinics/"+orgID+"/phones/"+phone, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("orgID", orgID)
	rctx.URLParams.Add("phone", phone)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	handler.PurgePhone(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp PurgePhoneResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.PhoneDigits != digits {
		t.Fatalf("expected digits %s, got %s", digits, resp.PhoneDigits)
	}
	if resp.ConversationID != conversationID {
		t.Fatalf("expected conversation_id %s, got %s", conversationID, resp.ConversationID)
	}
	if resp.RedisDeleted != 1 {
		t.Fatalf("expected redis_deleted=1, got %d", resp.RedisDeleted)
	}
	if mr.Exists(redisKey) {
		t.Fatalf("expected redis key deleted: %s", redisKey)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
