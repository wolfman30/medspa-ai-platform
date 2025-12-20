package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds application configuration
type Config struct {
	Port                     string
	Env                      string
	PublicBaseURL            string
	LogLevel                 string
	UseMemoryQueue           bool
	WorkerCount              int
	DatabaseURL              string
	SMSProvider              string
	TelnyxAPIKey             string
	TelnyxMessagingProfileID string
	TelnyxWebhookSecret      string
	TelnyxStopReply          string
	TelnyxHelpReply          string
	TelnyxRetryMaxAttempts   int
	TelnyxRetryBaseDelay     time.Duration
	TelnyxHostedPollInterval time.Duration
	TwilioAccountSID         string
	TwilioAuthToken          string
	TwilioWebhookSecret      string
	TwilioFromNumber         string
	TwilioOrgMapJSON         string
	PaymentProviderKey       string
	SquareAccessToken        string
	SquareLocationID         string
	SquareBaseURL            string
	SquareWebhookKey         string
	SquareSuccessURL         string
	SquareCancelURL          string
	SquareClientID           string
	SquareClientSecret       string
	SquareOAuthRedirectURI   string
	SquareOAuthSuccessURL    string
	SquareSandbox            bool
	AllowFakePayments        bool
	DepositAmountCents       int
	AdminJWTSecret           string
	QuietHoursStart          string
	QuietHoursEnd            string
	QuietHoursTimezone       string
	AWSRegion                string
	AWSAccessKeyID           string
	AWSSecretAccessKey       string
	AWSEndpointOverride      string
	ConversationQueueURL     string
	ConversationJobsTable    string
	BedrockModelID           string
	BedrockEmbeddingModelID  string
	RedisAddr                string
	RedisPassword            string
	RedisTLS                 bool

	// SendGrid Email Configuration
	SendGridAPIKey    string
	SendGridFromEmail string
	SendGridFromName  string

	// Nextech EMR Configuration
	NextechBaseURL      string
	NextechClientID     string
	NextechClientSecret string

	// Aesthetic Record (Shadow Scheduler) Configuration
	AestheticRecordClinicID          string
	AestheticRecordProviderID        string
	AestheticRecordSelectBaseURL     string
	AestheticRecordSelectBearerToken string
	AestheticRecordShadowSyncEnabled bool
	AestheticRecordSyncInterval      time.Duration
	AestheticRecordSyncWindowDays    int
	AestheticRecordSyncDurationMins  int
}

