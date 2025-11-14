package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds application configuration
type Config struct {
	Port                     string
	Env                      string
	LogLevel                 string
	UseMemoryQueue           bool
	WorkerCount              int
	DatabaseURL              string
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
	SquareWebhookKey         string
	SquareSuccessURL         string
	SquareCancelURL          string
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
	OpenAIAPIKey             string
	OpenAIModel              string
	OpenAIBaseURL            string
	OpenAIEmbeddingModel     string
	RedisAddr                string
	RedisPassword            string
}

// Load reads configuration from environment variables
func Load() *Config {
	return &Config{
		Port:                     getEnv("PORT", "8080"),
		Env:                      getEnv("ENV", "development"),
		LogLevel:                 getEnv("LOG_LEVEL", "info"),
		UseMemoryQueue:           getEnvAsBool("USE_MEMORY_QUEUE", false),
		WorkerCount:              getEnvAsInt("WORKER_COUNT", 2),
		DatabaseURL:              getEnv("DATABASE_URL", ""),
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
		SquareWebhookKey:         getEnv("SQUARE_WEBHOOK_SIGNATURE_KEY", ""),
		SquareSuccessURL:         getEnv("SQUARE_SUCCESS_URL", ""),
		SquareCancelURL:          getEnv("SQUARE_CANCEL_URL", ""),
		DepositAmountCents:       getEnvAsInt("DEPOSIT_AMOUNT_CENTS", 5000),
		AdminJWTSecret:           getEnv("ADMIN_JWT_SECRET", ""),
		QuietHoursStart:          getEnv("QUIET_HOURS_START", ""),
		QuietHoursEnd:            getEnv("QUIET_HOURS_END", ""),
		QuietHoursTimezone:       getEnv("QUIET_HOURS_TZ", "UTC"),
		AWSRegion:                getEnv("AWS_REGION", "us-east-1"),
		AWSAccessKeyID:           getEnv("AWS_ACCESS_KEY_ID", "localstack"),
		AWSSecretAccessKey:       getEnv("AWS_SECRET_ACCESS_KEY", "localstack"),
		AWSEndpointOverride:      getEnv("AWS_ENDPOINT_OVERRIDE", "http://localhost:4566"),
		ConversationQueueURL:     getEnv("CONVERSATION_QUEUE_URL", "http://localhost:4566/000000000000/conversation-events"),
		ConversationJobsTable:    getEnv("CONVERSATION_JOBS_TABLE", "conversation_jobs"),
		OpenAIAPIKey:             getEnv("OPENAI_API_KEY", ""),
		OpenAIModel:              getEnv("OPENAI_MODEL", "gpt-4o-mini"),
		OpenAIBaseURL:            getEnv("OPENAI_BASE_URL", ""),
		OpenAIEmbeddingModel:     getEnv("OPENAI_EMBEDDING_MODEL", "text-embedding-3-small"),
		RedisAddr:                getEnv("REDIS_ADDR", "redis:6379"),
		RedisPassword:            getEnv("REDIS_PASSWORD", ""),
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