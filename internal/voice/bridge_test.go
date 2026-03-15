package voice

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
)

type testMessenger struct {
	mu      sync.Mutex
	replies []conversation.OutboundReply
	ch      chan struct{}
}

func newTestMessenger() *testMessenger {
	return &testMessenger{ch: make(chan struct{}, 10)}
}

func (m *testMessenger) SendReply(_ context.Context, reply conversation.OutboundReply) error {
	m.mu.Lock()
	m.replies = append(m.replies, reply)
	m.mu.Unlock()
	select {
	case m.ch <- struct{}{}:
	default:
	}
	return nil
}

func (m *testMessenger) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.replies)
}

func (m *testMessenger) firstReply() conversation.OutboundReply {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.replies) == 0 {
		return conversation.OutboundReply{}
	}
	return m.replies[0]
}

func setupClinicStore(t *testing.T, cfg *clinic.Config) *clinic.Store {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
		mr.Close()
	})

	store := clinic.NewStore(client)
	if cfg != nil {
		if err := store.Set(context.Background(), cfg); err != nil {
			t.Fatalf("store.Set failed: %v", err)
		}
	}
	return store
}

func newBridgeForDepositTests(t *testing.T) (*Bridge, *testMessenger) {
	t.Helper()
	orgID := "org-bridge-test"
	cfg := clinic.DefaultConfig(orgID)
	cfg.Name = "Glow Clinic"
	cfg.Phone = "+15550000001"
	cfg.SMSPhoneNumber = "+15550000002"
	cfg.DepositAmountCents = 7500

	store := setupClinicStore(t, cfg)
	messenger := newTestMessenger()

	h := NewToolHandler(orgID, "+15551112222", "+15550000003", &ToolDeps{
		Messenger:   messenger,
		ClinicStore: store,
	}, slog.Default())

	b := &Bridge{
		logger:      slog.Default(),
		toolHandler: h,
		orgID:       orgID,
		callerPhone: "+15551112222",
	}
	return b, messenger
}

func waitForSend(t *testing.T, m *testMessenger) {
	t.Helper()
	select {
	case <-m.ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SMS send")
	}
}

func TestBridgeMaybeFireDepositSMS_FiresOnDepositAndTextYou(t *testing.T) {
	b, messenger := newBridgeForDepositTests(t)

	b.maybeCaptureSlotSelection("Perfect, Thursday March 19 at 5 PM works great.")
	b.maybeFireDepositSMS(context.Background(), "Great, there's a deposit and I'll text you the secure payment details now.")
	waitForSend(t, messenger)

	if got := messenger.count(); got != 1 {
		t.Fatalf("expected 1 SMS send, got %d", got)
	}
}

func TestBridgeMaybeFireDepositSMS_FiresOnDepositAndLink(t *testing.T) {
	b, messenger := newBridgeForDepositTests(t)

	b.maybeCaptureSlotSelection("Awesome, Wednesday March 18 at 4:30 PM works perfectly.")
	b.maybeFireDepositSMS(context.Background(), "Perfect — your deposit is next, use this secure link to complete it.")
	waitForSend(t, messenger)

	if got := messenger.count(); got != 1 {
		t.Fatalf("expected 1 SMS send, got %d", got)
	}
}

func TestBridgeMaybeFireDepositSMS_DoesNotFireForDepositOnly(t *testing.T) {
	b, messenger := newBridgeForDepositTests(t)

	b.maybeCaptureSlotSelection("Great, Tuesday March 17 at 3 PM works for you.")
	b.maybeFireDepositSMS(context.Background(), "A deposit is required to secure your appointment.")

	select {
	case <-messenger.ch:
		t.Fatal("unexpected SMS send")
	case <-time.After(150 * time.Millisecond):
		// expected
	}

	if got := messenger.count(); got != 0 {
		t.Fatalf("expected 0 SMS sends, got %d", got)
	}
}

