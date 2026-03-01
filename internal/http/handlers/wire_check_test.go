package handlers

import (
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/telnyxclient"
)

var _ messagingStore = (*messaging.Store)(nil)

var _ conversationPublisher = (*conversation.Publisher)(nil)
var _ processedTracker = (*events.ProcessedStore)(nil)
var _ conversationStore = (*conversation.ConversationStore)(nil)
var _ conversationStatusUpdater = (*conversation.ConversationStore)(nil)

var _ clinicByNumberLookup = (*messaging.Store)(nil)
var _ voiceConversationStore = (*conversation.ConversationStore)(nil)

var _ onboardingDB = (*pgxpool.Pool)(nil)
var _ adminClinicDataDB = (*pgxpool.Pool)(nil)

var _ telnyxClient = (*telnyxclient.Client)(nil)
var _ MissedCallTexter = (*TelnyxWebhookHandler)(nil)
var _ GitHubNotifier = (*TelegramNotifier)(nil)
var _ S3Uploader = (*s3.Client)(nil)
