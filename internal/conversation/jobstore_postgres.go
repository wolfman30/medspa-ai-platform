package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGJobStore persists job records to PostgreSQL for bootstrap deployments.
type PGJobStore struct {
	db *pgxpool.Pool
}

// NewPGJobStore builds a Postgres-backed JobStore.
func NewPGJobStore(db *pgxpool.Pool) *PGJobStore {
	if db == nil {
		panic("conversation: pgx pool cannot be nil")
	}
	return &PGJobStore{db: db}
}

var _ JobRecorder = (*PGJobStore)(nil)
var _ JobUpdater = (*PGJobStore)(nil)

// PutPending inserts a pending job record.
func (s *PGJobStore) PutPending(ctx context.Context, job *JobRecord) error {
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

	startJSON, err := marshalJSON(job.StartRequest)
	if err != nil {
		return err
	}
	msgJSON, err := marshalJSON(job.MessageRequest)
	if err != nil {
		return err
	}
	respJSON, err := marshalJSON(job.Response)
	if err != nil {
		return err
	}

	expiresAt := time.Unix(job.ExpiresAt, 0).UTC()
	if _, execErr := s.db.Exec(ctx, `
		INSERT INTO conversation_jobs (
			job_id, status, request_type, conversation_id,
			start_request, message_request, response, error_message,
			created_at, updated_at, expires_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	`, job.JobID, job.Status, job.RequestType, nullString(job.ConversationID), startJSON, msgJSON, respJSON, job.ErrorMessage, now, now, expiresAt); execErr != nil {
		return fmt.Errorf("conversation: failed to persist job: %w", execErr)
	}
	return nil
}

// MarkCompleted updates the job as completed with the final response.
func (s *PGJobStore) MarkCompleted(ctx context.Context, jobID string, resp *Response, conversationID string) error {
	if jobID == "" {
		return errors.New("conversation: jobID required")
	}
	respJSON, err := marshalJSON(resp)
	if err != nil {
		return err
	}

	result, execErr := s.db.Exec(ctx, `
		UPDATE conversation_jobs
		SET status = $2,
		    response = $3,
		    conversation_id = $4,
		    error_message = '',
		    updated_at = $5
		WHERE job_id = $1
	`, jobID, JobStatusCompleted, respJSON, nullString(conversationID), time.Now().UTC())
	if execErr != nil {
		return fmt.Errorf("conversation: failed to update job: %w", execErr)
	}
	if result.RowsAffected() == 0 {
		return ErrJobNotFound
	}
	return nil
}

// MarkFailed marks the job as failed with an error message.
func (s *PGJobStore) MarkFailed(ctx context.Context, jobID string, errMsg string) error {
	if jobID == "" {
		return errors.New("conversation: jobID required")
	}

	result, execErr := s.db.Exec(ctx, `
		UPDATE conversation_jobs
		SET status = $2,
		    response = NULL,
		    error_message = $3,
		    updated_at = $4
		WHERE job_id = $1
	`, jobID, JobStatusFailed, errMsg, time.Now().UTC())
	if execErr != nil {
		return fmt.Errorf("conversation: failed to update job: %w", execErr)
	}
	if result.RowsAffected() == 0 {
		return ErrJobNotFound
	}
	return nil
}

// GetJob loads a job by ID.
func (s *PGJobStore) GetJob(ctx context.Context, jobID string) (*JobRecord, error) {
	if jobID == "" {
		return nil, errors.New("conversation: jobID required")
	}

	var (
		startJSON    []byte
		messageJSON  []byte
		responseJSON []byte
		convoID      pgtype.Text
		createdAt    time.Time
		updatedAt    time.Time
		expiresAt    pgtype.Timestamptz
		status       string
		reqType      string
		errMsg       string
	)

	row := s.db.QueryRow(ctx, `
		SELECT job_id, status, request_type, conversation_id,
		       start_request, message_request, response, error_message,
		       created_at, updated_at, expires_at
		FROM conversation_jobs
		WHERE job_id = $1
	`, jobID)

	if err := row.Scan(&jobID, &status, &reqType, &convoID,
		&startJSON, &messageJSON, &responseJSON, &errMsg,
		&createdAt, &updatedAt, &expiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrJobNotFound
		}
		return nil, fmt.Errorf("conversation: failed to fetch job: %w", err)
	}

	job := &JobRecord{
		JobID:        jobID,
		Status:       JobStatus(status),
		RequestType:  jobType(reqType),
		ErrorMessage: errMsg,
		CreatedAt:    createdAt.Format(time.RFC3339Nano),
		UpdatedAt:    updatedAt.Format(time.RFC3339Nano),
	}
	if convoID.Valid {
		job.ConversationID = convoID.String
	}
	if expiresAt.Valid {
		job.ExpiresAt = expiresAt.Time.Unix()
	}

	if len(startJSON) > 0 {
		var sr StartRequest
		if err := json.Unmarshal(startJSON, &sr); err != nil {
			return nil, fmt.Errorf("conversation: failed to decode start_request: %w", err)
		}
		job.StartRequest = &sr
	}
	if len(messageJSON) > 0 {
		var mr MessageRequest
		if err := json.Unmarshal(messageJSON, &mr); err != nil {
			return nil, fmt.Errorf("conversation: failed to decode message_request: %w", err)
		}
		job.MessageRequest = &mr
	}
	if len(responseJSON) > 0 {
		var resp Response
		if err := json.Unmarshal(responseJSON, &resp); err != nil {
			return nil, fmt.Errorf("conversation: failed to decode response: %w", err)
		}
		job.Response = &resp
	}

	return job, nil
}

func marshalJSON(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("conversation: failed to encode json: %w", err)
	}
	return data, nil
}

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}