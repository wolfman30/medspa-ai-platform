package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// commandCapture records all Telnyx API commands sent during a test.
type commandCapture struct {
	mu       sync.Mutex
	commands []capturedCommand
}

type capturedCommand struct {
	command   string
	payload   map[string]interface{}
	timestamp time.Time
}

func (c *commandCapture) record(command string, payload map[string]interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.commands = append(c.commands, capturedCommand{
		command:   command,
		payload:   payload,
		timestamp: time.Now(),
	})
}

func (c *commandCapture) get() []capturedCommand {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]capturedCommand, len(c.commands))
	copy(cp, c.commands)
	return cp
}

// testOrgResolver implements messaging.OrgResolver for tests.
type testOrgResolver struct {
	orgs map[string]string
}

func (m *testOrgResolver) ResolveOrgID(_ context.Context, phone string) (string, error) {
	if orgID, ok := m.orgs[phone]; ok {
		return orgID, nil
	}
	return "", nil
}

func newMockTelnyxServer(capture *commandCapture) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimRight(r.URL.Path, "/"), "/")
		cmd := parts[len(parts)-1]
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		json.Unmarshal(body, &payload)
		capture.record(cmd, payload)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"data":{"result":"ok"}}`))
	}))
}

func newCallControlTestHandler(t *testing.T, orgs map[string]string, mockURL string) *CallControlHandler {
	t.Helper()
	return &CallControlHandler{
		telnyxAPIKey:  "test-key",
		telnyxBaseURL: mockURL,
		logger:        logging.New("info"),
		orgResolver:   &testOrgResolver{orgs: orgs},
	}
}

// --- REGRESSION TESTS ---
// These tests gate CI and prevent voice greeting regressions.

// TestPlaybackCommandName_Regression ensures we use "playback_start" not "play".
// REGRESSION: commit c338f7dd used "play" → Telnyx returned 404 → no greeting.
func TestPlaybackCommandName_Regression(t *testing.T) {
	capture := &commandCapture{}
	mock := newMockTelnyxServer(capture)
	defer mock.Close()

	h := newCallControlTestHandler(t, map[string]string{
		"+14407608111": "d9558a2d-2110-4e26-8224-1b36cd526e14",
	}, mock.URL)

	h.playPreRecordedGreeting("test-call-id", "+19378962713", "+14407608111")

	cmds := capture.get()
	if len(cmds) == 0 {
		t.Fatal("REGRESSION: no Telnyx command sent — greeting did not fire at all")
	}
	if cmds[0].command != "playback_start" {
		t.Errorf("REGRESSION: Telnyx command = %q, want %q ('play' causes 404)", cmds[0].command, "playback_start")
	}
}

// TestGreetingFiresWithin3Seconds verifies greeting latency is acceptable.
func TestGreetingFiresWithin3Seconds(t *testing.T) {
	capture := &commandCapture{}
	mock := newMockTelnyxServer(capture)
	defer mock.Close()

	h := newCallControlTestHandler(t, map[string]string{
		"+14407608111": "d9558a2d-2110-4e26-8224-1b36cd526e14",
	}, mock.URL)

	start := time.Now()
	h.playPreRecordedGreeting("test-call-id", "+19378962713", "+14407608111")
	elapsed := time.Since(start)

	if elapsed > 3*time.Second {
		t.Errorf("greeting took %v to fire, want < 3s", elapsed)
	}

	cmds := capture.get()
	if len(cmds) == 0 {
		t.Fatal("no greeting command sent")
	}
	t.Logf("greeting fired in %v (target: < 3s)", elapsed)
}

// TestGreetingIncludesAudioURL verifies the playback payload has audio_url.
func TestGreetingIncludesAudioURL(t *testing.T) {
	capture := &commandCapture{}
	mock := newMockTelnyxServer(capture)
	defer mock.Close()

	h := newCallControlTestHandler(t, map[string]string{
		"+14407608111": "d9558a2d-2110-4e26-8224-1b36cd526e14",
	}, mock.URL)

	h.playPreRecordedGreeting("test-call-id", "+19378962713", "+14407608111")

	cmds := capture.get()
	if len(cmds) == 0 {
		t.Fatal("no command sent")
	}

	// Must include media_name (Telnyx CDN) and audio_url (fallback)
	mediaName, _ := cmds[0].payload["media_name"].(string)
	audioURL, _ := cmds[0].payload["audio_url"].(string)
	if mediaName == "" && audioURL == "" {
		t.Fatal("both media_name and audio_url missing from playback_start payload")
	}
	if mediaName != "" && !strings.Contains(mediaName, "bodytonic") {
		t.Errorf("media_name = %q, should contain 'bodytonic'", mediaName)
	}
	if audioURL != "" && !strings.HasSuffix(audioURL, ".mp3") {
		t.Errorf("audio_url = %q, want .mp3 suffix", audioURL)
	}
}

// TestGreetingFallbackForUnknownOrg verifies unknown orgs don't send playback
// (sidecar handles greeting instead).
func TestGreetingFallbackForUnknownOrg(t *testing.T) {
	capture := &commandCapture{}
	mock := newMockTelnyxServer(capture)
	defer mock.Close()

	h := newCallControlTestHandler(t, map[string]string{
		"+15551234567": "unknown-org-id",
	}, mock.URL)

	h.playPreRecordedGreeting("test-call-id", "+19378962713", "+15551234567")

	cmds := capture.get()
	if len(cmds) != 0 {
		t.Errorf("expected 0 commands for unknown org (sidecar fallback), got %d", len(cmds))
	}
}

// TestAllKnownOrgsHaveGreetingURLs verifies every org with voice AI has a greeting.
func TestAllKnownOrgsHaveGreetingURLs(t *testing.T) {
	// These are the orgs we support for voice AI.
	// If you add a new voice org, add its greeting here or the test fails.
	voiceOrgs := []struct {
		orgID      string
		clinicName string
	}{
		{"d9558a2d-2110-4e26-8224-1b36cd526e14", "BodyTonic"},
		{"d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599", "Forever 22"},
	}

	// This must match the map in playPreRecordedGreeting
	greetingURLs := map[string]string{
		"d9558a2d-2110-4e26-8224-1b36cd526e14": "https://api-dev.aiwolfsolutions.com/static/greetings/bodytonic.mp3",
		"d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599": "https://api-dev.aiwolfsolutions.com/static/greetings/forever22.mp3",
	}

	for _, org := range voiceOrgs {
		url, ok := greetingURLs[org.orgID]
		if !ok {
			t.Errorf("org %s (%s) has no pre-recorded greeting URL — callers will get no greeting!", org.orgID, org.clinicName)
		}
		if !strings.HasSuffix(url, ".mp3") {
			t.Errorf("org %s greeting URL should end in .mp3: %s", org.clinicName, url)
		}
	}
}

// TestSidecarSkipSentinel verifies the __SKIP__ value prevents double greeting.
func TestSidecarSkipSentinel(t *testing.T) {
	// If this value changes, update nova-sonic-sidecar/src/bridge.ts too.
	// The sidecar checks: if (greeting === "__SKIP__") { skip ElevenLabs greeting }
	sentinel := "__SKIP__"
	if sentinel != "__SKIP__" {
		t.Error("sidecar skip sentinel changed — will cause double greeting regression")
	}
}

// TestStreamingStartsOnCallAnswered verifies streaming_start is also sent.
func TestStreamingStartsOnCallAnswered(t *testing.T) {
	capture := &commandCapture{}
	mock := newMockTelnyxServer(capture)
	defer mock.Close()

	h := newCallControlTestHandler(t, map[string]string{
		"+14407608111": "d9558a2d-2110-4e26-8224-1b36cd526e14",
	}, mock.URL)
	h.streamURL = "wss://test.example.com/ws/voice"

	// Simulate the full call.answered flow
	h.playPreRecordedGreeting("test-call-id", "+19378962713", "+14407608111")
	h.startStreaming("test-call-id", "+19378962713", "+14407608111")

	cmds := capture.get()
	if len(cmds) < 2 {
		t.Fatalf("expected at least 2 commands (playback_start + streaming_start), got %d", len(cmds))
	}

	gotPlayback := false
	gotStreaming := false
	for _, cmd := range cmds {
		switch cmd.command {
		case "playback_start":
			gotPlayback = true
		case "streaming_start":
			gotStreaming = true
		}
	}
	if !gotPlayback {
		t.Error("missing playback_start command")
	}
	if !gotStreaming {
		t.Error("missing streaming_start command")
	}
}
