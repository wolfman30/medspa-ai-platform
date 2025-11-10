package conversation

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// SQSQueue implements queueClient backed by AWS/LocalStack SQS.
type SQSQueue struct {
	client   *sqs.Client
	queueURL string
}

// NewSQSQueue creates a queue wrapper around the provided SQS client.
func NewSQSQueue(client *sqs.Client, queueURL string) *SQSQueue {
	if client == nil {
		panic("conversation: SQS client cannot be nil")
	}
	if queueURL == "" {
		panic("conversation: SQS queueURL cannot be empty")
	}
	return &SQSQueue{
		client:   client,
		queueURL: queueURL,
	}
}

func (q *SQSQueue) Send(ctx context.Context, body string) error {
	_, err := q.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(q.queueURL),
		MessageBody: aws.String(body),
	})
	if err != nil {
		return fmt.Errorf("conversation: failed to send SQS message: %w", err)
	}
	return nil
}

func (q *SQSQueue) Receive(ctx context.Context, maxMessages int, waitSeconds int) ([]queueMessage, error) {
	input := &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(q.queueURL),
		MaxNumberOfMessages: int32(maxMessages),
		WaitTimeSeconds:     int32(waitSeconds),
	}

	output, err := q.client.ReceiveMessage(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("conversation: failed to receive SQS messages: %w", err)
	}

	messages := make([]queueMessage, 0, len(output.Messages))
	for _, msg := range output.Messages {
		messages = append(messages, queueMessage{
			ID:            aws.ToString(msg.MessageId),
			Body:          aws.ToString(msg.Body),
			ReceiptHandle: aws.ToString(msg.ReceiptHandle),
		})
	}

	return messages, nil
}

func (q *SQSQueue) Delete(ctx context.Context, receiptHandle string) error {
	if receiptHandle == "" {
		return nil
	}

	_, err := q.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(q.queueURL),
		ReceiptHandle: aws.String(receiptHandle),
	})
	if err != nil {
		return fmt.Errorf("conversation: failed to delete SQS message: %w", err)
	}
	return nil
}
