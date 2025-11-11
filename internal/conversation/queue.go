package conversation

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/wolfman30/medspa-ai-platform/internal/events"
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
	jobTypePayment jobType = "payment_succeeded.v1"
)

type queuePayload struct {
	ID          string                     `json:"id"`
	Kind        jobType                    `json:"kind"`
	Start       StartRequest               `json:"start,omitempty"`
	Message     MessageRequest             `json:"message,omitempty"`
	TrackStatus bool                       `json:"track_status"`
	Payment     *events.PaymentSucceededV1 `json:"payment,omitempty"`
}

type PublishOption func(*queuePayload)

// WithoutJobTracking disables job status persistence for fire-and-forget work.
func WithoutJobTracking() PublishOption {
	return func(p *queuePayload) {
		p.TrackStatus = false
	}
}

func encodePayload(payload queuePayload) (queuePayload, string, error) {
	if payload.ID == "" {
		payload.ID = uuid.NewString()
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return queuePayload{}, "", fmt.Errorf("conversation: failed to encode payload: %w", err)
	}

	return payload, string(body), nil
}
