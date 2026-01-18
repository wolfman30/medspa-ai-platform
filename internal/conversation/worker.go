package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// PaymentNotifier sends notifications when payments are received.
type PaymentNotifier interface {
	NotifyPaymentSuccess(ctx context.Context, evt events.PaymentSucceededV1) error
}

// SandboxAutoPurger optionally purges demo/test data after sandbox payments complete.
// Implementations must be safe to call in production (no-ops unless explicitly enabled).
type SandboxAutoPurger interface {
	MaybePurgeAfterPayment(ctx context.Context, evt events.PaymentSucceededV1) error
}

// Worker consumes conversation jobs from the queue and invokes the processor.
type Worker struct {
	processor        Service
	queue            queueClient
	jobs             JobUpdater
	messenger        ReplyMessenger
	bookings         bookingConfirmer
	deposits         DepositSender
	depositPreloader *DepositPreloader
	notifier         PaymentNotifier
	autoPurge        SandboxAutoPurger
	processed        processedEventStore
	optOutChecker    OptOutChecker
	msgChecker       ProviderMessageChecker
	clinicStore      *clinic.Store
	supervisor       Supervisor
	supervisorMode   SupervisorMode
	transcript       *SMSTranscriptStore
	convStore        *ConversationStore
	logger           *logging.Logger

	cfg workerConfig
	wg  sync.WaitGroup
}

type workerConfig struct {
	workers          int
	receiveWaitSecs  int
	receiveBatchSize int
	deposit          DepositSender
	depositPreloader *DepositPreloader
	notifier         PaymentNotifier
	autoPurge        SandboxAutoPurger
	processed        processedEventStore
	optOutChecker    OptOutChecker
	msgChecker       ProviderMessageChecker
	clinicStore      *clinic.Store
	supervisor       Supervisor
	supervisorMode   SupervisorMode
	transcript       *SMSTranscriptStore
	convStore        *ConversationStore
}

const (
	defaultWorkerCount        = 2
	defaultWaitSeconds        = 2
	defaultBatchSize          = 5
	maxWaitSeconds            = 20
	maxReceiveBatchSize       = 10
	deleteTimeoutSeconds      = 5
	defaultSupervisorFallback = "Thanks for your message! A team member will follow up shortly."
)

// WorkerOption customizes worker behavior.
type WorkerOption func(*workerConfig)

type processedEventStore interface {
	AlreadyProcessed(ctx context.Context, provider, eventID string) (bool, error)
	MarkProcessed(ctx context.Context, provider, eventID string) (bool, error)
}

// OptOutChecker verifies whether a recipient has opted out of SMS.
type OptOutChecker interface {
	IsUnsubscribed(ctx context.Context, clinicID uuid.UUID, recipient string) (bool, error)
}

// ProviderMessageChecker verifies whether an inbound provider message exists.
type ProviderMessageChecker interface {
	HasProviderMessage(ctx context.Context, providerMessageID string) (bool, error)
}

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

// WithProviderMessageChecker configures a provider message lookup for stale-job detection.
func WithProviderMessageChecker(checker ProviderMessageChecker) WorkerOption {
	return func(cfg *workerConfig) {
		if checker != nil {
			cfg.msgChecker = checker
		}
	}
}

// WithDepositSender wires a deposit dispatcher used when responses include a deposit intent.
func WithDepositSender(sender DepositSender) WorkerOption {
	return func(cfg *workerConfig) {
		cfg.deposit = sender
	}
}

// WithDepositPreloader wires a preloader for parallel checkout link generation.
func WithDepositPreloader(preloader *DepositPreloader) WorkerOption {
	return func(cfg *workerConfig) {
		cfg.depositPreloader = preloader
	}
}

// WithPaymentNotifier wires a notifier to alert clinic operators on payment success.
func WithPaymentNotifier(notifier PaymentNotifier) WorkerOption {
	return func(cfg *workerConfig) {
		cfg.notifier = notifier
	}
}

// WithSMSTranscriptStore wires a Redis-backed SMS transcript store (for phone view / E2E recordings).
func WithSMSTranscriptStore(store *SMSTranscriptStore) WorkerOption {
	return func(cfg *workerConfig) {
		cfg.transcript = store
	}
}

