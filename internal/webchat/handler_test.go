package webchat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// mockPublisher records enqueued messages.
type mockPublisher struct {
	messages []conversation.MessageRequest
	starts   []conversation.StartRequest
}

func (m *mockPublisher) EnqueueMessage(_ context.Context, _ string, req conversation.MessageRequest, _ ...conversation.PublishOption) error {
	m.messages = append(m.messages, req)
	return nil
}

func (m *mockPublisher) EnqueueStart(_ context.Context, _ string, req conversation.StartRequest, _ ...conversation.PublishOption) error {
	m.starts = append(m.starts, req)
	return nil
}

// mockTranscript stores messages in memory.
type mockTranscript struct {
	store map[string][]conversation.SMSTranscriptMessage
}

func newMockTranscript() *mockTranscript {
	return &mockTranscript{store: make(map[string][]conversation.SMSTranscriptMessage)}
}

func (m *mockTranscript) Append(_ context.Context, convID string, msg conversation.SMSTranscriptMessage) error {
	m.store[convID] = append(m.store[convID], msg)
	return nil
}

func (m *mockTranscript) List(_ context.Context, convID string, limit int64) ([]conversation.SMSTranscriptMessage, error) {
	msgs := m.store[convID]
	if int64(len(msgs)) > limit {
		msgs = msgs[:limit]
	}
	return msgs, nil
}

func TestConversationID(t *testing.T) {
	assert.Equal(t, "webchat:org123:sess456", ConversationID("org123", "sess456"))
}

func TestGenerateSessionID(t *testing.T) {
	s1 := generateSessionID()
	s2 := generateSessionID()
	assert.NotEmpty(t, s1)
	assert.NotEqual(t, s1, s2)
	assert.Len(t, s1, 32) // 16 bytes = 32 hex chars
}

func TestHandleMessage_HTTP(t *testing.T) {
	pub := &mockPublisher{}
	ts := newMockTranscript()
	h := NewHandler(pub, ts, []byte("// widget"), logging.New("error"))

	body := `{"org_id":"org1","session_id":"sess1","text":"Hello"}`
	req := httptest.NewRequest(http.MethodPost, "/chat/message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleMessage(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "queued", resp["status"])
	assert.Equal(t, "sess1", resp["session_id"])

	// Verify message was enqueued
	require.Len(t, pub.messages, 1)
	assert.Equal(t, "org1", pub.messages[0].OrgID)
	assert.Equal(t, "Hello", pub.messages[0].Message)
	assert.Equal(t, conversation.ChannelWebChat, pub.messages[0].Channel)
	assert.Equal(t, "webchat:org1:sess1", pub.messages[0].ConversationID)

	// Verify transcript stored
	msgs := ts.store["webchat:org1:sess1"]
	require.Len(t, msgs, 1)
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "Hello", msgs[0].Body)
}

func TestHandleMessage_MissingFields(t *testing.T) {
	pub := &mockPublisher{}
	h := NewHandler(pub, nil, nil, logging.New("error"))

	body := `{"org_id":"","text":"Hello"}`
	req := httptest.NewRequest(http.MethodPost, "/chat/message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleMessage(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleMessage_GeneratesSessionID(t *testing.T) {
	pub := &mockPublisher{}
	h := NewHandler(pub, nil, nil, logging.New("error"))

	body := `{"org_id":"org1","text":"Hi"}`
	req := httptest.NewRequest(http.MethodPost, "/chat/message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.HandleMessage(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp["session_id"])
}

func TestHandleHistory(t *testing.T) {
	ts := newMockTranscript()
	ts.store["webchat:org1:sess1"] = []conversation.SMSTranscriptMessage{
		{Role: "user", Body: "Hello"},
		{Role: "assistant", Body: "Hi there!"},
	}
	h := NewHandler(nil, ts, nil, logging.New("error"))

	req := httptest.NewRequest(http.MethodGet, "/chat/history?org=org1&session=sess1", nil)
	w := httptest.NewRecorder()

	h.HandleHistory(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Messages []HistoryMessage `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Messages, 2)
	assert.Equal(t, "user", resp.Messages[0].Role)
	assert.Equal(t, "Hello", resp.Messages[0].Text)
	assert.Equal(t, "assistant", resp.Messages[1].Role)
}

func TestHandleHistory_MissingParams(t *testing.T) {
	h := NewHandler(nil, nil, nil, logging.New("error"))

	req := httptest.NewRequest(http.MethodGet, "/chat/history?org=org1", nil)
	w := httptest.NewRecorder()

	h.HandleHistory(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleHistory_NoTranscriptStore(t *testing.T) {
	h := NewHandler(nil, nil, nil, logging.New("error"))

	req := httptest.NewRequest(http.MethodGet, "/chat/history?org=org1&session=sess1", nil)
	w := httptest.NewRecorder()

	h.HandleHistory(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Messages []HistoryMessage `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.Messages)
}

func TestHandleWidgetJS(t *testing.T) {
	widgetContent := []byte("(function(){ /* widget */ })();")
	h := NewHandler(nil, nil, widgetContent, logging.New("error"))

	req := httptest.NewRequest(http.MethodGet, "/chat/widget.js", nil)
	w := httptest.NewRecorder()

	h.HandleWidgetJS(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/javascript", w.Header().Get("Content-Type"))
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, string(widgetContent), w.Body.String())
}
