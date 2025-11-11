package conversation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

const (
	jobTTL = 24 * time.Hour
)

// JobStatus represents the lifecycle of a conversation job.
type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
)

// ErrJobNotFound indicates the requested job ID does not exist.
var ErrJobNotFound = errors.New("conversation: job not found")

type dynamoAPI interface {
	PutItem(context.Context, *dynamodb.PutItemInput, ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	UpdateItem(context.Context, *dynamodb.UpdateItemInput, ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
	GetItem(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
}

// JobRecord captures the persisted state of a conversation request.
type JobRecord struct {
	JobID          string          `dynamodbav:"jobId" json:"jobId"`
	Status         JobStatus       `dynamodbav:"status" json:"status"`
	RequestType    jobType         `dynamodbav:"requestType" json:"requestType"`
	ConversationID string          `dynamodbav:"conversationId,omitempty" json:"conversationId,omitempty"`
	StartRequest   *StartRequest   `dynamodbav:"startRequest,omitempty" json:"startRequest,omitempty"`
	MessageRequest *MessageRequest `dynamodbav:"messageRequest,omitempty" json:"messageRequest,omitempty"`
	Response       *Response       `dynamodbav:"response,omitempty" json:"response,omitempty"`
	ErrorMessage   string          `dynamodbav:"errorMessage,omitempty" json:"errorMessage,omitempty"`
	CreatedAt      string          `dynamodbav:"createdAt" json:"createdAt"`
	UpdatedAt      string          `dynamodbav:"updatedAt" json:"updatedAt"`
	ExpiresAt      int64           `dynamodbav:"expiresAt,omitempty" json:"-"`
}

// JobStore persists job records to DynamoDB.
type JobRecorder interface {
	PutPending(ctx context.Context, job *JobRecord) error
	GetJob(ctx context.Context, jobID string) (*JobRecord, error)
}

type JobUpdater interface {
	MarkCompleted(ctx context.Context, jobID string, resp *Response, conversationID string) error
	MarkFailed(ctx context.Context, jobID string, errMsg string) error
}

type JobStore struct {
	client    dynamoAPI
	tableName string
	logger    *logging.Logger
}

var _ JobRecorder = (*JobStore)(nil)
var _ JobUpdater = (*JobStore)(nil)

// NewJobStore builds a store backed by the provided DynamoDB client.
func NewJobStore(client dynamoAPI, tableName string, logger *logging.Logger) *JobStore {
	if client == nil {
		panic("conversation: dynamodb client cannot be nil")
	}
	if tableName == "" {
		panic("conversation: table name cannot be empty")
	}
	if logger == nil {
		logger = logging.Default()
	}

	return &JobStore{
		client:    client,
		tableName: tableName,
		logger:    logger,
	}
}

// PutPending inserts a new pending job record.
func (s *JobStore) PutPending(ctx context.Context, job *JobRecord) error {
	if job == nil {
		return errors.New("conversation: job cannot be nil")
	}
	now := time.Now().UTC()
	job.Status = JobStatusPending
	job.CreatedAt = now.Format(time.RFC3339Nano)
	job.UpdatedAt = job.CreatedAt
	if job.ExpiresAt == 0 {
		job.ExpiresAt = now.Add(jobTTL).Unix()
	}

	item, err := attributevalue.MarshalMap(job)
	if err != nil {
		return fmt.Errorf("conversation: failed to marshal job: %w", err)
	}

	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(s.tableName),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(jobId)"),
	})
	if err != nil {
		return fmt.Errorf("conversation: failed to persist job: %w", err)
	}
	return nil
}

// MarkCompleted updates a job with the final response.
func (s *JobStore) MarkCompleted(ctx context.Context, jobID string, resp *Response, conversationID string) error {
	if jobID == "" {
		return errors.New("conversation: jobID required")
	}
	if resp == nil {
		resp = &Response{}
	}
	respAttr, err := attributevalue.Marshal(resp)
	if err != nil {
		return fmt.Errorf("conversation: failed to marshal response: %w", err)
	}

	return s.updateJob(
		ctx,
		jobID,
		map[string]types.AttributeValue{
			":status":       &types.AttributeValueMemberS{Value: string(JobStatusCompleted)},
			":response":     respAttr,
			":conversation": &types.AttributeValueMemberS{Value: conversationID},
			":error":        &types.AttributeValueMemberS{Value: ""},
			":updated":      &types.AttributeValueMemberS{Value: time.Now().UTC().Format(time.RFC3339Nano)},
		},
		map[string]string{
			"#status":   "status",
			"#response": "response",
			"#error":    "errorMessage",
			"#updated":  "updatedAt",
		},
		"SET #status = :status, #response = :response, conversationId = :conversation, #error = :error, #updated = :updated",
	)
}

// MarkFailed updates a job to the failed state.
func (s *JobStore) MarkFailed(ctx context.Context, jobID string, errMsg string) error {
	if jobID == "" {
		return errors.New("conversation: jobID required")
	}
	return s.updateJob(
		ctx,
		jobID,
		map[string]types.AttributeValue{
			":status":   &types.AttributeValueMemberS{Value: string(JobStatusFailed)},
			":response": &types.AttributeValueMemberNULL{Value: true},
			":error":    &types.AttributeValueMemberS{Value: errMsg},
			":updated":  &types.AttributeValueMemberS{Value: time.Now().UTC().Format(time.RFC3339Nano)},
		},
		map[string]string{
			"#status":   "status",
			"#response": "response",
			"#error":    "errorMessage",
			"#updated":  "updatedAt",
		},
		"SET #status = :status, #response = :response, #error = :error, #updated = :updated",
	)
}

// GetJob fetches a job by ID.
func (s *JobStore) GetJob(ctx context.Context, jobID string) (*JobRecord, error) {
	if jobID == "" {
		return nil, errors.New("conversation: jobID required")
	}
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"jobId": &types.AttributeValueMemberS{Value: jobID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("conversation: failed to fetch job: %w", err)
	}
	if out.Item == nil {
		return nil, ErrJobNotFound
	}

	var job JobRecord
	if err := attributevalue.UnmarshalMap(out.Item, &job); err != nil {
		return nil, fmt.Errorf("conversation: failed to decode job: %w", err)
	}
	return &job, nil
}

func (s *JobStore) updateJob(ctx context.Context, jobID string, values map[string]types.AttributeValue, names map[string]string, expression string) error {
	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"jobId": &types.AttributeValueMemberS{Value: jobID},
		},
		UpdateExpression:          aws.String(expression),
		ExpressionAttributeNames:  names,
		ExpressionAttributeValues: values,
		ConditionExpression:       aws.String("attribute_exists(jobId)"),
	})
	if err != nil {
		return fmt.Errorf("conversation: failed to update job %s: %w", jobID, err)
	}
	return nil
}