// WithSandboxAutoPurger wires a sandbox auto purge hook that runs after payment success events.
func WithSandboxAutoPurger(purger SandboxAutoPurger) WorkerOption {
	return func(cfg *workerConfig) {
		cfg.autoPurge = purger
	}
}

// WithProcessedEventsStore provides an idempotency store for event handling (e.g. payment confirmations).
func WithProcessedEventsStore(store processedEventStore) WorkerOption {
	return func(cfg *workerConfig) {
		cfg.processed = store
	}
}

// WithOptOutChecker wires a checker to suppress outbound SMS for opted-out recipients.
func WithOptOutChecker(checker OptOutChecker) WorkerOption {
	return func(cfg *workerConfig) {
		cfg.optOutChecker = checker
	}
}

// WithSupervisor wires a reply supervisor that can review or edit outgoing messages.
func WithSupervisor(supervisor Supervisor) WorkerOption {
	return func(cfg *workerConfig) {
		cfg.supervisor = supervisor
	}
}

// WithSupervisorMode sets the supervisor handling mode (warn, block, edit).
func WithSupervisorMode(mode SupervisorMode) WorkerOption {
	return func(cfg *workerConfig) {
		cfg.supervisorMode = ParseSupervisorMode(string(mode))
	}
}

// WithConversationStore enables persistent conversation storage in PostgreSQL.
func WithConversationStore(store *ConversationStore) WorkerOption {
	return func(cfg *workerConfig) {
		cfg.convStore = store
	}
}

// WithClinicConfigStore provides a clinic config store for personalized messaging.
func WithClinicConfigStore(store *clinic.Store) WorkerOption {
	return func(cfg *workerConfig) {
		cfg.clinicStore = store
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
		supervisorMode:   SupervisorModeWarn,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return &Worker{
		processor:        processor,
		queue:            queue,
		jobs:             jobs,
		messenger:        messenger,
		bookings:         bookings,
		deposits:         cfg.deposit,
		depositPreloader: cfg.depositPreloader,
		notifier:         cfg.notifier,
		autoPurge:        cfg.autoPurge,
		processed:        cfg.processed,
		optOutChecker:    cfg.optOutChecker,
		msgChecker:       cfg.msgChecker,
		clinicStore:      cfg.clinicStore,
		supervisor:       cfg.supervisor,
		supervisorMode:   cfg.supervisorMode,
		transcript:       cfg.transcript,
		convStore:        cfg.convStore,
		logger:           logger,
		cfg:              cfg,
	}
}

func (w *Worker) appendTranscript(ctx context.Context, conversationID string, msg SMSTranscriptMessage) {
	if w == nil || strings.TrimSpace(conversationID) == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// Append to Redis (real-time, ephemeral)
	if w.transcript != nil {
		if err := w.transcript.Append(ctx, conversationID, msg); err != nil {
			w.logger.Warn("failed to append sms transcript to Redis", "error", err, "conversation_id", conversationID)
		}
	}

	// Persist to PostgreSQL (long-term history)
	if w.convStore != nil {
		if err := w.convStore.AppendMessage(ctx, conversationID, msg); err != nil {
			w.logger.Warn("failed to persist message to database", "error", err, "conversation_id", conversationID)
		}
	}
}

func (w *Worker) isOptedOut(ctx context.Context, orgID string, recipient string) bool {
	if w == nil || w.optOutChecker == nil {
		return false
	}
	orgID = strings.TrimSpace(orgID)
	recipient = strings.TrimSpace(recipient)
	if orgID == "" || recipient == "" {
		return false
	}
	clinicID, err := uuid.Parse(orgID)
	if err != nil {
		w.logger.Warn("opt-out check skipped: invalid org id", "org_id", orgID)
		return false
	}
	unsubscribed, err := w.optOutChecker.IsUnsubscribed(ctx, clinicID, recipient)
	if err != nil {
		w.logger.Warn("opt-out check failed", "error", err, "org_id", orgID)
		return false
	}
	if unsubscribed {
		w.logger.Info("suppressing sms for opted-out recipient", "org_id", orgID, "to", recipient)
	}
	return unsubscribed
}

func (w *Worker) clinicName(ctx context.Context, orgID string) string {
	cfg := w.clinicConfig(ctx, orgID)
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Name)
}

