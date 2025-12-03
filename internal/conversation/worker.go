package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// Worker consumes conversation jobs from the queue and invokes the processor.
type Worker struct {
	processor Service
	queue     queueClient
	jobs      JobUpdater
	messenger ReplyMessenger
	bookings  bookingConfirmer
	deposits  DepositSender
	logger    *logging.Logger

	cfg workerConfig
	wg  sync.WaitGroup
}

type workerConfig struct {
	workers          int
	receiveWaitSecs  int
	receiveBatchSize int
	deposit          DepositSender
}

const (
	defaultWorkerCount   = 2
	defaultWaitSeconds   = 2
	defaultBatchSize     = 5
	maxWaitSeconds       = 20
	maxReceiveBatchSize  = 10
	deleteTimeoutSeconds = 5
)

// WorkerOption customizes worker behavior.
type WorkerOption func(*workerConfig)

// WithWorkerCount sets the number of concurrent consumer goroutines.
func WithWorkerCount(count int) WorkerOption {
	return func(cfg *workerConfig) {
		if count > 0 {
			cfg.workers = count
		}
	}
}

// WithReceiveWaitSeconds sets the SQS long-poll wait duration.
func WithReceiveWaitSeconds(seconds int) WorkerOption {
	return func(cfg *workerConfig) {
		if seconds < 0 {
			return
		}
		if seconds > maxWaitSeconds {
			seconds = maxWaitSeconds
		}
		cfg.receiveWaitSecs = seconds
	}
}

// WithReceiveBatchSize sets how many messages to fetch per poll.
func WithReceiveBatchSize(size int) WorkerOption {
	return func(cfg *workerConfig) {
		if size <= 0 {
			return
		}
		if size > maxReceiveBatchSize {
			size = maxReceiveBatchSize
		}
		cfg.receiveBatchSize = size
	}
}

// WithDepositSender wires a deposit dispatcher used when responses include a deposit intent.
func WithDepositSender(sender DepositSender) WorkerOption {
	return func(cfg *workerConfig) {
		cfg.deposit = sender
	}
}

// NewWorker constructs a queue consumer around the provided processor.
type bookingConfirmer interface {
	ConfirmBooking(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID, scheduledFor *time.Time) error
}

type DepositSender interface {
	SendDeposit(ctx context.Context, msg MessageRequest, resp *Response) error
}

