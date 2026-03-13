package conversation

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/wolfman30/medspa-ai-platform/internal/booking"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	moxieclient "github.com/wolfman30/medspa-ai-platform/internal/emr/moxie"
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
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
	moxieClient      *moxieclient.Client
	leadsRepo        leads.Repository
	manualHandoff    *booking.ManualHandoffAdapter
	voiceCaller      VoiceCallInitiator
	igMessenger      ReplyMessenger
	webChatMessenger ReplyMessenger
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
	moxieClient      *moxieclient.Client
	leadsRepo        leads.Repository
	manualHandoff    *booking.ManualHandoffAdapter
	voiceCaller      VoiceCallInitiator
	igMessenger      ReplyMessenger
	webChatMessenger ReplyMessenger
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

// WithWorkerMoxieClient wires a direct Moxie GraphQL API client for booking creation.
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

// WithInstagramMessenger sets the Instagram DM reply messenger.
func WithInstagramMessenger(m ReplyMessenger) WorkerOption {
	return func(cfg *workerConfig) {
		cfg.igMessenger = m
	}
}

// WithWebChatMessenger sets the web chat reply messenger.
func WithWebChatMessenger(m ReplyMessenger) WorkerOption {
	return func(cfg *workerConfig) {
		cfg.webChatMessenger = m
	}
}

// WithVoiceCaller wires a Telnyx voice client for initiating outbound AI callbacks.
func WithVoiceCaller(caller VoiceCallInitiator) WorkerOption {
	return func(cfg *workerConfig) {
		cfg.voiceCaller = caller
	}
}

// bookingConfirmer confirms a booking for a lead after payment.
type bookingConfirmer interface {
	ConfirmBooking(ctx context.Context, orgID uuid.UUID, leadID uuid.UUID, scheduledFor *time.Time) error
}

// DepositSender sends deposit/checkout links to patients after qualifying.
type DepositSender interface {
	SendDeposit(ctx context.Context, msg MessageRequest, resp *Response) error
}

// NewWorker constructs a queue consumer around the provided processor.
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
		moxieClient:      cfg.moxieClient,
		leadsRepo:        cfg.leadsRepo,
		manualHandoff:    cfg.manualHandoff,
		voiceCaller:      cfg.voiceCaller,
		igMessenger:      cfg.igMessenger,
		webChatMessenger: cfg.webChatMessenger,
		logger:           logger,
		events:           NewEventLogger(logger),
		cfg:              cfg,
	}
}
