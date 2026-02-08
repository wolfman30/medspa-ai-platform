package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds application configuration
type Config struct {
	Port                            string
	Env                             string
	PublicBaseURL                   string
	LogLevel                        string
	CORSAllowedOrigins              []string
	UseMemoryQueue                  bool
	WorkerCount                     int
	DatabaseURL                     string
	PersistConversationHistory      bool
	ConversationPersistExcludePhone string
	SMSProvider                     string
	TelnyxAPIKey                    string
	TelnyxMessagingProfileID        string
	TelnyxWebhookSecret             string
	TelnyxStopReply                 string
	TelnyxHelpReply                 string
	TelnyxStartReply                string
	TelnyxFirstContactReply         string
	TelnyxVoiceAckReply             string
	TelnyxFromNumber                string
	TelnyxTrackJobs                 bool
	TelnyxRetryMaxAttempts          int
	TelnyxRetryBaseDelay            time.Duration
	TelnyxHostedPollInterval        time.Duration
	TwilioAccountSID                string
	TwilioAuthToken                 string
	TwilioWebhookSecret             string
	TwilioFromNumber                string
	TwilioOrgMapJSON                string
	TwilioSkipSignature             bool
	PaymentProviderKey              string
	SquareAccessToken               string
	SquareLocationID                string
	SquareBaseURL                   string
	SquareWebhookKey                string
	SquareSuccessURL                string
	SquareCancelURL                 string
	SquareClientID                  string
	SquareClientSecret              string
	SquareOAuthRedirectURI          string
	SquareOAuthSuccessURL           string
	SquareSandbox                   bool
	SquareCheckoutMode              string
	SquareCheckoutAllowFallback     bool
	AllowFakePayments               bool
	DepositAmountCents              int
	SandboxAutoPurgePhones          string
	SandboxAutoPurgeDelay           time.Duration
	AdminJWTSecret                  string
	OnboardingToken                 string
	QuietHoursStart                 string
	QuietHoursEnd                   string
	QuietHoursTimezone              string
	AWSRegion                       string
	AWSAccessKeyID                  string
	AWSSecretAccessKey              string
	AWSEndpointOverride             string
	ConversationQueueURL            string
	ConversationJobsTable           string
	BedrockModelID                  string
	BedrockEmbeddingModelID         string

	// Gemini fallback provider configuration
	GeminiAPIKey        string
	GeminiModelID       string
	GeminiProjectID     string
	GeminiLocation      string
	LLMProvider         string // "bedrock" (default) or "gemini"
	LLMFallbackEnabled  bool
	LLMFallbackProvider string // Provider to use as fallback (default: "gemini")

	SupervisorEnabled      bool
	SupervisorMode         string
	SupervisorModelID      string
	SupervisorMaxLatency   time.Duration
	SupervisorSystemPrompt string
	RedisAddr              string
	RedisPassword          string
	RedisTLS               bool

	// SendGrid Email Configuration
	SendGridAPIKey    string
	SendGridFromEmail string
	SendGridFromName  string

	// AWS SES Email Configuration (preferred over SendGrid if configured)
	SESFromEmail string
	SESFromName  string

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

	// AWS Cognito Configuration
	CognitoUserPoolID string
	CognitoClientID   string
	CognitoRegion     string // defaults to AWSRegion if not set

	// Demo Mode Configuration (for 10DLC campaign compliance during testing)
	DemoMode       bool   // Wraps outbound messages with demo disclaimers
	DemoModePrefix string // e.g., "AI Wolf Solutions: "
	DemoModeSuffix string // e.g., " Demo only; no real services. Reply STOP to opt out."

	// Compliance Configuration
	SupervisorRequired  bool   // Enforce supervisor in production (panics if not enabled)
	DisclaimerEnabled   bool   // Add disclaimers to SMS messages
	DisclaimerLevel     string // "short", "medium", or "full"
	DisclaimerFirstOnly bool   // Only add disclaimer to first message in conversation
	AuditRetentionDays  int    // How long to retain audit logs (default: 2555 = 7 years)

	// Browser Sidecar Configuration (for scraping booking page availability)
	BrowserSidecarURL string // URL of the browser sidecar service (e.g., "http://localhost:3000")

	// S3 Archive Configuration (for archiving conversation data before purge)
	S3ArchiveBucket string // S3 bucket for conversation archives (e.g., "medspa-conversation-archives")
	S3ArchiveKMSKey string // Optional KMS key ID for SSE-KMS encryption
}