func (w *Worker) clinicConfig(ctx context.Context, orgID string) *clinic.Config {
	if w == nil || w.clinicStore == nil {
		return nil
	}
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cfg, err := w.clinicStore.Get(ctx, orgID)
	if err != nil {
		w.logger.Warn("failed to load clinic config", "error", err, "org_id", orgID)
		return nil
	}
	return cfg
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

	// Debug logging to track job processing
	w.logger.Info("worker processing job",
		"job_id", payload.ID,
		"kind", payload.Kind,
		"msg_id", msg.ID,
	)
	if payload.Kind == jobTypeMessage {
		w.logger.Info("worker job details",
			"job_id", payload.ID,
			"conversation_id", payload.Message.ConversationID,
			"message", payload.Message.Message,
			"from", payload.Message.From,
		)
	}

	if payload.Kind == jobTypeMessage && w.msgChecker != nil {
		providerID := providerMessageID(payload.Message.Metadata)
		if providerID != "" {
			exists, err := w.msgChecker.HasProviderMessage(ctx, providerID)
			if err != nil {
				w.logger.Warn("provider message lookup failed", "error", err, "provider_message_id", providerID, "job_id", payload.ID)
			} else if !exists {
				w.logger.Info("skipping conversation job: inbound message missing", "provider_message_id", providerID, "job_id", payload.ID)
				if payload.TrackStatus && w.jobs != nil {
					if storeErr := w.jobs.MarkFailed(ctx, payload.ID, "skipped: inbound message missing"); storeErr != nil {
						w.logger.Error("failed to update job status", "error", storeErr, "job_id", payload.ID)
					}
				}
				w.deleteMessage(context.Background(), msg.ReceiptHandle)
				return
			}
		}
	}

	var (
		err  error
		resp *Response
	)
	switch payload.Kind {
	case jobTypeStart:
		w.logger.Info("worker calling StartConversation", "job_id", payload.ID)
		resp, err = w.processor.StartConversation(ctx, payload.Start)
	case jobTypeMessage:
		// Pre-detect deposit intent and start parallel checkout generation
		if w.depositPreloader != nil && ShouldPreloadDeposit(payload.Message.Message) {
			w.logger.Info("deposit preloader: detected potential deposit agreement, starting parallel generation",
				"job_id", payload.ID,
				"conversation_id", payload.Message.ConversationID,
			)
			w.depositPreloader.StartPreload(ctx, payload.Message.ConversationID, payload.Message.OrgID, payload.Message.LeadID)
		}
		w.logger.Info("worker calling ProcessMessage", "job_id", payload.ID, "conversation_id", payload.Message.ConversationID)
		resp, err = w.processor.ProcessMessage(ctx, payload.Message)
	case jobTypePayment:
		err = w.handlePaymentEvent(ctx, payload.Payment)
	case jobTypePaymentFailed:
		err = w.handlePaymentFailedEvent(ctx, payload.PaymentFailed)
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
		if payload.Kind == jobTypeMessage {
			w.logger.Warn("sending fallback reply after conversation failure", "job_id", payload.ID, "org_id", payload.Message.OrgID)
			w.sendReply(ctx, payload, &Response{
				ConversationID: payload.Message.ConversationID,
				Message:        "Sorry - I'm having trouble responding right now. Please reply again in a moment.",
				Timestamp:      time.Now().UTC(),
			})
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
			blocked := w.sendReply(ctx, payload, resp)
			if !blocked {
				w.handleDepositIntent(ctx, payload.Message, resp)
			}
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

func providerMessageID(metadata map[string]string) string {
	if metadata == nil {
		return ""
	}
	if value := strings.TrimSpace(metadata["provider_message_id"]); value != "" {
		return value
	}
	if value := strings.TrimSpace(metadata["telnyx_message_id"]); value != "" {
		return value
	}
	return ""
}

func (w *Worker) sendReply(ctx context.Context, payload queuePayload, resp *Response) bool {
	if resp == nil || resp.Message == "" {
		return false
	}
	msg := payload.Message
	if msg.Channel != ChannelSMS {
		return false
	}
	if msg.From == "" || msg.To == "" {
		return false
	}
	if w.isOptedOut(ctx, msg.OrgID, msg.From) {
		return false
	}
	if w.msgChecker != nil {
		providerID := providerMessageID(msg.Metadata)
		if providerID != "" {
			exists, err := w.msgChecker.HasProviderMessage(ctx, providerID)
			if err != nil {
				w.logger.Warn("provider message lookup failed", "error", err, "provider_message_id", providerID, "job_id", payload.ID)
			} else if !exists {
				w.logger.Info("suppressing reply: inbound message missing", "provider_message_id", providerID, "job_id", payload.ID)
				return true
			}
		}
	}

	outboundText, blocked := w.applySupervisor(ctx, SupervisorRequest{
		OrgID:          msg.OrgID,
		ConversationID: msg.ConversationID,
		LeadID:         msg.LeadID,
		Channel:        msg.Channel,
		UserMessage:    msg.Message,
		DraftMessage:   resp.Message,
	})
	if blocked {
		resp = &Response{
			ConversationID: resp.ConversationID,
			Message:        outboundText,
			Timestamp:      time.Now().UTC(),
		}
	} else if outboundText != resp.Message {
		resp = &Response{
			ConversationID: resp.ConversationID,
			Message:        outboundText,
			Timestamp:      resp.Timestamp,
		}
	}

	conversationID := strings.TrimSpace(resp.ConversationID)
	if conversationID == "" {
		conversationID = strings.TrimSpace(msg.ConversationID)
	}

	if w.messenger != nil {
		sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		reply := OutboundReply{
			OrgID:          msg.OrgID,
			LeadID:         msg.LeadID,
			ConversationID: conversationID,
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

	w.appendTranscript(context.Background(), conversationID, SMSTranscriptMessage{
		Role:      "assistant",
		From:      msg.To,
		To:        msg.From,
		Body:      resp.Message,
		Timestamp: resp.Timestamp,
		Kind:      "ai_reply",
	})
	return blocked
}

func (w *Worker) applySupervisor(ctx context.Context, req SupervisorRequest) (string, bool) {
	if w == nil || w.supervisor == nil {
		return req.DraftMessage, false
	}
	draft := strings.TrimSpace(req.DraftMessage)
	if draft == "" {
		return req.DraftMessage, false
	}
	mode := w.supervisorMode
	if mode == "" {
		mode = SupervisorModeWarn
	}
	decision, err := w.supervisor.Review(ctx, req)
	if err != nil {
		w.logger.Warn("supervisor review failed; allowing reply", "error", err, "mode", mode)
		return req.DraftMessage, false
	}
	action := decision.Action
	switch mode {
	case SupervisorModeWarn:
		if action != SupervisorActionAllow {
			w.logger.Warn("supervisor flagged reply", "action", action, "reason", decision.Reason)
		}
		return req.DraftMessage, false
	case SupervisorModeBlock:
		switch action {
		case SupervisorActionBlock:
			w.logger.Warn("supervisor blocked reply", "reason", decision.Reason)
			return defaultSupervisorFallback, true
		case SupervisorActionEdit:
			if strings.TrimSpace(decision.EditedText) != "" {
				w.logger.Info("supervisor edited reply", "reason", decision.Reason)
				return decision.EditedText, false
			}
			w.logger.Warn("supervisor edit missing content; allowing reply", "reason", decision.Reason)
			return req.DraftMessage, false
		default:
			return req.DraftMessage, false
		}
	case SupervisorModeEdit:
		switch action {
		case SupervisorActionEdit:
			if strings.TrimSpace(decision.EditedText) != "" {
				w.logger.Info("supervisor edited reply", "reason", decision.Reason)
				return decision.EditedText, false
			}
			w.logger.Warn("supervisor edit missing content; allowing reply", "reason", decision.Reason)
			return req.DraftMessage, false
		case SupervisorActionBlock:
			w.logger.Warn("supervisor blocked reply", "reason", decision.Reason)
			return defaultSupervisorFallback, true
		default:
			return req.DraftMessage, false
		}
	default:
		w.logger.Warn("supervisor mode unknown; allowing reply", "mode", mode)
		return req.DraftMessage, false
	}
}

func (w *Worker) handleDepositIntent(ctx context.Context, msg MessageRequest, resp *Response) {
	if w.deposits == nil || resp == nil || resp.DepositIntent == nil {
		return
	}
	if w.isOptedOut(ctx, msg.OrgID, msg.From) {
		return
	}

	// Check for preloaded checkout link (generated in parallel with LLM call)
	if w.depositPreloader != nil {
		if preloaded := w.depositPreloader.WaitForPreloaded(msg.ConversationID, 2*time.Second); preloaded != nil {
			if preloaded.Error == nil && preloaded.URL != "" {
				resp.DepositIntent.PreloadedURL = preloaded.URL
				resp.DepositIntent.PreloadedPaymentID = preloaded.PrePaymentID.String()
				w.logger.Info("deposit: using preloaded checkout link",
					"conversation_id", msg.ConversationID,
					"preloaded_url", preloaded.URL[:min(50, len(preloaded.URL))]+"...",
				)
			}
			w.depositPreloader.ClearPreloaded(msg.ConversationID)
		}
	}

	if err := w.deposits.SendDeposit(ctx, msg, resp); err != nil {
		w.logger.Error("failed to send deposit intent", "error", err, "org_id", msg.OrgID, "lead_id", msg.LeadID)
	}
}

func (w *Worker) handlePaymentEvent(ctx context.Context, evt *events.PaymentSucceededV1) error {
	if evt == nil {
		return errors.New("conversation: missing payment payload")
	}
	idempotencyKey := strings.TrimSpace(evt.ProviderRef)
	if idempotencyKey == "" {
		idempotencyKey = strings.TrimSpace(evt.BookingIntentID)
	}
	if idempotencyKey == "" {
		idempotencyKey = strings.TrimSpace(evt.EventID)
	}
	if w.processed != nil && idempotencyKey != "" {
		already, err := w.processed.AlreadyProcessed(ctx, "conversation.payment_succeeded.v1", idempotencyKey)
		if err != nil {
			w.logger.Warn("failed to check payment event idempotency", "error", err, "key", idempotencyKey, "event_id", evt.EventID, "provider_ref", evt.ProviderRef, "org_id", evt.OrgID, "lead_id", evt.LeadID)
		} else if already {
			w.logger.Info("skipping duplicate payment success event", "key", idempotencyKey, "event_id", evt.EventID, "provider_ref", evt.ProviderRef, "org_id", evt.OrgID, "lead_id", evt.LeadID)
			return nil
		}
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

	// Notify clinic operators about the payment (non-blocking)
	if w.notifier != nil {
		if err := w.notifier.NotifyPaymentSuccess(ctx, *evt); err != nil {
			w.logger.Error("failed to send payment notification to clinic", "error", err, "org_id", evt.OrgID, "lead_id", evt.LeadID)
			// Don't fail the payment flow if notification fails
		}
	}

	if evt.LeadPhone != "" && evt.FromNumber != "" {
		if !w.isOptedOut(ctx, evt.OrgID, evt.LeadPhone) {
			cfg := w.clinicConfig(ctx, evt.OrgID)
			var clinicName, bookingURL, callbackTime string
			if cfg != nil {
				clinicName = strings.TrimSpace(cfg.Name)
				bookingURL = strings.TrimSpace(cfg.BookingURL)
				callbackTime = cfg.ExpectedCallbackTime(time.Now())
			}
			if callbackTime == "" {
				callbackTime = "within 24 hours" // fallback
			}
			body := paymentConfirmationMessage(evt, clinicName, bookingURL, callbackTime)

			if w.messenger == nil {
				// Transcript is still recorded even when SMS sending is disabled.
			} else {
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

			w.appendTranscript(context.Background(), smsConversationID(evt.OrgID, evt.LeadPhone), SMSTranscriptMessage{
				Role: "assistant",
				From: evt.FromNumber,
				To:   evt.LeadPhone,
				Body: body,
				Kind: "payment_confirmation",
			})
		}
	}
	if w.processed != nil && idempotencyKey != "" {
		if _, err := w.processed.MarkProcessed(ctx, "conversation.payment_succeeded.v1", idempotencyKey); err != nil {
			w.logger.Warn("failed to mark payment event processed", "error", err, "key", idempotencyKey, "event_id", evt.EventID, "provider_ref", evt.ProviderRef, "org_id", evt.OrgID, "lead_id", evt.LeadID)
		}
	}
	if w.autoPurge != nil {
		if err := w.autoPurge.MaybePurgeAfterPayment(ctx, *evt); err != nil {
			w.logger.Warn("sandbox auto purge hook failed", "error", err, "org_id", evt.OrgID, "lead_id", evt.LeadID, "provider_ref", evt.ProviderRef)
		}
	}
	return nil
}

func paymentConfirmationMessage(evt *events.PaymentSucceededV1, clinicName, bookingURL, callbackTime string) string {
	if evt == nil {
		return ""
	}
	name := strings.TrimSpace(clinicName)
	bookingURL = strings.TrimSpace(bookingURL)
	callbackTime = strings.TrimSpace(callbackTime)
	if callbackTime == "" {
		callbackTime = "within 24 hours"
	}

	// Build the booking link section if URL is configured
	// Provides context that booking online is optional and staff will call regardless
	var bookingSection string
	if bookingURL != "" {
		bookingSection = fmt.Sprintf("\n\nWant to lock in your preferred date and time now? You can schedule online here: %s\n\nThis is completely optional - our team will call you either way. The link just helps expedite the process if you'd like to confirm sooner.", bookingURL)
	}

	if evt.ScheduledFor != nil {
		date := evt.ScheduledFor.Format("Monday, January 2 at 3:04 PM")
		if name != "" {
			return fmt.Sprintf("Payment received! Your appointment on %s is confirmed. A %s team member will call you %s with final details.%s", date, name, callbackTime, bookingSection)
		}
		return fmt.Sprintf("Payment received! Your appointment on %s is confirmed. Our team will call %s with final details.%s", date, callbackTime, bookingSection)
	}
	amount := float64(evt.AmountCents) / 100
	if name != "" {
		return fmt.Sprintf("Payment of $%.2f received - thank you! A %s team member will call you %s to confirm your appointment.%s", amount, name, callbackTime, bookingSection)
	}
	return fmt.Sprintf("Payment of $%.2f received - thank you! Our team will call you %s to confirm your appointment.%s", amount, callbackTime, bookingSection)
}

func (w *Worker) handlePaymentFailedEvent(ctx context.Context, evt *events.PaymentFailedV1) error {
	if evt == nil {
		return errors.New("conversation: missing payment failed payload")
	}
	idempotencyKey := strings.TrimSpace(evt.ProviderRef)
	if idempotencyKey == "" {
		idempotencyKey = strings.TrimSpace(evt.BookingIntentID)
	}
	if idempotencyKey == "" {
		idempotencyKey = strings.TrimSpace(evt.EventID)
	}
	if w.processed != nil && idempotencyKey != "" {
		already, err := w.processed.AlreadyProcessed(ctx, "conversation.payment_failed.v1", idempotencyKey)
		if err != nil {
			w.logger.Warn("failed to check payment failed event idempotency", "error", err, "key", idempotencyKey, "event_id", evt.EventID, "provider_ref", evt.ProviderRef, "org_id", evt.OrgID, "lead_id", evt.LeadID)
		} else if already {
			w.logger.Info("skipping duplicate payment failed event", "key", idempotencyKey, "event_id", evt.EventID, "provider_ref", evt.ProviderRef, "org_id", evt.OrgID, "lead_id", evt.LeadID)
			return nil
		}
	}

	if w.messenger != nil && evt.LeadPhone != "" && evt.FromNumber != "" {
		if !w.isOptedOut(ctx, evt.OrgID, evt.LeadPhone) {
			body := "Payment failed - we didn't receive your deposit. If you'd still like to book, please reply and we can send a new secure payment link. Our team can also help by phone."
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
				w.logger.Error("failed to send payment failed sms", "error", err, "event_id", evt.EventID, "org_id", evt.OrgID)
			}
		}
	}
	if w.processed != nil && idempotencyKey != "" {
		if _, err := w.processed.MarkProcessed(ctx, "conversation.payment_failed.v1", idempotencyKey); err != nil {
			w.logger.Warn("failed to mark payment failed event processed", "error", err, "key", idempotencyKey, "event_id", evt.EventID, "provider_ref", evt.ProviderRef, "org_id", evt.OrgID, "lead_id", evt.LeadID)
		}
	}
	return nil
}

func smsConversationID(orgID string, phone string) string {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return ""
	}
	digits := sanitizeDigits(phone)
	digits = normalizeUSDigits(digits)
	if digits == "" {
		return ""
	}
	return fmt.Sprintf("sms:%s:%s", orgID, digits)
}
