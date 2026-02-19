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

	"github.com/wolfman30/medspa-ai-platform/internal/browser"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	moxieclient "github.com/wolfman30/medspa-ai-platform/internal/moxie"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// BrowserBookingClient is the subset of browser.Client used for Moxie booking sessions.
// Moxie clinics do NOT use Square — the sidecar automates Steps 1-4, then hands off
// Moxie's Step 5 payment page URL so the patient can enter their card and finalize.
type BrowserBookingClient interface {
	StartBookingSession(ctx context.Context, req browser.BookingStartRequest) (*browser.BookingStartResponse, error)
	GetHandoffURL(ctx context.Context, sessionID string) (*browser.BookingHandoffResponse, error)
	GetBookingStatus(ctx context.Context, sessionID string) (*browser.BookingStatusResponse, error)
	CancelBookingSession(ctx context.Context, sessionID string) error
}

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
	browserBooking   BrowserBookingClient
	moxieClient      *moxieclient.Client
	leadsRepo        leads.Repository
	logger           *logging.Logger
	events           *EventLogger

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
	browserBooking   BrowserBookingClient
	moxieClient      *moxieclient.Client
	leadsRepo        leads.Repository
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

// WithBrowserBookingClient wires a browser sidecar client for Moxie booking sessions.
func WithBrowserBookingClient(client BrowserBookingClient) WorkerOption {
	return func(cfg *workerConfig) {
		cfg.browserBooking = client
	}
}

// WithWorkerMoxieClient wires a direct Moxie GraphQL API client for booking creation.
// When set, Moxie clinics will use the API directly instead of browser automation.
func WithWorkerMoxieClient(client *moxieclient.Client) WorkerOption {
	return func(cfg *workerConfig) {
		cfg.moxieClient = client
	}
}

