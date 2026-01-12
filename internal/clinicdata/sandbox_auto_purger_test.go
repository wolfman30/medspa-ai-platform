package clinicdata

import (
	"context"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	pgxmock "github.com/pashagolub/pgxmock/v4"
	"github.com/redis/go-redis/v9"
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestSandboxAutoPurger_PurgesWhenPhoneMatches(t *testing.T) {
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
	conversationIDE164 := "sms:" + orgID + ":" + e164
	redisKeyDigits := "conversation:" + conversationID
	redisKeyE164 := "conversation:" + conversationIDE164

	if err := redisClient.Set(context.Background(), redisKeyDigits, "[]", 0).Err(); err != nil {
		t.Fatalf("seed redis: %v", err)
	}
	if err := redisClient.Set(context.Background(), redisKeyE164, "[]", 0).Err(); err != nil {
		t.Fatalf("seed redis: %v", err)
	}

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM conversation_jobs").WithArgs(conversationID, conversationIDE164).WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec("DELETE FROM conversation_messages").WithArgs(conversationID, conversationIDE164).WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec("DELETE FROM conversations").WithArgs(orgID, conversationID, conversationIDE164, digits).WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec("DELETE FROM outbox").WithArgs(orgID, digits).WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec("DELETE FROM payments").WithArgs(orgID, digits).WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec("DELETE FROM bookings").WithArgs(orgID, digits).WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec("DELETE FROM callback_promises").WithArgs(orgUUID, digits, orgID).WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec("DELETE FROM escalations").WithArgs(orgUUID, digits, orgID).WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec("DELETE FROM compliance_audit_events").WithArgs(orgUUID, conversationID, conversationIDE164, orgID, digits).WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec("DELETE FROM leads").WithArgs(orgID, digits).WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec("DELETE FROM messages").WithArgs(orgUUID, e164).WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectExec("DELETE FROM unsubscribes").WithArgs(orgUUID, e164).WillReturnResult(pgxmock.NewResult("DELETE", 0))
	mock.ExpectCommit()

	purger := NewPurger(mock, redisClient, logging.Default())
	auto := NewSandboxAutoPurger(purger, AutoPurgeConfig{
		Enabled:            true,
		AllowedPhoneDigits: []string{phone},
		Delay:              0,
	}, logging.Default())

	evt := events.PaymentSucceededV1{
		EventID:      "evt-1",
		OrgID:        orgID,
		LeadID:       uuid.New().String(),
		Provider:     "square",
		ProviderRef:  "pay_123",
		AmountCents:  5000,
		OccurredAt:   time.Now().UTC(),
		LeadPhone:    e164,
		FromNumber:   "+15550000000",
		ScheduledFor: nil,
	}

	if err := auto.MaybePurgeAfterPayment(context.Background(), evt); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if mr.Exists(redisKeyDigits) || mr.Exists(redisKeyE164) {
		t.Fatalf("expected redis conversation keys deleted")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestSandboxAutoPurger_SkipsWhenPhoneDoesNotMatch(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock: %v", err)
	}
	defer mock.Close()

	purger := NewPurger(mock, nil, logging.Default())
	auto := NewSandboxAutoPurger(purger, AutoPurgeConfig{
		Enabled:            true,
		AllowedPhoneDigits: []string{"15550001111"},
		Delay:              0,
	}, logging.Default())

	evt := events.PaymentSucceededV1{
		EventID:     "evt-1",
		OrgID:       uuid.New().String(),
		LeadID:      uuid.New().String(),
		Provider:    "square",
		ProviderRef: "pay_123",
		LeadPhone:   "+19378962713",
	}

	if err := auto.MaybePurgeAfterPayment(context.Background(), evt); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
