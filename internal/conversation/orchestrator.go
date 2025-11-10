package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// Dispatcher exposes the queue-backed entrypoints used by HTTP handlers.
type Dispatcher interface {
	StartConversation(ctx context.Context, req StartRequest) (*Response, error)
	ProcessMessage(ctx context.Context, req MessageRequest) (*Response, error)
	Shutdown(ctx context.Context) error
}

// ErrOrchestratorClosed indicates the dispatcher is no longer accepting work.
var ErrOrchestratorClosed = errors.New("conversation: orchestrator closed")

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

// Orchestrator routes conversation work through a queue before invoking the
// downstream conversation engine. This allows the system to point at LocalStack
// SQS during development and swap to AWS SQS in production without touching the
// HTTP handlers.
type Orchestrator struct {
	processor Service
	queue     queueClient
	logger    *logging.Logger

	cfg orchestratorConfig

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	pending sync.Map // jobID -> chan dispatchResult
}

var _ Service = (*Orchestrator)(nil)
var _ Dispatcher = (*Orchestrator)(nil)

const (
	defaultWorkers          = 2
	defaultReceiveWait      = 2  // seconds
	defaultReceiveMax       = 5  // messages
	maxReceiveWaitSeconds   = 20 // SQS limit
	maxReceiveBatchMessages = 10
)

type orchestratorConfig struct {
	workers          int
	receiveWaitSecs  int
	receiveBatchSize int
}

// OrchestratorOption configures the dispatcher.
type OrchestratorOption func(*orchestratorConfig)

// WithWorkerCount overrides the number of queue polling goroutines.
func WithWorkerCount(workers int) OrchestratorOption {
	return func(cfg *orchestratorConfig) {
		if workers > 0 {
			cfg.workers = workers
		}
	}
}

// WithReceiveWaitSeconds sets the long-poll wait time for ReceiveMessage calls.
func WithReceiveWaitSeconds(seconds int) OrchestratorOption {
	return func(cfg *orchestratorConfig) {
		if seconds < 0 {
			return
		}
		if seconds > maxReceiveWaitSeconds {
			seconds = maxReceiveWaitSeconds
		}
		cfg.receiveWaitSecs = seconds
	}
}

// WithReceiveBatchSize overrides how many messages each poll should return.
func WithReceiveBatchSize(size int) OrchestratorOption {
	return func(cfg *orchestratorConfig) {
		if size <= 0 {
			return
		}
		if size > maxReceiveBatchMessages {
			size = maxReceiveBatchMessages
		}
		cfg.receiveBatchSize = size
	}
}

// NewOrchestrator wires a queue-backed dispatcher around the supplied service.
func NewOrchestrator(processor Service, queue queueClient, logger *logging.Logger, opts ...OrchestratorOption) *Orchestrator {
	if processor == nil {
		panic("conversation: processor cannot be nil")
	}
	if queue == nil {
		panic("conversation: queue cannot be nil")
	}
	if logger == nil {
		logger = logging.Default()
	}

	cfg := orchestratorConfig{
		workers:          defaultWorkers,
		receiveWaitSecs:  defaultReceiveWait,
		receiveBatchSize: defaultReceiveMax,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	ctx, cancel := context.WithCancel(context.Background())
	o := &Orchestrator{
		processor: processor,
		queue:     queue,
		logger:    logger,
		cfg:       cfg,
		ctx:       ctx,
		cancel:    cancel,
	}

	for i := 0; i < cfg.workers; i++ {
		o.wg.Add(1)
		go o.runWorker(i + 1)
	}

	return o
}

// StartConversation enqueues the request and blocks until the downstream
// processor completes.
func (o *Orchestrator) StartConversation(ctx context.Context, req StartRequest) (*Response, error) {
	return o.enqueue(ctx, jobTypeStart, req, MessageRequest{})
}

// ProcessMessage enqueues a conversation turn and returns the processed output.
func (o *Orchestrator) ProcessMessage(ctx context.Context, req MessageRequest) (*Response, error) {
	return o.enqueue(ctx, jobTypeMessage, StartRequest{}, req)
}

// Shutdown stops worker goroutines and notifies any pending callers.
func (o *Orchestrator) Shutdown(ctx context.Context) error {
	o.cancel()

	done := make(chan struct{})
	go func() {
		o.wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
	}

	o.pending.Range(func(key, value any) bool {
		if ch, ok := value.(chan dispatchResult); ok {
			select {
			case ch <- dispatchResult{err: ErrOrchestratorClosed}:
			default:
			}
		}
		o.pending.Delete(key)
		return true
	})

	return nil
}

func (o *Orchestrator) enqueue(ctx context.Context, kind jobType, startReq StartRequest, msgReq MessageRequest) (*Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	jobID := uuid.NewString()
	payload := queuePayload{
		ID:      jobID,
		Kind:    kind,
		Start:   startReq,
		Message: msgReq,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("conversation: failed to encode payload: %w", err)
	}

	resultCh := make(chan dispatchResult, 1)
	o.pending.Store(jobID, resultCh)
	defer o.pending.Delete(jobID)

	if err := o.queue.Send(ctx, string(body)); err != nil {
		return nil, fmt.Errorf("conversation: failed to enqueue job: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-resultCh:
		return res.response, res.err
	}
}

func (o *Orchestrator) runWorker(workerID int) {
	defer o.wg.Done()
	o.logger.Debug("conversation orchestrator worker started", "worker_id", workerID)

	backoff := time.Second

	for {
		select {
		case <-o.ctx.Done():
			o.logger.Debug("conversation orchestrator worker stopping", "worker_id", workerID)
			return
		default:
		}

		messages, err := o.queue.Receive(o.ctx, o.cfg.receiveBatchSize, o.cfg.receiveWaitSecs)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			o.logger.Error("failed to receive conversation jobs", "error", err, "worker_id", workerID)
			time.Sleep(backoff)
			if backoff < 5*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second

		if len(messages) == 0 {
			continue
		}

		for _, msg := range messages {
			o.handleQueueMessage(msg)
		}
	}
}

func (o *Orchestrator) handleQueueMessage(msg queueMessage) {
	var payload queuePayload
	if err := json.Unmarshal([]byte(msg.Body), &payload); err != nil {
		o.logger.Error("failed to decode conversation job", "error", err)
		deleteCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = o.queue.Delete(deleteCtx, msg.ReceiptHandle)
		return
	}

	var (
		resp *Response
		err  error
	)

	processingCtx := o.ctx

	switch payload.Kind {
	case jobTypeStart:
		resp, err = o.processor.StartConversation(processingCtx, payload.Start)
	case jobTypeMessage:
		resp, err = o.processor.ProcessMessage(processingCtx, payload.Message)
	default:
		err = fmt.Errorf("conversation: unknown job type %q", payload.Kind)
	}

	deleteCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if delErr := o.queue.Delete(deleteCtx, msg.ReceiptHandle); delErr != nil {
		o.logger.Error("failed to delete conversation job", "error", delErr)
	}

	o.deliverResult(payload.ID, resp, err)
}

func (o *Orchestrator) deliverResult(jobID string, resp *Response, err error) {
	value, ok := o.pending.Load(jobID)
	if !ok {
		o.logger.Debug("no waiting caller for conversation job", "job_id", jobID)
		return
	}

	ch, ok := value.(chan dispatchResult)
	if !ok {
		o.logger.Error("conversation orchestrator pending map corrupted", "job_id", jobID)
		o.pending.Delete(jobID)
		return
	}

	select {
	case ch <- dispatchResult{response: resp, err: err}:
	default:
	}
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

type dispatchResult struct {
	response *Response
	err      error
}
