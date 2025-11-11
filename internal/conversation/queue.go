package conversation

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

type queueClient interface {
	Send(ctx context.Context, body string) error
	Receive(ctx context.Context, maxMessages int, waitSeconds int) ([]queueMessage, error)
	Delete(ctx context.Context, receiptHandle string) error
}

type queueMessage struct {
	ID            string
	Body          string
	ReceiptHandle string
}

type jobType string

const (
	jobTypeStart   jobType = "start"
	jobTypeMessage jobType = "message"
)

type queuePayload struct {
	ID      string         `json:"id"`
	Kind    jobType        `json:"kind"`
	Start   StartRequest   `json:"start,omitempty"`
	Message MessageRequest `json:"message,omitempty"`
}

func encodePayload(kind jobType, jobID string, start StartRequest, message MessageRequest) (queuePayload, string, error) {
	if jobID == "" {
		jobID = uuid.NewString()
	}
	payload := queuePayload{
		ID:      jobID,
		Kind:    kind,
		Start:   start,
		Message: message,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return queuePayload{}, "", fmt.Errorf("conversation: failed to encode payload: %w", err)
	}

	return payload, string(body), nil
}
