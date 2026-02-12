package archive

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockBedrockClient implements BedrockConverseAPI for testing.
type mockBedrockClient struct {
	response string
	err      error
}

func (m *mockBedrockClient) Converse(_ context.Context, _ *bedrockruntime.ConverseInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &bedrockruntime.ConverseOutput{
		Output: &brtypes.ConverseOutputMemberMessage{
			Value: brtypes.Message{
				Content: []brtypes.ContentBlock{
					&brtypes.ContentBlockMemberText{Value: m.response},
				},
			},
		},
	}, nil
}

func TestClassifier_TestNumber(t *testing.T) {
	c := NewClassifier(nil, "test-model", nil)

	labels, err := c.Classify(context.Background(), "+15005550002", []Message{
		{Role: "user", Content: "test message", Timestamp: time.Now()},
	})

	require.NoError(t, err)
	assert.Equal(t, "test_internal", labels.ConversationCategory)
	assert.Equal(t, "test_detection", labels.LabelModel)
	assert.True(t, labels.AutoLabeled)
}

func TestClassifier_LLMClassification(t *testing.T) {
	mock := &mockBedrockClient{
		response: `{"medical_liability_risk":"low","prompt_injection_detected":false,"prompt_injection_type":"none","conversation_category":"normal_booking","sentiment":"positive","contains_phi":false}`,
	}

	c := NewClassifier(mock, "haiku-model", nil)

	labels, err := c.Classify(context.Background(), "+15551234567", []Message{
		{Role: "user", Content: "I want to book Botox", Timestamp: time.Now()},
		{Role: "assistant", Content: "Sure! What day works?", Timestamp: time.Now()},
	})

	require.NoError(t, err)
	assert.Equal(t, "normal_booking", labels.ConversationCategory)
	assert.Equal(t, "low", labels.MedicalLiabilityRisk)
	assert.Equal(t, "positive", labels.Sentiment)
	assert.True(t, labels.AutoLabeled)
	assert.Equal(t, "claude-haiku", labels.LabelModel)
}

func TestClassifier_NilClient(t *testing.T) {
	c := NewClassifier(nil, "", nil)
	labels, err := c.Classify(context.Background(), "+15551234567", []Message{
		{Role: "user", Content: "hello", Timestamp: time.Now()},
	})
	require.NoError(t, err)
	assert.Equal(t, "normal_booking", labels.ConversationCategory)
	assert.False(t, labels.AutoLabeled)
}

func TestParseLabelsJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		category string
	}{
		{"plain json", `{"conversation_category":"abandoned","medical_liability_risk":"none","prompt_injection_detected":false,"prompt_injection_type":"none","sentiment":"neutral","contains_phi":false}`, "abandoned"},
		{"wrapped in markdown", "```json\n{\"conversation_category\":\"escalation\"}\n```", "escalation"},
		{"garbage", "no json here", "normal_booking"}, // falls back to default
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels, _ := parseLabelsJSON(tt.input)
			assert.Equal(t, tt.category, labels.ConversationCategory)
		})
	}
}
