package config

import (
	"os"
	"strconv"
)

// Config holds application configuration
type Config struct {
	Port                  string
	Env                   string
	LogLevel              string
	DatabaseURL           string
	TwilioAccountSID      string
	TwilioAuthToken       string
	TwilioWebhookSecret   string
	PaymentProviderKey    string
	AWSRegion             string
	AWSAccessKeyID        string
	AWSSecretAccessKey    string
	AWSEndpointOverride   string
	ConversationQueueURL  string
	ConversationJobsTable string
}

// Load reads configuration from environment variables
func Load() *Config {
	return &Config{
		Port:                  getEnv("PORT", "8080"),
		Env:                   getEnv("ENV", "development"),
		LogLevel:              getEnv("LOG_LEVEL", "info"),
		DatabaseURL:           getEnv("DATABASE_URL", ""),
		TwilioAccountSID:      getEnv("TWILIO_ACCOUNT_SID", ""),
		TwilioAuthToken:       getEnv("TWILIO_AUTH_TOKEN", ""),
		TwilioWebhookSecret:   getEnv("TWILIO_WEBHOOK_SECRET", ""),
		PaymentProviderKey:    getEnv("PAYMENT_PROVIDER_KEY", ""),
		AWSRegion:             getEnv("AWS_REGION", "us-east-1"),
		AWSAccessKeyID:        getEnv("AWS_ACCESS_KEY_ID", "localstack"),
		AWSSecretAccessKey:    getEnv("AWS_SECRET_ACCESS_KEY", "localstack"),
		AWSEndpointOverride:   getEnv("AWS_ENDPOINT_OVERRIDE", "http://localhost:4566"),
		ConversationQueueURL:  getEnv("CONVERSATION_QUEUE_URL", "http://localhost:4566/000000000000/conversation-events"),
		ConversationJobsTable: getEnv("CONVERSATION_JOBS_TABLE", "conversation_jobs"),
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
