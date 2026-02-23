package messaging

import (
	"context"
	"errors"
	"testing"

	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
)

// mockMessenger implements conversation.ReplyMessenger for testing.
type mockMessenger struct {
	err      error
	calls    int
	lastBody string
}

func (m *mockMessenger) SendReply(_ context.Context, reply conversation.OutboundReply) error {
	m.calls++
	m.lastBody = reply.Body
	return m.err
}

func TestStaticOrgResolver(t *testing.T) {
	resolver := NewStaticOrgResolver(map[string]string{
		"+11234567890": "org-1",
		"+10987654321": "org-2",
	})

	ctx := context.Background()

	t.Run("found", func(t *testing.T) {
		orgID, err := resolver.ResolveOrgID(ctx, "+11234567890")
		if err != nil || orgID != "org-1" {
			t.Errorf("ResolveOrgID = (%q, %v), want (org-1, nil)", orgID, err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := resolver.ResolveOrgID(ctx, "+15555555555")
		if !errors.Is(err, ErrOrgNotFound) {
			t.Errorf("expected ErrOrgNotFound, got %v", err)
		}
	})

	t.Run("empty number", func(t *testing.T) {
		_, err := resolver.ResolveOrgID(ctx, "")
		if !errors.Is(err, ErrOrgNotFound) {
			t.Errorf("expected ErrOrgNotFound, got %v", err)
		}
	})

	t.Run("nil resolver", func(t *testing.T) {
		var r *StaticOrgResolver
		_, err := r.ResolveOrgID(ctx, "+11234567890")
		if !errors.Is(err, ErrOrgNotFound) {
			t.Errorf("expected ErrOrgNotFound, got %v", err)
		}
	})

	t.Run("default from number", func(t *testing.T) {
		num := resolver.DefaultFromNumber("org-1")
		if num == "" {
			t.Error("expected non-empty default from number")
		}
	})

	t.Run("default from number nil", func(t *testing.T) {
		var r *StaticOrgResolver
		if got := r.DefaultFromNumber("org-1"); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})
}

func TestFailoverMessenger(t *testing.T) {
	ctx := context.Background()
	reply := conversation.OutboundReply{To: "+11234567890", Body: "test"}

	t.Run("primary succeeds", func(t *testing.T) {
		primary := &mockMessenger{}
		secondary := &mockMessenger{}
		f := NewFailoverMessenger(primary, "primary", secondary, "secondary", nil)
		err := f.SendReply(ctx, reply)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if primary.calls != 1 {
			t.Error("primary should be called once")
		}
		if secondary.calls != 0 {
			t.Error("secondary should not be called")
		}
	})

	t.Run("primary fails secondary succeeds", func(t *testing.T) {
		primary := &mockMessenger{err: errors.New("fail")}
		secondary := &mockMessenger{}
		f := NewFailoverMessenger(primary, "primary", secondary, "secondary", nil)
		err := f.SendReply(ctx, reply)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if secondary.calls != 1 {
			t.Error("secondary should be called once")
		}
	})

	t.Run("both fail", func(t *testing.T) {
		primary := &mockMessenger{err: errors.New("fail1")}
		secondary := &mockMessenger{err: errors.New("fail2")}
		f := NewFailoverMessenger(primary, "primary", secondary, "secondary", nil)
		err := f.SendReply(ctx, reply)
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("nil failover", func(t *testing.T) {
		var f *FailoverMessenger
		err := f.SendReply(ctx, reply)
		if err == nil {
			t.Error("expected error for nil failover")
		}
	})

	t.Run("primary fails no secondary", func(t *testing.T) {
		primary := &mockMessenger{err: errors.New("fail")}
		f := NewFailoverMessenger(primary, "primary", nil, "", nil)
		err := f.SendReply(ctx, reply)
		if err == nil {
			t.Error("expected error")
		}
	})
}

func TestDemoModeMessenger(t *testing.T) {
	ctx := context.Background()

	t.Run("disabled returns original", func(t *testing.T) {
		inner := &mockMessenger{}
		result := WrapWithDemoMode(inner, DemoModeConfig{Enabled: false})
		if result != inner {
			t.Error("expected original messenger when disabled")
		}
	})

	t.Run("nil messenger", func(t *testing.T) {
		result := WrapWithDemoMode(nil, DemoModeConfig{Enabled: true})
		if result != nil {
			t.Error("expected nil when messenger is nil")
		}
	})

	t.Run("wraps message", func(t *testing.T) {
		inner := &mockMessenger{}
		wrapped := WrapWithDemoMode(inner, DemoModeConfig{
			Enabled: true,
			Prefix:  "AI Wolf: ",
			Suffix:  " Reply STOP to opt out.",
		})
		reply := conversation.OutboundReply{Body: "Hello"}
		err := wrapped.SendReply(ctx, reply)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if inner.lastBody != "AI Wolf: Hello Reply STOP to opt out." {
			t.Errorf("unexpected body: %q", inner.lastBody)
		}
	})
}
