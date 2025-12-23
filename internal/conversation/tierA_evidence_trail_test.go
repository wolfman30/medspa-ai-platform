package conversation

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestTierA_CI20_EvidenceTrailCorrelation_OutboxDispatcher(t *testing.T) {
	queue := &stubQueue{}
	jobs := &stubJobRecorder{}
	publisher := NewPublisher(queue, jobs, logging.Default())
	dispatcher := NewOutboxDispatcher(publisher)

	now := time.Now().UTC().Truncate(time.Second)
	evt := events.MessageReceivedV1{
		MessageID:     "msg-1",
		ClinicID:      "clinic-1",
		FromE164:      "+15550001111",
		ToE164:        "+15559998888",
		Body:          "Need info",
		Provider:      "telnyx",
		ReceivedAt:    now,
		TelnyxEventID: "evt-1",
	}
	payload, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal message event: %v", err)
	}
	env := events.Envelope{
		EventID:         uuid.New(),
		EventType:       evt.EventType(),
		Aggregate:       "clinic:clinic-1",
		TimestampMicros: now.UnixMicro(),
		CorrelationID:   "corr-123",
		Payload:         payload,
	}
	envJSON, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	entry := events.OutboxEntry{
		ID:        uuid.New(),
		Aggregate: env.Aggregate,
		EventType: env.EventType,
		Payload:   envJSON,
		CreatedAt: now,
	}

	if err := dispatcher.Handle(context.Background(), entry); err != nil {
		t.Fatalf("dispatcher handle: %v", err)
	}

	if len(queue.sent) != 1 {
		t.Fatalf("expected 1 enqueued message, got %d", len(queue.sent))
	}
	var qp queuePayload
	if err := json.Unmarshal([]byte(queue.sent[0]), &qp); err != nil {
		t.Fatalf("decode queued payload: %v", err)
	}
	if qp.ID != "corr-123" {
		t.Fatalf("expected correlation ID used as job id, got %q", qp.ID)
	}
	if qp.Kind != jobTypeMessage {
		t.Fatalf("expected jobTypeMessage, got %q", qp.Kind)
	}
	if qp.Message.OrgID != evt.ClinicID || qp.Message.From != evt.FromE164 || qp.Message.To != evt.ToE164 {
		t.Fatalf("unexpected message mapping: %#v", qp.Message)
	}
	if qp.Message.Message != evt.Body {
		t.Fatalf("expected body propagated, got %q", qp.Message.Message)
	}
	if len(jobs.jobs) != 1 || jobs.jobs[0].JobID != "corr-123" {
		t.Fatalf("expected job record stored with correlation id, got %#v", jobs.jobs)
	}
}