// SMSProviderIssues returns a list of configuration problems that would prevent
// SMS from working. An empty slice means at least one provider is fully configured.
// This is intended for startup diagnostics and integration tests — if the returned
// list is non-empty, voice-to-SMS acknowledgements will silently fail.
func (c *Config) SMSProviderIssues() []string {
	var issues []string

	telnyxOK := c.TelnyxAPIKey != "" && c.TelnyxMessagingProfileID != ""
	twilioOK := c.TwilioAccountSID != "" && c.TwilioAuthToken != ""

	if !telnyxOK && !twilioOK {
		issues = append(issues, "no SMS provider configured: need TELNYX_API_KEY+TELNYX_MESSAGING_PROFILE_ID or TWILIO_ACCOUNT_SID+TWILIO_AUTH_TOKEN")
	}
	if telnyxOK && c.TelnyxFromNumber == "" {
		issues = append(issues, "TELNYX_FROM_NUMBER is empty — outbound SMS will fail")
	}
	if twilioOK && c.TwilioFromNumber == "" && c.TwilioOrgMapJSON == "{}" {
		issues = append(issues, "TWILIO_FROM_NUMBER is empty and TWILIO_ORG_MAP_JSON has no entries — outbound SMS will fail")
	}
	return issues
}

// Load reads configuration from environment variables
func Load() *Config {
	corsAllowedOrigins := []string{}
	if raw := strings.TrimSpace(getEnv("CORS_ALLOWED_ORIGINS", "")); raw != "" {
		for _, origin := range strings.Split(raw, ",") {
			origin = strings.TrimSpace(origin)
			if origin == "" {
				continue
			}
			corsAllowedOrigins = append(corsAllowedOrigins, origin)
		}
	}
	bedrockModel := strings.TrimSpace(getEnv("BEDROCK_MODEL_ID", ""))
	supervisorModel := strings.TrimSpace(getEnv("SUPERVISOR_MODEL_ID", ""))
	if supervisorModel == "" {
		supervisorModel = bedrockModel
	}
	supervisorLatency := getEnvAsDuration("SUPERVISOR_MAX_LATENCY", 5*time.Second)
	if ms := getEnvAsInt("SUPERVISOR_MAX_LATENCY_MS", 0); ms > 0 {
		supervisorLatency = time.Duration(ms) * time.Millisecond
	}

	return &Config{
		Port:                            getEnv("PORT", "8080"),
		Env:                             getEnv("ENV", "development"),
		PublicBaseURL:                   getEnv("PUBLIC_BASE_URL", ""),
		LogLevel:                        getEnv("LOG_LEVEL", "info"),
		CORSAllowedOrigins:              corsAllowedOrigins,
		UseMemoryQueue:                  getEnvAsBool("USE_MEMORY_QUEUE", false),
		WorkerCount:                     getEnvAsInt("WORKER_COUNT", 2),
		DatabaseURL:                     getEnv("DATABASE_URL", ""),
		PersistConversationHistory:      getEnvAsBool("PERSIST_CONVERSATION_HISTORY", false),
		ConversationPersistExcludePhone: getEnv("CONVERSATION_PERSIST_EXCLUDE_PHONE", ""),
		SMSProvider:                     strings.ToLower(strings.TrimSpace(getEnv("SMS_PROVIDER", "auto"))),
		TelnyxAPIKey:                    getEnv("TELNYX_API_KEY", ""),
		TelnyxMessagingProfileID:        getEnv("TELNYX_MESSAGING_PROFILE_ID", ""),
		TelnyxWebhookSecret:             getEnv("TELNYX_WEBHOOK_SECRET", ""),
		TelnyxStopReply:                 getEnv("TELNYX_STOP_REPLY", "You have been opted out. Reply HELP for info."),
		TelnyxHelpReply:                 getEnv("TELNYX_HELP_REPLY", "Reply STOP to opt out or contact support@medspa.ai."),
		TelnyxStartReply:                getEnv("TELNYX_START_REPLY", "You're opted back in. Reply STOP to opt out."),
		TelnyxFirstContactReply:         getEnv("TELNYX_FIRST_CONTACT_REPLY", ""),
		TelnyxVoiceAckReply:             getEnv("TELNYX_VOICE_ACK_REPLY", ""),
		TelnyxFromNumber:                getEnv("TELNYX_FROM_NUMBER", ""),
		TelnyxTrackJobs:                 getEnvAsBool("TELNYX_TRACK_JOBS", false),
		TelnyxRetryMaxAttempts:          getEnvAsInt("TELNYX_RETRY_MAX_ATTEMPTS", 5),
		TelnyxRetryBaseDelay:            getEnvAsDuration("TELNYX_RETRY_BASE_DELAY", 5*time.Minute),
		TelnyxHostedPollInterval:        getEnvAsDuration("TELNYX_HOSTED_POLL_INTERVAL", 15*time.Minute),
		TwilioAccountSID:                getEnv("TWILIO_ACCOUNT_SID", ""),
		TwilioAuthToken:                 getEnv("TWILIO_AUTH_TOKEN", ""),
		TwilioWebhookSecret:             getEnv("TWILIO_WEBHOOK_SECRET", ""),
		TwilioFromNumber:                getEnv("TWILIO_FROM_NUMBER", ""),
		TwilioOrgMapJSON:                getEnv("TWILIO_ORG_MAP_JSON", ""),
		TwilioSkipSignature:             getEnvAsBool("TWILIO_SKIP_SIGNATURE", false),
		PaymentProviderKey:              getEnv("PAYMENT_PROVIDER_KEY", ""),
		SquareAccessToken:               getEnv("SQUARE_ACCESS_TOKEN", ""),
		SquareLocationID:                getEnv("SQUARE_LOCATION_ID", ""),
		SquareBaseURL:                   getEnv("SQUARE_BASE_URL", ""),
		SquareWebhookKey:                getEnv("SQUARE_WEBHOOK_SIGNATURE_KEY", ""),
		SquareSuccessURL:                getEnv("SQUARE_SUCCESS_URL", ""),
		SquareCancelURL:                 getEnv("SQUARE_CANCEL_URL", ""),
		SquareClientID:                  getEnv("SQUARE_CLIENT_ID", ""),
		SquareClientSecret:              getEnv("SQUARE_CLIENT_SECRET", ""),
		SquareOAuthRedirectURI:          getEnv("SQUARE_OAUTH_REDIRECT_URI", ""),
		SquareOAuthSuccessURL:           getEnv("SQUARE_OAUTH_SUCCESS_URL", ""),
		SquareSandbox:                   getEnvAsBool("SQUARE_SANDBOX", true),
		SquareCheckoutMode:              getEnv("SQUARE_CHECKOUT_MODE", "auto"),
		SquareCheckoutAllowFallback:     getEnvAsBool("SQUARE_CHECKOUT_ALLOW_FALLBACK", true),
		AllowFakePayments:               getEnvAsBool("ALLOW_FAKE_PAYMENTS", false),
		DepositAmountCents:              getEnvAsInt("DEPOSIT_AMOUNT_CENTS", 5000),
		SandboxAutoPurgePhones:          getEnv("SANDBOX_AUTO_PURGE_PHONE_DIGITS", ""),
		SandboxAutoPurgeDelay:           getEnvAsDuration("SANDBOX_AUTO_PURGE_DELAY", 0),
		AdminJWTSecret:                  getEnv("ADMIN_JWT_SECRET", ""),
		OnboardingToken:                 getEnv("ONBOARDING_TOKEN", ""),
		QuietHoursStart:                 getEnv("QUIET_HOURS_START", ""),
		QuietHoursEnd:                   getEnv("QUIET_HOURS_END", ""),
		QuietHoursTimezone:              getEnv("QUIET_HOURS_TZ", "UTC"),
		AWSRegion:                       getEnv("AWS_REGION", "us-east-1"),
		AWSAccessKeyID:                  getEnv("AWS_ACCESS_KEY_ID", ""),
		AWSSecretAccessKey:              getEnv("AWS_SECRET_ACCESS_KEY", ""),
		AWSEndpointOverride:             getEnv("AWS_ENDPOINT_OVERRIDE", ""),
		ConversationQueueURL:            getEnv("CONVERSATION_QUEUE_URL", ""),
		ConversationJobsTable:           getEnv("CONVERSATION_JOBS_TABLE", "conversation_jobs"),
		BedrockModelID:                  bedrockModel,
		BedrockEmbeddingModelID:         getEnv("BEDROCK_EMBEDDING_MODEL_ID", ""),

		// Gemini fallback configuration
		GeminiAPIKey:        getEnv("GEMINI_API_KEY", ""),
		GeminiModelID:       getEnv("GEMINI_MODEL_ID", "gemini-2.5-flash"),
		GeminiProjectID:     getEnv("GOOGLE_CLOUD_PROJECT", ""),
		GeminiLocation:      getEnv("GEMINI_LOCATION", "us-central1"),
		LLMProvider:         strings.ToLower(strings.TrimSpace(getEnv("LLM_PROVIDER", "bedrock"))),
		LLMFallbackEnabled:  getEnvAsBool("LLM_FALLBACK_ENABLED", false),
		LLMFallbackProvider: strings.ToLower(strings.TrimSpace(getEnv("LLM_FALLBACK_PROVIDER", "gemini"))),

		SupervisorEnabled:      getEnvAsBool("SUPERVISOR_ENABLED", false),
		SupervisorMode:         strings.ToLower(strings.TrimSpace(getEnv("SUPERVISOR_MODE", "warn"))),
		SupervisorModelID:      supervisorModel,
		SupervisorMaxLatency:   supervisorLatency,
		SupervisorSystemPrompt: strings.TrimSpace(getEnv("SUPERVISOR_SYSTEM_PROMPT", "")),
		RedisAddr:              getEnv("REDIS_ADDR", "redis:6379"),
		RedisPassword:          getEnv("REDIS_PASSWORD", ""),
		RedisTLS:               getEnvAsBool("REDIS_TLS", false),

		// SendGrid Email Configuration
		SendGridAPIKey:    getEnv("SENDGRID_API_KEY", ""),
		SendGridFromEmail: getEnv("SENDGRID_FROM_EMAIL", ""),
		SendGridFromName:  getEnv("SENDGRID_FROM_NAME", "MedSpa AI"),

		// AWS SES Email Configuration (preferred over SendGrid if configured)
		SESFromEmail: getEnv("SES_FROM_EMAIL", ""),
		SESFromName:  getEnv("SES_FROM_NAME", "MedSpa AI"),

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

		// AWS Cognito Configuration
		CognitoUserPoolID: getEnv("COGNITO_USER_POOL_ID", ""),
		CognitoClientID:   getEnv("COGNITO_CLIENT_ID", ""),
		CognitoRegion:     getEnv("COGNITO_REGION", getEnv("AWS_REGION", "us-east-1")),

		// Demo Mode Configuration
		DemoMode:       getEnvAsBool("DEMO_MODE", false),
		DemoModePrefix: getEnv("DEMO_MODE_PREFIX", "AI Wolf Solutions: "),
		DemoModeSuffix: getEnv("DEMO_MODE_SUFFIX", " Demo only; no real services. Reply STOP to opt out."),

		// Compliance Configuration
		SupervisorRequired:  getEnvAsBool("SUPERVISOR_REQUIRED", false),
		DisclaimerEnabled:   getEnvAsBool("DISCLAIMER_ENABLED", true),
		DisclaimerLevel:     strings.ToLower(strings.TrimSpace(getEnv("DISCLAIMER_LEVEL", "medium"))),
		DisclaimerFirstOnly: getEnvAsBool("DISCLAIMER_FIRST_ONLY", true),
		AuditRetentionDays:  getEnvAsInt("AUDIT_RETENTION_DAYS", 2555), // 7 years for HIPAA

		// Browser Sidecar Configuration
		BrowserSidecarURL: getEnv("BROWSER_SIDECAR_URL", ""),

		// S3 Archive Configuration
		S3ArchiveBucket: getEnv("S3_ARCHIVE_BUCKET", ""),
		S3ArchiveKMSKey: getEnv("S3_ARCHIVE_KMS_KEY", ""),
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