func NewWorker(processor Service, queue queueClient, jobs JobUpdater, messenger ReplyMessenger, bookings bookingConfirmer, logger *logging.Logger, opts ...WorkerOption) *Worker {
	if processor == nil {
		panic("conversation: processor cannot be nil")
	}
	if queue == nil {
		panic("conversation: queue cannot be nil")
	}
	if jobs == nil {
		panic("conversation: job store cannot be nil")
	}
	if logger == nil {
		logger = logging.Default()
	}

	cfg := workerConfig{
		workers:          defaultWorkerCount,
		receiveWaitSecs:  defaultWaitSeconds,
		receiveBatchSize: defaultBatchSize,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return &Worker{
		processor: processor,
		queue:     queue,
		jobs:      jobs,
		messenger: messenger,
		bookings:  bookings,
		deposits:  cfg.deposit,
		logger:    logger,
		cfg:       cfg,
	}
}

// Start launches worker goroutines until ctx is cancelled.
func (w *Worker) Start(ctx context.Context) {
	for i := 0; i < w.cfg.workers; i++ {
		w.wg.Add(1)
		go w.run(ctx, i+1)
	}
}

// Wait blocks until all worker goroutines exit.
func (w *Worker) Wait() {
	w.wg.Wait()
}

func (w *Worker) run(ctx context.Context, workerID int) {
	defer w.wg.Done()
	w.logger.Debug("conversation worker started", "worker_id", workerID)

	backoff := time.Second

	for {
		select {
		case <-ctx.Done():
			w.logger.Debug("conversation worker stopping", "worker_id", workerID)
			return
		default:
		}

		messages, err := w.queue.Receive(ctx, w.cfg.receiveBatchSize, w.cfg.receiveWaitSecs)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			w.logger.Error("failed to receive conversation jobs", "error", err, "worker_id", workerID)
			time.Sleep(backoff)
			if backoff < 5*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second

		for _, msg := range messages {
			w.handleMessage(ctx, msg)
		}
	}
}

func (w *Worker) handleMessage(ctx context.Context, msg queueMessage) {
	var payload queuePayload
	if err := json.Unmarshal([]byte(msg.Body), &payload); err != nil {
		w.logger.Error("failed to decode conversation job", "error", err)
		w.deleteMessage(context.Background(), msg.ReceiptHandle)
		return
	}

	var (
		err  error
		resp *Response
	)
	switch payload.Kind {
	case jobTypeStart:
		resp, err = w.processor.StartConversation(ctx, payload.Start)
	case jobTypeMessage:
		resp, err = w.processor.ProcessMessage(ctx, payload.Message)
	case jobTypePayment:
		err = w.handlePaymentEvent(ctx, payload.Payment)
	default:
		err = fmt.Errorf("conversation: unknown job type %q", payload.Kind)
	}

	if err != nil {
		w.logger.Error("conversation job failed", "error", err, "job_id", payload.ID, "kind", payload.Kind)
		if payload.TrackStatus {
			if storeErr := w.jobs.MarkFailed(ctx, payload.ID, err.Error()); storeErr != nil {
				w.logger.Error("failed to update job status", "error", storeErr, "job_id", payload.ID)
			}
		}
	} else {
		w.logger.Debug("conversation job processed", "job_id", payload.ID, "kind", payload.Kind)
		var convID string
		if resp != nil {
			convID = resp.ConversationID
			if convID == "" && payload.Kind == jobTypeMessage {
				convID = payload.Message.ConversationID
			}
		}
		if payload.TrackStatus {
			if storeErr := w.jobs.MarkCompleted(ctx, payload.ID, resp, convID); storeErr != nil {
				w.logger.Error("failed to update job status", "error", storeErr, "job_id", payload.ID)
			}
		}
		if payload.Kind == jobTypeMessage {
			w.sendReply(ctx, payload, resp)
			w.handleDepositIntent(ctx, payload.Message, resp)
		}
	}

	w.deleteMessage(context.Background(), msg.ReceiptHandle)
}

func (w *Worker) deleteMessage(ctx context.Context, receiptHandle string) {
	if receiptHandle == "" {
		return
	}

	deleteCtx, cancel := context.WithTimeout(ctx, deleteTimeoutSeconds*time.Second)
	defer cancel()

	if err := w.queue.Delete(deleteCtx, receiptHandle); err != nil {
		w.logger.Error("failed to delete conversation job", "error", err)
	}
}

func (w *Worker) sendReply(ctx context.Context, payload queuePayload, resp *Response) {
	if w.messenger == nil || resp == nil || resp.Message == "" {
		return
	}
	msg := payload.Message
	if msg.Channel != ChannelSMS {
		return
	}
	if msg.From == "" || msg.To == "" {
		return
	}

	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	reply := OutboundReply{
		OrgID:          msg.OrgID,
		LeadID:         msg.LeadID,
		ConversationID: resp.ConversationID,
		To:             msg.From,
		From:           msg.To,
		Body:           resp.Message,
		Metadata: map[string]string{
			"job_id": payload.ID,
		},
	}

	if err := w.messenger.SendReply(sendCtx, reply); err != nil {
		w.logger.Error("failed to send outbound reply", "error", err, "job_id", payload.ID, "org_id", msg.OrgID)
	}
}

func (w *Worker) handleDepositIntent(ctx context.Context, msg MessageRequest, resp *Response) {
	if w.deposits == nil || resp == nil || resp.DepositIntent == nil {
		return
	}
	if err := w.deposits.SendDeposit(ctx, msg, resp); err != nil {
		w.logger.Error("failed to send deposit intent", "error", err, "org_id", msg.OrgID, "lead_id", msg.LeadID)
	}
}

func (w *Worker) handlePaymentEvent(ctx context.Context, evt *events.PaymentSucceededV1) error {
	if evt == nil {
		return errors.New("conversation: missing payment payload")
	}
	if w.bookings == nil {
		return nil
	}
	orgID, err := uuid.Parse(evt.OrgID)
	if err != nil {
		return fmt.Errorf("conversation: invalid org id: %w", err)
	}
	leadID, err := uuid.Parse(evt.LeadID)
	if err != nil {
		return fmt.Errorf("conversation: invalid lead id: %w", err)
	}
	if err := w.bookings.ConfirmBooking(ctx, orgID, leadID, evt.ScheduledFor); err != nil {
		return fmt.Errorf("conversation: confirm booking failed: %w", err)
	}
	if w.messenger != nil && evt.LeadPhone != "" && evt.FromNumber != "" {
		body := fmt.Sprintf("Payment of $%.2f received. Your appointment is confirmed! We'll share final details shortly.", float64(evt.AmountCents)/100)
		if evt.ScheduledFor != nil {
			body = fmt.Sprintf("Your appointment on %s is confirmed. See you soon!", evt.ScheduledFor.Format(time.RFC1123))
		}
		reply := OutboundReply{
			OrgID:          evt.OrgID,
			LeadID:         evt.LeadID,
			ConversationID: "",
			To:             evt.LeadPhone,
			From:           evt.FromNumber,
			Body:           body,
			Metadata: map[string]string{
				"event_id": evt.EventID,
			},
		}
		sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := w.messenger.SendReply(sendCtx, reply); err != nil {
			w.logger.Error("failed to send booking confirmation sms", "error", err, "event_id", evt.EventID, "org_id", evt.OrgID)
		}
	}
	return nil
}
