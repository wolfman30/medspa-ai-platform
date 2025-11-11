package conversation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestJobStore_PutPendingPersistsDefaults(t *testing.T) {
	mock := &mockDynamo{}
	store := NewJobStore(mock, "conversation_jobs", logging.Default())

	job := &JobRecord{
		JobID:       "job-123",
		RequestType: jobTypeStart,
	}

	if err := store.PutPending(context.Background(), job); err != nil {
		t.Fatalf("PutPending returned error: %v", err)
	}

	if mock.putInput == nil {
		t.Fatalf("expected PutItem to be called")
	}

	var stored JobRecord
	if err := attributevalue.UnmarshalMap(mock.putInput.Item, &stored); err != nil {
		t.Fatalf("failed to unmarshal stored job: %v", err)
	}

	if stored.Status != JobStatusPending {
		t.Fatalf("expected status pending, got %s", stored.Status)
	}
	if stored.CreatedAt == "" || stored.UpdatedAt == "" {
		t.Fatal("expected timestamps to be populated")
	}
	if stored.ExpiresAt == 0 {
		t.Fatal("expected TTL to be set")
	}
	if stored.ExpiresAt <= time.Now().Unix() {
		t.Fatal("expected TTL to be in the future")
	}

	if expr := mock.putInput.ConditionExpression; expr == nil || *expr != "attribute_not_exists(jobId)" {
		t.Fatalf("expected condition expression to prevent overwrites, got %v", expr)
	}
}

func TestJobStore_PutPendingNilJob(t *testing.T) {
	store := NewJobStore(&mockDynamo{}, "conversation_jobs", logging.Default())
	if err := store.PutPending(context.Background(), nil); err == nil {
		t.Fatal("expected error when job is nil")
	}
}

func TestJobStore_MarkCompleted_UsesReservedAttributeNames(t *testing.T) {
	mock := &mockDynamo{}
	store := NewJobStore(mock, "conversation_jobs", logging.Default())

	resp := &Response{
		ConversationID: "conv-1",
		Message:        "thanks!",
	}

	if err := store.MarkCompleted(context.Background(), "job-123", resp, "conv-1"); err != nil {
		t.Fatalf("MarkCompleted returned error: %v", err)
	}

	if len(mock.updateInputs) != 1 {
		t.Fatalf("expected 1 update call, got %d", len(mock.updateInputs))
	}

	update := mock.updateInputs[0]

	names := update.ExpressionAttributeNames
	if names["#response"] != "response" || names["#error"] != "errorMessage" {
		t.Fatalf("expected reserved attribute names to be aliased, got %v", names)
	}

	values := update.ExpressionAttributeValues
	status := values[":status"].(*types.AttributeValueMemberS).Value
	if status != string(JobStatusCompleted) {
		t.Fatalf("expected completed status, got %s", status)
	}
	if _, ok := values[":response"].(*types.AttributeValueMemberM); !ok {
		t.Fatalf("expected marshalled response attribute, got %T", values[":response"])
	}
}

func TestJobStore_MarkFailed_SetsNullResponse(t *testing.T) {
	mock := &mockDynamo{}
	store := NewJobStore(mock, "conversation_jobs", logging.Default())

	if err := store.MarkFailed(context.Background(), "job-123", "boom"); err != nil {
		t.Fatalf("MarkFailed returned error: %v", err)
	}

	update := mock.updateInputs[0]
	if _, ok := update.ExpressionAttributeValues[":response"].(*types.AttributeValueMemberNULL); !ok {
		t.Fatalf("expected response to be set to NULL, got %T", update.ExpressionAttributeValues[":response"])
	}
}

func TestJobStore_MarkCompleted_PropagatesError(t *testing.T) {
	mock := &mockDynamo{updateErr: errors.New("dynamo failed")}
	store := NewJobStore(mock, "conversation_jobs", logging.Default())

	err := store.MarkCompleted(context.Background(), "job-1", &Response{}, "conv")
	if err == nil || !stringsContain(err.Error(), "dynamo failed") {
		t.Fatalf("expected dynamo error, got %v", err)
	}
}

func TestJobStore_GetJob_Success(t *testing.T) {
	mock := &mockDynamo{
		getOutput: &dynamodb.GetItemOutput{
			Item: map[string]types.AttributeValue{
				"jobId":  &types.AttributeValueMemberS{Value: "job-42"},
				"status": &types.AttributeValueMemberS{Value: string(JobStatusPending)},
			},
		},
	}
	store := NewJobStore(mock, "conversation_jobs", logging.Default())

	job, err := store.GetJob(context.Background(), "job-42")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if job.JobID != "job-42" || job.Status != JobStatusPending {
		t.Fatalf("unexpected job result: %#v", job)
	}
}

func TestJobStore_GetJob_NotFound(t *testing.T) {
	mock := &mockDynamo{getOutput: &dynamodb.GetItemOutput{}}
	store := NewJobStore(mock, "conversation_jobs", logging.Default())

	_, err := store.GetJob(context.Background(), "job-42")
	if !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("expected ErrJobNotFound, got %v", err)
	}
}

func TestJobStore_GetJob_EmptyID(t *testing.T) {
	store := NewJobStore(&mockDynamo{}, "conversation_jobs", logging.Default())
	if _, err := store.GetJob(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty jobID")
	}
}

type mockDynamo struct {
	putInput     *dynamodb.PutItemInput
	putErr       error
	updateInputs []*dynamodb.UpdateItemInput
	updateErr    error
	getOutput    *dynamodb.GetItemOutput
	getErr       error
}

func (m *mockDynamo) PutItem(ctx context.Context, input *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	m.putInput = input
	return &dynamodb.PutItemOutput{}, m.putErr
}

func (m *mockDynamo) UpdateItem(ctx context.Context, input *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	m.updateInputs = append(m.updateInputs, input)
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	return &dynamodb.UpdateItemOutput{}, nil
}

func (m *mockDynamo) GetItem(ctx context.Context, input *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.getOutput == nil {
		return &dynamodb.GetItemOutput{}, nil
	}
	return m.getOutput, nil
}

func stringsContain(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
