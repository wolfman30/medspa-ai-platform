package conversation

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

type igStubMessenger struct {
	mu      sync.Mutex
	called  bool
	count   int
	last    OutboundReply
	sendErr error
}

func (s *igStubMessenger) SendReply(_ context.Context, reply OutboundReply) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called = true
	s.count++
	s.last = reply
	return s.sendErr
}

func (s *igStubMessenger) wasCalled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.called
}

func (s *igStubMessenger) lastReply() OutboundReply {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

func newTestIGWorker(igm ReplyMessenger) *Worker {
	logger := logging.Default()
	return &Worker{
		igMessenger: igm,
		events:      NewEventLogger(logger),
		logger:      logger,
	}
}

func TestSendInstagramReply_Basic(t *testing.T) {
	igm := &igStubMessenger{}
	w := newTestIGWorker(igm)

	payload := queuePayload{
		ID: "job-ig-1",
		Message: MessageRequest{
			OrgID:          "org_1",
			LeadID:         "lead_1",
			ConversationID: "ig_org_1_user_123",
			Message:        "I want Botox",
			Channel:        ChannelInstagram,
			From:           "user_123",
			To:             "page_456",
		},
	}

	resp := &Response{
		ConversationID: "ig_org_1_user_123",
		Message:        "We have great Botox options!",
		Timestamp:      time.Now().UTC(),
	}

	blocked := w.sendInstagramReply(context.Background(), payload, resp)

	if blocked {
		t.Error("expected blocked=false")
	}
	if !igm.wasCalled() {
		t.Fatal("expected igMessenger.SendReply to be called")
	}
	reply := igm.lastReply()
	if reply.To != "user_123" {
		t.Errorf("reply.To = %s, want user_123", reply.To)
	}
	if reply.Body != "We have great Botox options!" {
		t.Errorf("reply.Body = %s", reply.Body)
	}
	if reply.OrgID != "org_1" {
		t.Errorf("reply.OrgID = %s, want org_1", reply.OrgID)
	}
	if reply.Metadata["channel"] != "instagram" {
		t.Errorf("metadata channel = %s, want instagram", reply.Metadata["channel"])
	}
}

func TestSendInstagramReply_NilResponse(t *testing.T) {
	igm := &igStubMessenger{}
	w := newTestIGWorker(igm)

	payload := queuePayload{ID: "job-ig-2", Message: MessageRequest{From: "user_1", Channel: ChannelInstagram}}

	blocked := w.sendInstagramReply(context.Background(), payload, nil)
	if blocked {
		t.Error("expected blocked=false for nil response")
	}
	if igm.wasCalled() {
		t.Error("should not call messenger for nil response")
	}
}

func TestSendInstagramReply_EmptyFrom(t *testing.T) {
	igm := &igStubMessenger{}
	w := newTestIGWorker(igm)

	payload := queuePayload{ID: "job-ig-3", Message: MessageRequest{From: "", Channel: ChannelInstagram}}
	resp := &Response{Message: "hello", Timestamp: time.Now()}

	blocked := w.sendInstagramReply(context.Background(), payload, resp)
	if blocked {
		t.Error("expected blocked=false for empty from")
	}
	if igm.wasCalled() {
		t.Error("should not call messenger when From is empty")
	}
}

func TestSendInstagramReply_NoMessenger(t *testing.T) {
	w := newTestIGWorker(nil)

	payload := queuePayload{
		ID: "job-ig-4",
		Message: MessageRequest{
			From:    "user_1",
			To:      "page_1",
			Channel: ChannelInstagram,
		},
	}
	resp := &Response{Message: "hello", Timestamp: time.Now()}

	// Should not panic
	blocked := w.sendInstagramReply(context.Background(), payload, resp)
	if blocked {
		t.Error("expected blocked=false")
	}
}
