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

	"github.com/wolfman30/medspa-ai-platform/internal/booking"
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
	manualHandoff    *booking.ManualHandoffAdapter
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
	manualHandoff    *booking.ManualHandoffAdapter
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

// WithManualHandoff wires a manual handoff adapter for non-Moxie clinics.
func WithManualHandoff(adapter *booking.ManualHandoffAdapter) WorkerOption {
	return func(cfg *workerConfig) {
		cfg.manualHandoff = adapter
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
		manualHandoff:    cfg.manualHandoff,
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
