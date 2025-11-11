package events

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

type stubExec struct {
	args []any
}

type badEvent struct{}

func (badEvent) EventType() string { return "" }

func (s *stubExec) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	s.args = args
	return pgconn.CommandTag{}, nil
}

func TestNewEnvelope(t *testing.T) {
	fixedNow := time.Unix(0, 123456000).UTC()
	prevNow := nowFunc
	nowFunc = func() time.Time { return fixedNow }
	defer func() { nowFunc = prevNow }()

	id := uuid.MustParse("9a20d7d1-bf6a-4d33-bd55-5d25a816f1a8")
	env, err := newEnvelope("clinic:123", "corr-1", MessageReceivedV1{
		MessageID:  "msg-1",
		ClinicID:   "clinic-123",
		FromE164:   "+15550001111",
		ToE164:     "+15552223333",
		Body:       "hi",
		Provider:   "telnyx",
		ReceivedAt: fixedNow,
	}, WithEventID(id))
	if err != nil {
		t.Fatalf("newEnvelope failed: %v", err)
	}
	if env.EventID != id {
		t.Fatalf("expected event id override, got %s", env.EventID)
	}
	if env.TimestampMicros != fixedNow.UnixMicro() {
		t.Fatalf("unexpected timestamp: %d", env.TimestampMicros)
	}
	if env.EventType != "messaging.message.received.v1" {
		t.Fatalf("unexpected type: %s", env.EventType)
	}
	if env.Aggregate != "clinic:123" {
		t.Fatalf("unexpected aggregate: %s", env.Aggregate)
	}
	if len(env.Payload) == 0 {
		t.Fatal("expected payload bytes")
	}
}

func TestAppendCanonicalEvent(t *testing.T) {
	exec := &stubExec{}
	env, err := AppendCanonicalEvent(context.Background(), exec, "clinic:123", "corr-1", HostedOrderActivatedV1{
		OrderID:     "order-1",
		ClinicID:    "clinic-123",
		E164Number:  "+15550007777",
		ActivatedAt: time.Unix(100, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("append canonical failed: %v", err)
	}
	if env.EventID == uuid.Nil {
		t.Fatal("expected generated event id")
	}
	if exec.args == nil || len(exec.args) != 4 {
		t.Fatalf("expected exec args, got %#v", exec.args)
	}
	if exec.args[0] != env.EventID {
		t.Fatalf("id mismatch")
	}
	payloadBytes, ok := exec.args[3].([]byte)
	if !ok {
		t.Fatalf("payload arg type %T", exec.args[3])
	}
	var stored Envelope
	if err := json.Unmarshal(payloadBytes, &stored); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if stored.EventType != env.EventType || stored.Aggregate != env.Aggregate {
		t.Fatalf("stored envelope mismatch: %#v", stored)
	}
	if string(stored.Payload) == "" {
		t.Fatal("expected nested payload")
	}
}

func TestEnvelopeValidation(t *testing.T) {
	if _, err := newEnvelope("", "", MessageSentV1{}); err == nil {
		t.Fatal("expected aggregate error")
	}
	if _, err := newEnvelope("agg", "", nil); err == nil {
		t.Fatal("expected nil event error")
	}
	if _, err := newEnvelope("agg", "", badEvent{}); err == nil {
		t.Fatal("expected event type error")
	}
}

func TestWithTimestampOption(t *testing.T) {
	target := time.Unix(50, 123000).UTC()
	env, err := newEnvelope("agg", "", MessageReceivedV1{MessageID: "x"}, WithTimestamp(target))
	if err != nil {
		t.Fatalf("newEnvelope: %v", err)
	}
	if env.TimestampMicros != target.UnixMicro() {
		t.Fatalf("expected timestamp override, got %d", env.TimestampMicros)
	}
}

func TestAppendCanonicalEventRequiresExec(t *testing.T) {
	if _, err := AppendCanonicalEvent(context.Background(), nil, "agg", "", MessageSentV1{MessageID: "x"}); err == nil {
		t.Fatal("expected exec error")
	}
}
