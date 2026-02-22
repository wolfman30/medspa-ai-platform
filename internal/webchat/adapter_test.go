package webchat

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestReplyMessenger_SendReply(t *testing.T) {
	ts := newMockTranscript()
	pub := &mockPublisher{}
	h := NewHandler(pub, ts, nil, logging.New("error"))
	m := NewReplyMessenger(h, logging.New("error"))

	err := m.SendReply(context.Background(), conversation.OutboundReply{
		OrgID:          "org1",
		ConversationID: "webchat:org1:sess1",
		To:             "sess1",
		From:           "org1",
		Body:           "Hello from AI!",
	})

	require.NoError(t, err)

	// Verify transcript was stored
	msgs := ts.store["webchat:org1:sess1"]
	require.Len(t, msgs, 1)
	assert.Equal(t, "assistant", msgs[0].Role)
	assert.Equal(t, "Hello from AI!", msgs[0].Body)
	assert.Equal(t, "webchat_reply", msgs[0].Kind)
}

func TestReplyMessenger_NoTranscript(t *testing.T) {
	pub := &mockPublisher{}
	h := NewHandler(pub, nil, nil, logging.New("error"))
	m := NewReplyMessenger(h, logging.New("error"))

	err := m.SendReply(context.Background(), conversation.OutboundReply{
		OrgID:          "org1",
		ConversationID: "webchat:org1:sess1",
		Body:           "Hello!",
	})

	assert.NoError(t, err)
}