func TestBridgeMaybeFireDepositSMS_OnlyFiresOncePerCall(t *testing.T) {
	b, messenger := newBridgeForDepositTests(t)

	b.maybeCaptureSlotSelection("Great, Tuesday March 17 at 3 PM works for you.")
	b.maybeFireDepositSMS(context.Background(), "There's a deposit and I'll text you now.")
	waitForSend(t, messenger)
	b.maybeFireDepositSMS(context.Background(), "Resending the deposit link now.")

	select {
	case <-messenger.ch:
		t.Fatal("unexpected second SMS send")
	case <-time.After(150 * time.Millisecond):
		// expected
	}

	if !b.depositSMSSent {
		t.Fatal("expected depositSMSSent to be true")
	}
	if got := messenger.count(); got != 1 {
		t.Fatalf("expected exactly 1 SMS send, got %d", got)
	}
}

func TestBridgeShouldProcessAssistantText_DeduplicatesWithinWindow(t *testing.T) {
	b := &Bridge{recentAssistantText: make(map[string]time.Time)}

	if !b.shouldProcessAssistantText("I'll text you the link now") {
		t.Fatal("expected first assistant text to be processed")
	}
	if b.shouldProcessAssistantText("  i'll text you the link now  ") {
		t.Fatal("expected normalized duplicate assistant text to be suppressed")
	}
}

func TestBridgeMaybeFireDepositSMS_DuplicateTranscriptVariantsStillSendOnce(t *testing.T) {
	b, messenger := newBridgeForDepositTests(t)

	b.maybeCaptureSlotSelection("Perfect, Friday March 20 at 11 AM works great.")
	b.maybeFireDepositSMS(context.Background(), "I'll text you the deposit link now.")
	waitForSend(t, messenger)
	b.maybeFireDepositSMS(context.Background(), "  i'll TEXT you the deposit LINK now.  ")

	select {
	case <-messenger.ch:
		t.Fatal("unexpected second SMS send for duplicate variant")
	case <-time.After(150 * time.Millisecond):
		// expected
	}

	if got := messenger.count(); got != 1 {
		t.Fatalf("expected exactly 1 SMS send, got %d", got)
	}
}

func TestBridgeMaybeFireDepositSMS_DoesNotFireBeforeExplicitSlotSelection(t *testing.T) {
	b, messenger := newBridgeForDepositTests(t)

	b.maybeFireDepositSMS(context.Background(), "I'll text you the deposit link now.")

	select {
	case <-messenger.ch:
		t.Fatal("unexpected SMS send before explicit slot selection")
	case <-time.After(150 * time.Millisecond):
		// expected
	}
	if got := messenger.count(); got != 0 {
		t.Fatalf("expected 0 SMS sends, got %d", got)
	}
}

func TestLooksLikeExplicitSlotSelection(t *testing.T) {
	if !looksLikeExplicitSlotSelection("Perfect, Thursday March 19 at 5 PM works great.") {
		t.Fatal("expected explicit slot selection to be detected")
	}
	if !looksLikeExplicitSlotSelection("Wednesday March 25th at 4:30 PM — awesome! There's a fifty dollar deposit to hold your spot.") {
		t.Fatal("expected explicit slot selection to be detected for 'awesome' confirmations")
	}
	if looksLikeExplicitSlotSelection("I can check Thursday afternoon if that works") {
		t.Fatal("did not expect vague time reference to count as explicit slot selection")
	}
	// Nova Sonic renders times as words — must detect spoken slot confirmations
	if !looksLikeExplicitSlotSelection("Great, you're all set for Thursday, March twentieth at four thirty PM.") {
		t.Fatal("expected spoken word slot confirmation to be detected")
	}
	if !looksLikeExplicitSlotSelection("All set for Tuesday, March eighteenth at two PM.") {
		t.Fatal("expected 'all set' with spoken time to be detected")
	}
}

func TestPaymentConfirmationChannel(t *testing.T) {
	caller := "+15551234567"
	want := "voice:payment:+15551234567"
	if got := PaymentConfirmationChannel(caller); got != want {
		t.Fatalf("PaymentConfirmationChannel() = %q, want %q", got, want)
	}
}