// Load reads configuration from environment variables
func Load() *Config {
	return &Config{
		Port:                     getEnv("PORT", "8080"),
		Env:                      getEnv("ENV", "development"),
		PublicBaseURL:            getEnv("PUBLIC_BASE_URL", ""),
		LogLevel:                 getEnv("LOG_LEVEL", "info"),
		UseMemoryQueue:           getEnvAsBool("USE_MEMORY_QUEUE", false),
		WorkerCount:              getEnvAsInt("WORKER_COUNT", 2),
		DatabaseURL:              getEnv("DATABASE_URL", ""),
		SMSProvider:              strings.ToLower(strings.TrimSpace(getEnv("SMS_PROVIDER", "auto"))),
		TelnyxAPIKey:             getEnv("TELNYX_API_KEY", ""),
		TelnyxMessagingProfileID: getEnv("TELNYX_MESSAGING_PROFILE_ID", ""),
		TelnyxWebhookSecret:      getEnv("TELNYX_WEBHOOK_SECRET", ""),
		TelnyxStopReply:          getEnv("TELNYX_STOP_REPLY", "You have been opted out. Reply HELP for info."),
		TelnyxHelpReply:          getEnv("TELNYX_HELP_REPLY", "Reply STOP to opt out or contact support@medspa.ai."),
		TelnyxRetryMaxAttempts:   getEnvAsInt("TELNYX_RETRY_MAX_ATTEMPTS", 5),
		TelnyxRetryBaseDelay:     getEnvAsDuration("TELNYX_RETRY_BASE_DELAY", 5*time.Minute),
		TelnyxHostedPollInterval: getEnvAsDuration("TELNYX_HOSTED_POLL_INTERVAL", 15*time.Minute),
		TwilioAccountSID:         getEnv("TWILIO_ACCOUNT_SID", ""),
		TwilioAuthToken:          getEnv("TWILIO_AUTH_TOKEN", ""),
		TwilioWebhookSecret:      getEnv("TWILIO_WEBHOOK_SECRET", ""),
		TwilioFromNumber:         getEnv("TWILIO_FROM_NUMBER", ""),
		TwilioOrgMapJSON:         getEnv("TWILIO_ORG_MAP_JSON", ""),
		PaymentProviderKey:       getEnv("PAYMENT_PROVIDER_KEY", ""),
		SquareAccessToken:        getEnv("SQUARE_ACCESS_TOKEN", ""),
		SquareLocationID:         getEnv("SQUARE_LOCATION_ID", ""),
		SquareBaseURL:            getEnv("SQUARE_BASE_URL", ""),
		SquareWebhookKey:         getEnv("SQUARE_WEBHOOK_SIGNATURE_KEY", ""),
		SquareSuccessURL:         getEnv("SQUARE_SUCCESS_URL", ""),
		SquareCancelURL:          getEnv("SQUARE_CANCEL_URL", ""),
		SquareClientID:           getEnv("SQUARE_CLIENT_ID", ""),
		SquareClientSecret:       getEnv("SQUARE_CLIENT_SECRET", ""),
		SquareOAuthRedirectURI:   getEnv("SQUARE_OAUTH_REDIRECT_URI", ""),
		SquareOAuthSuccessURL:    getEnv("SQUARE_OAUTH_SUCCESS_URL", ""),
		SquareSandbox:            getEnvAsBool("SQUARE_SANDBOX", true),
		AllowFakePayments:        getEnvAsBool("ALLOW_FAKE_PAYMENTS", false),
		DepositAmountCents:       getEnvAsInt("DEPOSIT_AMOUNT_CENTS", 5000),
		AdminJWTSecret:           getEnv("ADMIN_JWT_SECRET", ""),
		QuietHoursStart:          getEnv("QUIET_HOURS_START", ""),
		QuietHoursEnd:            getEnv("QUIET_HOURS_END", ""),
		QuietHoursTimezone:       getEnv("QUIET_HOURS_TZ", "UTC"),
		AWSRegion:                getEnv("AWS_REGION", "us-east-1"),
		AWSAccessKeyID:           getEnv("AWS_ACCESS_KEY_ID", ""),
		AWSSecretAccessKey:       getEnv("AWS_SECRET_ACCESS_KEY", ""),
		AWSEndpointOverride:      getEnv("AWS_ENDPOINT_OVERRIDE", ""),
		ConversationQueueURL:     getEnv("CONVERSATION_QUEUE_URL", ""),
		ConversationJobsTable:    getEnv("CONVERSATION_JOBS_TABLE", "conversation_jobs"),
		BedrockModelID:           getEnv("BEDROCK_MODEL_ID", ""),
		BedrockEmbeddingModelID:  getEnv("BEDROCK_EMBEDDING_MODEL_ID", ""),
		RedisAddr:                getEnv("REDIS_ADDR", "redis:6379"),
		RedisPassword:            getEnv("REDIS_PASSWORD", ""),
		RedisTLS:                 getEnvAsBool("REDIS_TLS", false),

		// SendGrid Email Configuration
		SendGridAPIKey:    getEnv("SENDGRID_API_KEY", ""),
		SendGridFromEmail: getEnv("SENDGRID_FROM_EMAIL", ""),
		SendGridFromName:  getEnv("SENDGRID_FROM_NAME", "MedSpa AI"),

		// Nextech EMR Configuration
		NextechBaseURL:      getEnv("NEXTECH_BASE_URL", ""),
		NextechClientID:     getEnv("NEXTECH_CLIENT_ID", ""),
		NextechClientSecret: getEnv("NEXTECH_CLIENT_SECRET", ""),

		// Aesthetic Record (Shadow Scheduler) Configuration
		AestheticRecordClinicID:          getEnv("AESTHETIC_RECORD_CLINIC_ID", ""),
		AestheticRecordProviderID:        getEnv("AESTHETIC_RECORD_PROVIDER_ID", ""),
		AestheticRecordSelectBaseURL:     getEnv("AESTHETIC_RECORD_SELECT_BASE_URL", ""),
		AestheticRecordSelectBearerToken: getEnv("AESTHETIC_RECORD_SELECT_BEARER_TOKEN", ""),
		AestheticRecordShadowSyncEnabled: getEnvAsBool("AESTHETIC_RECORD_SHADOW_SYNC_ENABLED", false),
		AestheticRecordSyncInterval:      getEnvAsDuration("AESTHETIC_RECORD_SYNC_INTERVAL", 30*time.Minute),
		AestheticRecordSyncWindowDays:    getEnvAsInt("AESTHETIC_RECORD_SYNC_WINDOW_DAYS", 7),
		AestheticRecordSyncDurationMins:  getEnvAsInt("AESTHETIC_RECORD_SYNC_DURATION_MINS", 30),
	}
}

// getEnv retrieves an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvAsInt retrieves an environment variable as an integer or returns a default value
func getEnvAsInt(key string, defaultValue int) int {
	valueStr := getEnv(key, "")
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}
	return defaultValue
}

// getEnvAsBool retrieves an environment variable as a boolean or returns a default value
func getEnvAsBool(key string, defaultValue bool) bool {
	valueStr := getEnv(key, "")
	if value, err := strconv.ParseBool(valueStr); err == nil {
		return value
	}
	return defaultValue
}

func getEnvAsDuration(key string, defaultValue time.Duration) time.Duration {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return defaultValue
	}
	if value, err := time.ParseDuration(valueStr); err == nil {
		return value
	}
	return defaultValue
}