// WithWorkerLeadsRepo wires a leads repository for booking session updates.
func WithWorkerLeadsRepo(repo leads.Repository) WorkerOption {
	return func(cfg *workerConfig) {
		cfg.leadsRepo = repo
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
		browserBooking:   cfg.browserBooking,
		moxieClient:      cfg.moxieClient,
		leadsRepo:        cfg.leadsRepo,
		logger:           logger,
		events:           NewEventLogger(logger),
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
			w.depositPreloader.StartPreload(ctx, payload.Message.ConversationID, payload.Message.OrgID, payload.Message.LeadID, payload.Message.To)
		}
		// Set up progress callback to send intermediate SMS during long searches
		progressSent := make(map[string]bool)
		payload.Message.OnProgress = func(progressCtx context.Context, msg string) {
			if w.messenger == nil || progressSent[msg] {
				return
			}
			progressSent[msg] = true
			reply := OutboundReply{
				OrgID:          payload.Message.OrgID,
				LeadID:         payload.Message.LeadID,
				ConversationID: payload.Message.ConversationID,
				To:             payload.Message.From,
				From:           payload.Message.To,
				Body:           msg,
			}
			sendCtx, cancel := context.WithTimeout(progressCtx, 5*time.Second)
			defer cancel()
			if err := w.messenger.SendReply(sendCtx, reply); err != nil {
				w.logger.Warn("failed to send progress SMS", "error", err)
			}
			// Save progress messages to transcript so they appear in admin UI
			progressMsg := SMSTranscriptMessage{
				Role:      "assistant",
				Body:      msg,
				From:      payload.Message.To,
				To:        payload.Message.From,
				Timestamp: time.Now(),
			}
			if w.transcript != nil {
				_ = w.transcript.Append(progressCtx, payload.Message.ConversationID, progressMsg)
			}
			// Also persist to database for admin portal history
			if w.convStore != nil {
				_ = w.convStore.AppendMessage(progressCtx, payload.Message.ConversationID, progressMsg)
			}
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
		// Handle time selection for StartConversation (first message had all qualifications)
		if payload.Kind == jobTypeStart && resp != nil && resp.TimeSelectionResponse != nil && resp.TimeSelectionResponse.SMSMessage != "" {
			// Send the LLM reply first, then the time selection SMS
			w.sendReply(ctx, payload, resp)
			// Build a MessageRequest-like struct for handleTimeSelectionResponse
			startMsg := MessageRequest{
				OrgID:          payload.Start.OrgID,
				LeadID:         payload.Start.LeadID,
				ConversationID: resp.ConversationID,
				From:           payload.Start.From,
				To:             payload.Start.To,
				Channel:        payload.Start.Channel,
			}
			w.handleTimeSelectionResponse(ctx, startMsg, resp)
		}

		if payload.Kind == jobTypeMessage {
			// Time selection responses take priority over LLM reply — send only the slots/fallback message
			if resp != nil && resp.TimeSelectionResponse != nil && resp.TimeSelectionResponse.SMSMessage != "" {
				w.handleTimeSelectionResponse(ctx, payload.Message, resp)
			} else if resp != nil && resp.BookingRequest != nil && (w.browserBooking != nil || w.deposits != nil) {
				// Booking response: skip LLM reply, go directly to Moxie booking/Stripe checkout
				w.handleMoxieBooking(ctx, payload.Message, resp.BookingRequest)
			} else {
				// Normal path: send LLM reply, then check deposit intent
				blocked := w.sendReply(ctx, payload, resp)
				if !blocked {
					w.handleDepositIntent(ctx, payload.Message, resp)
				}
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

	// Output guard: scan reply for sensitive data leaks before sending.
	leakResult := ScanOutputForLeaks(resp.Message)
	if leakResult.Leaked {
		for _, reason := range leakResult.Reasons {
			w.events.OutputGuardTriggered(ctx, resp.ConversationID, msg.OrgID, reason)
		}
		w.logger.Warn("output guard: sensitive data leak detected",
			"conversation_id", resp.ConversationID,
			"org_id", msg.OrgID,
			"reasons", leakResult.Reasons,
		)
		if leakResult.Sanitized == "" {
			// Can't salvage — use generic fallback
			resp = &Response{
				ConversationID: resp.ConversationID,
				Message:        defaultSupervisorFallback,
				Timestamp:      time.Now().UTC(),
			}
		} else {
			resp = &Response{
				ConversationID: resp.ConversationID,
				Message:        leakResult.Sanitized,
				Timestamp:      resp.Timestamp,
			}
		}
	}

	conversationID := strings.TrimSpace(resp.ConversationID)
	if conversationID == "" {
		conversationID = strings.TrimSpace(msg.ConversationID)
	}

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

	var sendErr error
	if w.messenger != nil {
		sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		if err := w.messenger.SendReply(sendCtx, reply); err != nil {
			sendErr = err
			w.logger.Error("failed to send outbound reply", "error", err, "job_id", payload.ID, "org_id", msg.OrgID)
		}
	}

	providerMessageID := ""
	providerStatus := ""
	if reply.Metadata != nil {
		providerMessageID = strings.TrimSpace(reply.Metadata["provider_message_id"])
		providerStatus = strings.TrimSpace(reply.Metadata["provider_status"])
	}
	if providerStatus == "" && w.messenger != nil {
		if sendErr != nil {
			providerStatus = "failed"
		} else {
			providerStatus = "sent"
		}
	}
	errorReason := ""
	if sendErr != nil {
		errorReason = sendErr.Error()
	}

	w.appendTranscript(context.Background(), conversationID, SMSTranscriptMessage{
		Role:              "assistant",
		From:              msg.To,
		To:                msg.From,
		Body:              resp.Message,
		Timestamp:         resp.Timestamp,
		Kind:              "ai_reply",
		ProviderMessageID: providerMessageID,
		Status:            providerStatus,
		ErrorReason:       errorReason,
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

func (w *Worker) handleTimeSelectionResponse(ctx context.Context, msg MessageRequest, resp *Response) {
	if resp == nil || resp.TimeSelectionResponse == nil {
		return
	}
	if w.isOptedOut(ctx, msg.OrgID, msg.From) {
		return
	}

	tsr := resp.TimeSelectionResponse

	// Send the time selection SMS
	if tsr.SMSMessage != "" && w.messenger != nil {
		reply := OutboundReply{
			OrgID:          msg.OrgID,
			LeadID:         msg.LeadID,
			ConversationID: msg.ConversationID,
			To:             msg.From, // Send to the customer
			From:           msg.To,   // From the clinic number
			Body:           tsr.SMSMessage,
		}
		if err := w.messenger.SendReply(ctx, reply); err != nil {
			w.logger.Error("failed to send time selection SMS", "error", err, "org_id", msg.OrgID, "lead_id", msg.LeadID)
			return
		}

		// Record to transcript + database
		timeSelMsg := SMSTranscriptMessage{
			Role:      "assistant",
			Body:      tsr.SMSMessage,
			From:      msg.To,
			To:        msg.From,
			Timestamp: time.Now(),
		}
		if w.transcript != nil {
			_ = w.transcript.Append(ctx, msg.ConversationID, timeSelMsg)
		}
		if w.convStore != nil {
			_ = w.convStore.AppendMessage(ctx, msg.ConversationID, timeSelMsg)
		}

		// Save time options to LLM conversation history if not already saved by the LLM service.
		// Without this, the LLM won't know what times were presented when the
		// patient replies with a slot number (e.g. "6"), causing confusion.
		if !tsr.SavedToHistory && w.processor != nil {
			if histStore, ok := w.processor.(interface {
				AppendAssistantMessage(ctx context.Context, conversationID, message string) error
			}); ok {
				if err := histStore.AppendAssistantMessage(ctx, msg.ConversationID, tsr.SMSMessage); err != nil {
					w.logger.Warn("failed to save time selection to LLM history", "error", err)
				}
			}
		}
	}

	// Update conversation status to awaiting_time_selection
	if w.convStore != nil {
		if err := w.convStore.UpdateStatus(ctx, msg.ConversationID, StatusAwaitingTimeSelection); err != nil {
			w.logger.Warn("failed to update conversation status to awaiting_time_selection", "error", err, "conversation_id", msg.ConversationID)
		}
	}

	w.logger.Info("time selection SMS sent",
		"conversation_id", msg.ConversationID,
		"slots_presented", len(tsr.Slots),
		"service", tsr.Service,
		"exact_match", tsr.ExactMatch,
	)
}

func (w *Worker) handleMoxieBooking(ctx context.Context, msg MessageRequest, req *BookingRequest) {
	if req == nil {
		return
	}

	// Check if clinic uses Stripe for payments — if so, send Stripe Checkout link
	// instead of Moxie sidecar URL. After payment, handlePaymentEvent will call
	// createMoxieBookingAfterPayment to book via Moxie API.
	if w.deposits != nil && w.clinicStore != nil {
		cfg, err := w.clinicStore.Get(ctx, req.OrgID)
		if err == nil && cfg != nil && cfg.UsesStripePayment() {
			w.logger.Info("moxie booking: routing to Stripe Checkout (payment_provider=stripe)",
				"org_id", req.OrgID, "lead_id", req.LeadID, "service", req.Service)
			// Parse booking date/time into a time.Time for the deposit intent
			var scheduledFor *time.Time
			if req.Date != "" && req.Time != "" {
				loc, _ := time.LoadLocation(cfg.Timezone)
				if loc == nil {
					loc = time.UTC
				}
				if parsed, perr := time.ParseInLocation("2006-01-02 3:04pm", req.Date+" "+strings.ToLower(req.Time), loc); perr == nil {
					scheduledFor = &parsed
				}
			}
			desc := req.Service
			if scheduledFor != nil {
				desc = fmt.Sprintf("%s - %s", req.Service, scheduledFor.Format("Mon Jan 2 at 3:04 PM"))
			}
			resp := &Response{
				DepositIntent: &DepositIntent{
					AmountCents:     int32(cfg.DepositAmountForService(req.Service)),
					Description:     desc,
					ScheduledFor:    scheduledFor,
					BookingPolicies: cfg.BookingPolicies,
				},
			}
			if err := w.deposits.SendDeposit(ctx, msg, resp); err != nil {
				w.logger.Error("failed to send Stripe checkout for Moxie booking",
					"error", err, "org_id", req.OrgID, "lead_id", req.LeadID)
			}
			return
		}
	}

	// Fallback: use browser sidecar for Moxie checkout URL handoff
	if w.browserBooking == nil {
		w.logger.Warn("booking request received but no booking client configured",
			"org_id", req.OrgID, "lead_id", req.LeadID)
		return
	}
	w.handleMoxieBookingSidecar(ctx, msg, req)
}

// handleMoxieBookingDirect creates a Moxie appointment via their GraphQL API.
// No browser needed — instant booking with confirmation SMS.
func (w *Worker) handleMoxieBookingDirect(ctx context.Context, msg MessageRequest, req *BookingRequest, cfg *clinic.Config) {
	mc := cfg.MoxieConfig
	w.logger.Info("creating Moxie appointment via direct API",
		"org_id", req.OrgID, "lead_id", req.LeadID,
		"medspa_id", mc.MedspaID, "service", req.Service)

	// Resolve serviceMenuItemId from service name
	serviceMenuItemID := ""
	normalizedService := strings.ToLower(req.Service)
	if mc.ServiceMenuItems != nil {
		serviceMenuItemID = mc.ServiceMenuItems[normalizedService]
		// Try alias resolution
		if serviceMenuItemID == "" {
			resolved := cfg.ResolveServiceName(normalizedService)
			serviceMenuItemID = mc.ServiceMenuItems[strings.ToLower(resolved)]
		}
	}
	if serviceMenuItemID == "" {
		w.logger.Error("no Moxie serviceMenuItemId for service",
			"service", req.Service, "org_id", req.OrgID)
		w.sendBookingFallbackSMS(ctx, msg, "We couldn't find that service in our booking system. Please call the clinic directly to book your appointment.")
		return
	}

	// Parse the selected time slot to get start/end times in UTC
	// req.Date is YYYY-MM-DD, req.Time is e.g. "7:15 PM"
	startTime, endTime, err := w.parseMoxieTimeSlot(req.Date, req.Time, cfg.Timezone)
	if err != nil {
		w.logger.Error("failed to parse time slot for Moxie booking",
			"error", err, "date", req.Date, "time", req.Time)
		w.sendBookingFallbackSMS(ctx, msg, "We had trouble with the appointment time. Please try again or call the clinic directly.")
		return
	}

	// Determine provider ID
	providerID := mc.DefaultProviderID
	if providerID == "" {
		providerID = "no-preference"
	}

	// Create the appointment
	result, err := w.moxieClient.CreateAppointment(ctx, moxieclient.CreateAppointmentRequest{
		MedspaID:  mc.MedspaID,
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Email:     req.Email,
		Phone:     req.Phone,
		Note:      "",
		Services: []moxieclient.ServiceInput{{
			ServiceMenuItemID: serviceMenuItemID,
			ProviderID:        providerID,
			StartTime:         startTime,
			EndTime:           endTime,
		}},
		IsNewClient:              true, // Assume new client for SMS leads
		NoPreferenceProviderUsed: providerID == "no-preference",
	})
	if err != nil {
		w.logger.Error("Moxie API create appointment failed", "error", err,
			"org_id", req.OrgID, "lead_id", req.LeadID)
		w.sendBookingFallbackSMS(ctx, msg, "We're having trouble booking your appointment right now. Please try again in a moment or call the clinic directly.")
		return
	}

	if !result.OK {
		w.logger.Error("Moxie appointment creation returned not OK",
			"message", result.Message, "org_id", req.OrgID, "lead_id", req.LeadID)
		w.sendBookingFallbackSMS(ctx, msg, "We're having trouble booking your appointment right now. Please try again in a moment or call the clinic directly.")
		return
	}

	w.logger.Info("Moxie appointment created successfully via API",
		"appointment_id", result.AppointmentID,
		"org_id", req.OrgID, "lead_id", req.LeadID,
		"service", req.Service, "date", req.Date, "time", req.Time)

	// Update conversation status to booked
	if w.convStore != nil {
		if err := w.convStore.UpdateStatus(ctx, msg.ConversationID, StatusBooked); err != nil {
			w.logger.Warn("failed to update conversation status to booked", "error", err, "conversation_id", msg.ConversationID)
		}
	}

	// Send confirmation SMS
	confirmMsg := fmt.Sprintf("Your appointment has been booked! 🎉\n\n📋 %s\n📅 %s at %s\n📍 %s\n\nYou'll receive a confirmation from the clinic shortly. See you then!",
		req.Service, req.Date, req.Time, cfg.Name)
	if w.messenger != nil {
		reply := OutboundReply{
			OrgID:          msg.OrgID,
			LeadID:         msg.LeadID,
			ConversationID: msg.ConversationID,
			To:             msg.From,
			From:           msg.To,
			Body:           confirmMsg,
		}
		if err := w.messenger.SendReply(ctx, reply); err != nil {
			w.logger.Error("failed to send booking confirmation SMS", "error", err,
				"org_id", req.OrgID, "appointment_id", result.AppointmentID)
		}
	}

	// Update lead with appointment ID
	if w.leadsRepo != nil && req.LeadID != "" {
		now := time.Now()
		if err := w.leadsRepo.UpdateBookingSession(ctx, req.LeadID, leads.BookingSessionUpdate{
			SessionID:     result.AppointmentID,
			Platform:      "moxie",
			HandoffSentAt: &now,
		}); err != nil {
			w.logger.Warn("failed to update lead with appointment ID", "error", err,
				"lead_id", req.LeadID, "appointment_id", result.AppointmentID)
		}
	}

	// Record to transcript + DB
	w.appendTranscript(ctx, msg.ConversationID, SMSTranscriptMessage{
		Role:      "assistant",
		Body:      confirmMsg,
		Timestamp: time.Now(),
	})
	if w.convStore != nil {
		_ = w.convStore.AppendMessage(ctx, msg.ConversationID, SMSTranscriptMessage{
			Role:      "assistant",
			Body:      confirmMsg,
			Timestamp: time.Now(),
		})
	}
}

// parseMoxieTimeSlot converts date + time string to UTC ISO 8601 start/end times.
func (w *Worker) parseMoxieTimeSlot(date, timeStr, timezone string) (string, string, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}

	// Parse "7:15 PM" or "7:15pm" style
	timeStr = strings.TrimSpace(strings.ToUpper(timeStr))
	timeStr = strings.Replace(timeStr, ".", "", -1) // remove dots from "P.M."

	// Try common formats
	var t time.Time
	for _, fmt := range []string{"3:04 PM", "3:04PM", "15:04"} {
		t, err = time.Parse(fmt, timeStr)
		if err == nil {
			break
		}
	}
	if err != nil {
		return "", "", fmt.Errorf("parse time %q: %w", timeStr, err)
	}

	// Parse date
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return "", "", fmt.Errorf("parse date %q: %w", date, err)
	}

	// Combine date + time in clinic timezone
	start := time.Date(d.Year(), d.Month(), d.Day(), t.Hour(), t.Minute(), 0, 0, loc)
	end := start.Add(45 * time.Minute) // Default 45 min appointment

	return start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339), nil
}

// handleMoxieBookingSidecar is the legacy browser-based booking flow.
func (w *Worker) handleMoxieBookingSidecar(ctx context.Context, msg MessageRequest, req *BookingRequest) {
	if w.browserBooking == nil {
		w.logger.Warn("booking request received but no browser booking client configured",
			"org_id", req.OrgID, "lead_id", req.LeadID)
		return
	}

	// Step 1: Start the booking session on the sidecar
	startReq := browser.BookingStartRequest{
		BookingURL: req.BookingURL,
		Date:       req.Date,
		Time:       req.Time,
		Lead: browser.BookingLeadInfo{
			FirstName: req.FirstName,
			LastName:  req.LastName,
			Phone:     req.Phone,
			Email:     req.Email,
		},
		Service:     req.Service,
		Provider:    req.Provider,
		CallbackURL: req.CallbackURL,
	}

	startResp, err := w.browserBooking.StartBookingSession(ctx, startReq)
	if err != nil {
		w.logger.Error("failed to start Moxie booking session", "error", err,
			"org_id", req.OrgID, "lead_id", req.LeadID, "booking_url", req.BookingURL)
		w.sendBookingFallbackSMS(ctx, msg, "We're having trouble starting your booking right now. Please try again in a moment or call the clinic directly.")
		return
	}
	if !startResp.Success {
		w.logger.Error("Moxie booking session start failed", "error", startResp.Error,
			"org_id", req.OrgID, "lead_id", req.LeadID)
		w.sendBookingFallbackSMS(ctx, msg, "We're having trouble starting your booking right now. Please try again in a moment or call the clinic directly.")
		return
	}

	sessionID := startResp.SessionID
	w.logger.Info("Moxie booking session started", "session_id", sessionID,
		"org_id", req.OrgID, "lead_id", req.LeadID)

	// Step 2: Update lead with session ID
	if w.leadsRepo != nil && req.LeadID != "" {
		if err := w.leadsRepo.UpdateBookingSession(ctx, req.LeadID, leads.BookingSessionUpdate{
			SessionID: sessionID,
			Platform:  "moxie",
		}); err != nil {
			w.logger.Warn("failed to update lead with booking session", "error", err,
				"lead_id", req.LeadID, "session_id", sessionID)
		}
	}

	// Step 3: Poll for handoff URL (every 2s, up to 90s)
	var handoffURL string
	pollCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-pollCtx.Done():
			w.logger.Warn("Moxie booking handoff URL timed out", "session_id", sessionID,
				"org_id", req.OrgID, "lead_id", req.LeadID)
			_ = w.browserBooking.CancelBookingSession(ctx, sessionID)
			w.sendBookingFallbackSMS(ctx, msg, "We're having trouble completing your booking right now. Please try again in a moment or call the clinic directly.")
			return
		case <-ticker.C:
			resp, err := w.browserBooking.GetHandoffURL(pollCtx, sessionID)
			if err != nil {
				w.logger.Debug("handoff URL poll error", "error", err, "session_id", sessionID)
				continue
			}
			if resp.Success && resp.HandoffURL != "" {
				handoffURL = resp.HandoffURL
				goto gotHandoff
			}
		}
	}

gotHandoff:
	w.logger.Info("Moxie booking handoff URL received", "session_id", sessionID,
		"handoff_url", handoffURL[:min(50, len(handoffURL))], "org_id", req.OrgID)

	handoffMsg := fmt.Sprintf("Your booking is almost complete! Tap the link below to enter your payment info and finalize your appointment:\n%s", handoffURL)
	if w.messenger != nil {
		reply := OutboundReply{
			OrgID:          msg.OrgID,
			LeadID:         msg.LeadID,
			ConversationID: msg.ConversationID,
			To:             msg.From,
			From:           msg.To,
			Body:           handoffMsg,
		}
		if err := w.messenger.SendReply(ctx, reply); err != nil {
			w.logger.Error("failed to send booking handoff SMS", "error", err,
				"session_id", sessionID, "org_id", req.OrgID)
		}
	}

	// Update lead with handoff URL
	if w.leadsRepo != nil && req.LeadID != "" {
		now := time.Now()
		if err := w.leadsRepo.UpdateBookingSession(ctx, req.LeadID, leads.BookingSessionUpdate{
			HandoffURL:    handoffURL,
			HandoffSentAt: &now,
		}); err != nil {
			w.logger.Warn("failed to update lead with handoff URL", "error", err,
				"lead_id", req.LeadID, "session_id", sessionID)
		}
	}

	// Record to transcript
	w.appendTranscript(ctx, msg.ConversationID, SMSTranscriptMessage{
		Role:      "assistant",
		From:      msg.To,
		To:        msg.From,
		Body:      handoffMsg,
		Timestamp: time.Now(),
		Kind:      "booking_handoff",
	})
}

func (w *Worker) sendBookingFallbackSMS(ctx context.Context, msg MessageRequest, body string) {
	if w.messenger == nil {
		return
	}
	reply := OutboundReply{
		OrgID:          msg.OrgID,
		LeadID:         msg.LeadID,
		ConversationID: msg.ConversationID,
		To:             msg.From,
		From:           msg.To,
		Body:           body,
	}
	if err := w.messenger.SendReply(ctx, reply); err != nil {
		w.logger.Error("failed to send booking fallback SMS", "error", err, "org_id", msg.OrgID)
	}
	w.appendTranscript(ctx, msg.ConversationID, SMSTranscriptMessage{
		Role:      "assistant",
		From:      msg.To,
		To:        msg.From,
		Body:      body,
		Timestamp: time.Now(),
		Kind:      "booking_fallback",
	})
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

	// For Moxie+Stripe clinics: create the actual appointment on Moxie now that
	// the deposit has been collected. This is the critical "Step 4b" — without it
	// the patient pays but never gets booked.
	cfg := w.clinicConfig(ctx, evt.OrgID)
	moxieBooked := false
	var moxieConfirmMsg string
	if cfg != nil && cfg.UsesStripePayment() && cfg.UsesMoxieBooking() && w.moxieClient != nil && cfg.MoxieConfig != nil {
		moxieBooked, moxieConfirmMsg = w.createMoxieBookingAfterPayment(ctx, evt, cfg)
	}

	if evt.LeadPhone != "" && evt.FromNumber != "" {
		if !w.isOptedOut(ctx, evt.OrgID, evt.LeadPhone) {
			var body string
			if moxieBooked && moxieConfirmMsg != "" {
				body = moxieConfirmMsg
			} else {
				var clinicName, bookingURL, callbackTime, tz string
				if cfg != nil {
					clinicName = strings.TrimSpace(cfg.Name)
					bookingURL = strings.TrimSpace(cfg.BookingURL)
					callbackTime = cfg.ExpectedCallbackTime(time.Now())
					tz = cfg.Timezone
				}
				if callbackTime == "" {
					callbackTime = "within 24 hours" // fallback
				}
				// Convert scheduled time to clinic timezone for display
				if evt.ScheduledFor != nil && tz != "" {
					if loc, lerr := time.LoadLocation(tz); lerr == nil {
						localTime := evt.ScheduledFor.In(loc)
						evt.ScheduledFor = &localTime
					}
				}
				body = paymentConfirmationMessage(evt, clinicName, bookingURL, callbackTime)
			}

			if w.messenger == nil {
				// Transcript is still recorded even when SMS sending is disabled.
			} else {
				reply := OutboundReply{
					OrgID:          evt.OrgID,
					LeadID:         evt.LeadID,
					ConversationID: smsConversationID(evt.OrgID, evt.LeadPhone),
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

	// Update conversation status to deposit_paid
	if w.convStore != nil && evt.LeadPhone != "" {
		if err := w.convStore.UpdateStatusByPhone(ctx, evt.OrgID, evt.LeadPhone, "deposit_paid"); err != nil {
			w.logger.Warn("failed to update conversation status to deposit_paid", "error", err, "org_id", evt.OrgID, "lead_phone", evt.LeadPhone)
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

// createMoxieBookingAfterPayment creates a Moxie appointment after Stripe deposit is collected.
// Returns (booked, confirmationMessage). If booking fails, we still proceed with the
// generic payment confirmation — the clinic can manually book the patient.
func (w *Worker) createMoxieBookingAfterPayment(ctx context.Context, evt *events.PaymentSucceededV1, cfg *clinic.Config) (bool, string) {
	mc := cfg.MoxieConfig
	if mc == nil || mc.MedspaID == "" {
		w.logger.Warn("moxie booking after payment skipped: no moxie config", "org_id", evt.OrgID)
		return false, ""
	}

	// Fetch lead to get selected appointment details
	if w.leadsRepo == nil {
		w.logger.Warn("moxie booking after payment skipped: no leads repo", "org_id", evt.OrgID)
		return false, ""
	}
	lead, err := w.leadsRepo.GetByID(ctx, evt.OrgID, evt.LeadID)
	if err != nil {
		w.logger.Error("moxie booking after payment: lead fetch failed", "error", err,
			"org_id", evt.OrgID, "lead_id", evt.LeadID)
		return false, ""
	}

	// The lead must have a selected appointment (date/time + service)
	if lead.SelectedDateTime == nil {
		// Fall back to evt.ScheduledFor if available
		if evt.ScheduledFor == nil {
			w.logger.Warn("moxie booking after payment skipped: no selected appointment time",
				"org_id", evt.OrgID, "lead_id", evt.LeadID)
			return false, ""
		}
		lead.SelectedDateTime = evt.ScheduledFor
	}

	service := lead.SelectedService
	if service == "" {
		service = lead.ServiceInterest
	}
	if service == "" {
		w.logger.Warn("moxie booking after payment skipped: no service selected",
			"org_id", evt.OrgID, "lead_id", evt.LeadID)
		return false, ""
	}

	// Resolve serviceMenuItemId
	normalizedService := strings.ToLower(service)
	serviceMenuItemID := ""
	if mc.ServiceMenuItems != nil {
		serviceMenuItemID = mc.ServiceMenuItems[normalizedService]
		if serviceMenuItemID == "" {
			resolved := cfg.ResolveServiceName(normalizedService)
			serviceMenuItemID = mc.ServiceMenuItems[strings.ToLower(resolved)]
		}
	}
	if serviceMenuItemID == "" {
		w.logger.Error("moxie booking after payment: no serviceMenuItemId for service",
			"service", service, "org_id", evt.OrgID, "lead_id", evt.LeadID)
		return false, ""
	}

	// Parse start/end times from the selected datetime
	loc, locErr := time.LoadLocation(cfg.Timezone)
	if locErr != nil {
		loc = time.UTC
	}
	localTime := lead.SelectedDateTime.In(loc)
	startTime := lead.SelectedDateTime.UTC().Format(time.RFC3339)
	endTime := lead.SelectedDateTime.Add(45 * time.Minute).UTC().Format(time.RFC3339)

	providerID := mc.DefaultProviderID
	if providerID == "" {
		providerID = "no-preference"
	}

	// Split name into first/last
	firstName, lastName := splitName(lead.Name)

	w.logger.Info("creating Moxie appointment after Stripe payment",
		"org_id", evt.OrgID, "lead_id", evt.LeadID,
		"medspa_id", mc.MedspaID, "service", service,
		"start_time", startTime)

	result, err := w.moxieClient.CreateAppointment(ctx, moxieclient.CreateAppointmentRequest{
		MedspaID:  mc.MedspaID,
		FirstName: firstName,
		LastName:  lastName,
		Email:     lead.Email,
		Phone:     lead.Phone,
		Note:      fmt.Sprintf("Deposit collected via Stripe (ref: %s)", evt.ProviderRef),
		Services: []moxieclient.ServiceInput{{
			ServiceMenuItemID: serviceMenuItemID,
			ProviderID:        providerID,
			StartTime:         startTime,
			EndTime:           endTime,
		}},
		IsNewClient:              lead.PatientType != "existing",
		NoPreferenceProviderUsed: providerID == "no-preference",
	})
	if err != nil {
		w.logger.Error("Moxie API create appointment after payment failed", "error", err,
			"org_id", evt.OrgID, "lead_id", evt.LeadID)
		return false, ""
	}
	if !result.OK {
		w.logger.Error("Moxie appointment creation after payment returned not OK",
			"message", result.Message, "org_id", evt.OrgID, "lead_id", evt.LeadID)
		return false, ""
	}

	w.logger.Info("Moxie appointment created successfully after Stripe payment",
		"appointment_id", result.AppointmentID,
		"org_id", evt.OrgID, "lead_id", evt.LeadID,
		"service", service)

	// Update conversation status to booked
	if w.convStore != nil {
		if err := w.convStore.UpdateStatusByPhone(ctx, evt.OrgID, evt.LeadPhone, StatusBooked); err != nil {
			w.logger.Warn("failed to update conversation status to booked", "error", err, "org_id", evt.OrgID, "lead_phone", evt.LeadPhone)
		}
	}

	// Update lead with booking session info
	now := time.Now()
	if err := w.leadsRepo.UpdateBookingSession(ctx, evt.LeadID, leads.BookingSessionUpdate{
		SessionID:   result.AppointmentID,
		Platform:    "moxie",
		Outcome:     "success",
		CompletedAt: &now,
	}); err != nil {
		w.logger.Warn("failed to update lead with Moxie appointment after payment",
			"error", err, "lead_id", evt.LeadID, "appointment_id", result.AppointmentID)
	}

	// Build Moxie-specific confirmation message with appointment details
	dateStr := localTime.Format("Monday, January 2")
	tzAbbrev := localTime.Format("MST")
	timeStr := localTime.Format("3:04 PM") + " " + tzAbbrev
	confirmMsg := fmt.Sprintf(
		"Payment received and your appointment is booked! 🎉\n\n"+
			"📋 %s\n"+
			"📅 %s at %s\n"+
			"📍 %s\n\n"+
			"Reminder: There is a 24-hour cancellation policy. Cancellations made less than 24 hours before your appointment are non-refundable.\n\n"+
			"See you then!",
		service, dateStr, timeStr, cfg.Name)

	return true, confirmMsg
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

	cancellationPolicy := "\n\nReminder: There is a 24-hour cancellation policy. Cancellations made less than 24 hours before your appointment are non-refundable."

	if evt.ScheduledFor != nil {
		tzAbbrev := evt.ScheduledFor.Format("MST")
		date := evt.ScheduledFor.Format("Monday, January 2 at 3:04 PM") + " " + tzAbbrev
		service := evt.ServiceName
		if service == "" {
			service = "your appointment"
		}
		if name != "" {
			return fmt.Sprintf("Payment received! Your %s appointment at %s on %s is confirmed.%s", service, name, date, cancellationPolicy)
		}
		return fmt.Sprintf("Payment received! Your %s appointment on %s is confirmed.%s", service, date, cancellationPolicy)
	}
	amount := float64(evt.AmountCents) / 100
	if name != "" {
		return fmt.Sprintf("Payment of $%.2f received - thank you! A %s team member will call you %s to confirm your appointment.%s", amount, name, callbackTime, cancellationPolicy)
	}
	return fmt.Sprintf("Payment of $%.2f received - thank you! Our team will call you %s to confirm your appointment.%s", amount, callbackTime, cancellationPolicy)
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
				ConversationID: smsConversationID(evt.OrgID, evt.LeadPhone),
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
