package messaging

import (
	"context"
	"strings"
	"testing"

	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
)

type stubMessenger struct {
	last  conversation.OutboundReply
	calls int
}

func (s *stubMessenger) SendReply(ctx context.Context, reply conversation.OutboundReply) error {
	s.last = reply
	s.calls++
	return nil
}

type stubAssistantChecker struct {
	has   bool
	err   error
	calls int
}

func (s *stubAssistantChecker) HasAssistantMessage(ctx context.Context, conversationID string) (bool, error) {
	s.calls++
	return s.has, s.err
}

func TestDisclaimerWrapperAddsDisclaimer(t *testing.T) {
	inner := &stubMessenger{}
	wrapped := WrapWithDisclaimers(inner, DisclaimerWrapperConfig{
		Enabled: true,
		Level:   "short",
	})

	_ = wrapped.SendReply(context.Background(), conversation.OutboundReply{
		OrgID:          "org-1",
		ConversationID: "sms:org-1:15551234567",
		Body:           "Hello!",
	})

	if inner.calls != 1 {
		t.Fatalf("expected inner messenger to be called once")
	}
	if !strings.Contains(inner.last.Body, "Auto-assistant. Not medical advice.") {
		t.Fatalf("expected disclaimer to be appended, got %q", inner.last.Body)
	}
}

func TestDisclaimerWrapperSkipsWhenNotFirstMessage(t *testing.T) {
	inner := &stubMessenger{}
	checker := &stubAssistantChecker{has: true}
	wrapped := WrapWithDisclaimers(inner, DisclaimerWrapperConfig{
		Enabled:           true,
		Level:             "short",
		FirstMessageOnly:  true,
		ConversationStore: checker,
	})

	_ = wrapped.SendReply(context.Background(), conversation.OutboundReply{
		OrgID:          "org-1",
		ConversationID: "sms:org-1:15551234567",
		Body:           "Hello again!",
	})

	if inner.calls != 1 {
		t.Fatalf("expected inner messenger to be called once")
	}
	if strings.Contains(inner.last.Body, "Auto-assistant. Not medical advice.") {
		t.Fatalf("expected disclaimer to be skipped for non-first message")
	}
	if checker.calls != 1 {
		t.Fatalf("expected checker to be called once")
	}
}

func TestDisclaimerWrapperFallsBackWhenCheckerErrors(t *testing.T) {
	inner := &stubMessenger{}
	checker := &stubAssistantChecker{err: context.DeadlineExceeded}
	wrapped := WrapWithDisclaimers(inner, DisclaimerWrapperConfig{
		Enabled:           true,
		Level:             "short",
		FirstMessageOnly:  true,
		ConversationStore: checker,
	})

	_ = wrapped.SendReply(context.Background(), conversation.OutboundReply{
		OrgID:          "org-1",
		ConversationID: "sms:org-1:15551234567",
		Body:           "Hello!",
	})

	if inner.calls != 1 {
		t.Fatalf("expected inner messenger to be called once")
	}
	if !strings.Contains(inner.last.Body, "Auto-assistant. Not medical advice.") {
		t.Fatalf("expected disclaimer when checker fails, got %q", inner.last.Body)
	}
}
