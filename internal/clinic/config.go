// Package clinic provides clinic-specific configuration and business logic.
package clinic

// DayHours represents the opening hours for a single day.
// Nil means the clinic is closed that day.
type DayHours struct {
	Open  string `json:"open"`  // "09:00" in 24-hour format
	Close string `json:"close"` // "18:00" in 24-hour format
}

// BusinessHours maps day names to their hours.
type BusinessHours struct {
	Monday    *DayHours `json:"monday,omitempty"`
	Tuesday   *DayHours `json:"tuesday,omitempty"`
	Wednesday *DayHours `json:"wednesday,omitempty"`
	Thursday  *DayHours `json:"thursday,omitempty"`
	Friday    *DayHours `json:"friday,omitempty"`
	Saturday  *DayHours `json:"saturday,omitempty"`
	Sunday    *DayHours `json:"sunday,omitempty"`
}

// NotificationPrefs holds notification preferences for a clinic.
type NotificationPrefs struct {
	// Email notifications
	EmailEnabled    bool     `json:"email_enabled"`
	EmailRecipients []string `json:"email_recipients,omitempty"` // e.g., ["owner@clinic.com"]

	// SMS notifications to operator
	SMSEnabled    bool     `json:"sms_enabled"`
	SMSRecipient  string   `json:"sms_recipient,omitempty"`  // Legacy: single operator's cell phone
	SMSRecipients []string `json:"sms_recipients,omitempty"` // Multiple operator phone numbers

	// What to notify about
	NotifyOnPayment bool `json:"notify_on_payment"`  // When deposit is paid
	NotifyOnNewLead bool `json:"notify_on_new_lead"` // When new lead comes in
}

// GetSMSRecipients returns all configured SMS recipients, merging legacy single
// recipient with the new array. Duplicates are removed.
func (n *NotificationPrefs) GetSMSRecipients() []string {
	seen := make(map[string]struct{})
	var recipients []string

	// Add legacy single recipient first
	if n.SMSRecipient != "" {
		seen[n.SMSRecipient] = struct{}{}
		recipients = append(recipients, n.SMSRecipient)
	}

	// Add new array recipients
	for _, r := range n.SMSRecipients {
		if r != "" {
			if _, ok := seen[r]; !ok {
				seen[r] = struct{}{}
				recipients = append(recipients, r)
			}
		}
	}

	return recipients
}

// AIPersona configures the AI assistant's voice and personality for a clinic.
type AIPersona struct {
	// ProviderName is the name the AI uses (e.g., "Brandi" for solo practitioners)
	ProviderName string `json:"provider_name,omitempty"`
	// IsSoloOperator indicates if the clinic is run by a single provider (affects messaging)
	IsSoloOperator bool `json:"is_solo_operator,omitempty"`
	// Tone sets the communication style: "clinical", "warm", "professional" (default: "warm")
	Tone string `json:"tone,omitempty"`
	// CustomGreeting overrides the default greeting during business hours
	CustomGreeting string `json:"custom_greeting,omitempty"`
	// AfterHoursGreeting is used when the clinic is closed (evenings, weekends)
	AfterHoursGreeting string `json:"after_hours_greeting,omitempty"`
	// BusyMessage explains why the provider can't answer (for solo operators)
	BusyMessage string `json:"busy_message,omitempty"`
	// SpecialServices lists non-cosmetic medical services offered (e.g., hyperhidrosis, migraines)
	SpecialServices []string `json:"special_services,omitempty"`
}

// Config holds clinic-specific configuration.
type Config struct {
	OrgID string `json:"org_id"`
	Name  string `json:"name"`
	// LegalName is the business name exactly as it appears on IRS filings.
	// Required for 10DLC brand registration. May differ from the DBA/display Name.
	LegalName string `json:"legal_name,omitempty"`
	// EIN is the Employer Identification Number for 10DLC registration.
	EIN        string `json:"ein,omitempty"`
	Email      string `json:"email,omitempty"`
	Phone      string `json:"phone,omitempty"`
	Address    string `json:"address,omitempty"`
	City       string `json:"city,omitempty"`
	State      string `json:"state,omitempty"`
	ZipCode    string `json:"zip_code,omitempty"`
	WebsiteURL string `json:"website_url,omitempty"`
	// SMSPhoneNumber is the clinic's phone number used for SMS (may differ from main phone).
	SMSPhoneNumber string `json:"sms_phone_number,omitempty"`
	// SMSPhoneType is "landline", "voip", or "cell" — determines LOA eligibility.
	SMSPhoneType string `json:"sms_phone_type,omitempty"`
	// LOAStatus tracks the Letter of Authorization status: "not_started", "pending", "approved", "rejected".
	LOAStatus string `json:"loa_status,omitempty"`
	// LOAOrderID is the Telnyx hosted messaging order ID for LOA tracking.
	LOAOrderID string `json:"loa_order_id,omitempty"`
	// TenDLCStatus tracks overall 10DLC registration: "not_started", "brand_pending", "campaign_pending", "active", "rejected".
	TenDLCStatus           string        `json:"ten_dlc_status,omitempty"`
	Timezone               string        `json:"timezone"` // e.g., "America/New_York"
	ClinicInfoConfirmed    bool          `json:"clinic_info_confirmed"`
	BusinessHoursConfirmed bool          `json:"business_hours_confirmed"`
	ServicesConfirmed      bool          `json:"services_confirmed"`
	ContactInfoConfirmed   bool          `json:"contact_info_confirmed"`
	BusinessHours          BusinessHours `json:"business_hours"`
	CallbackSLAHours       int           `json:"callback_sla_hours"`   // e.g., 12
	DepositAmountCents     int           `json:"deposit_amount_cents"` // e.g., 5000
	// ServiceDepositAmountCents overrides the default deposit per service (keyed by normalized service name).
	ServiceDepositAmountCents map[string]int `json:"service_deposit_amount_cents,omitempty"`
	// ServicePriceText provides a human-readable price string per service (keyed by normalized service name).
	ServicePriceText map[string]string `json:"service_price_text,omitempty"`
	Services         []string          `json:"services,omitempty"` // e.g., ["Botox", "Fillers"]
	// BookingURL is the clinic's online booking page (e.g., Moxie, Calendly, etc.)
	BookingURL string `json:"booking_url,omitempty"`
	// BookingPlatform specifies which booking system the clinic uses: "moxie", "boulevard", "vagaro", or "square" (default: "square")
	// When "moxie", the AI will use Moxie integration for availability + booking.
	// When "boulevard", the AI will use Boulevard GraphQL cart-based booking.
	// When "vagaro", the AI will use Vagaro REST integration for availability + booking.
	// When "square", the AI will send a Square payment link for deposit collection.
	BookingPlatform string `json:"booking_platform,omitempty"`
	// VagaroBusinessAlias identifies the clinic on Vagaro (e.g., the {businessAlias} in vagaro.com/{businessAlias}).
	// Used when BookingPlatform == "vagaro".
	VagaroBusinessAlias string            `json:"vagaro_business_alias,omitempty"`
	Notifications       NotificationPrefs `json:"notifications"`
	// AIPersona customizes the AI assistant's voice for this clinic
	AIPersona AIPersona `json:"ai_persona,omitempty"`
	// StripeAccountID is the connected Stripe account ID for clinics using Stripe Connect.
	StripeAccountID string `json:"stripe_account_id,omitempty"`
	// PaymentProvider specifies which payment processor: "square" (default) or "stripe".
	PaymentProvider string `json:"payment_provider,omitempty"`
	// ServiceAliases maps common patient-facing names to the actual service name
	// on the booking platform. For example, {"botox": "Tox", "wrinkle relaxers": "Tox"}.
	// Keys are normalized (lowercased). Values are the search term used by the scraper.
	ServiceAliases map[string]string `json:"service_aliases,omitempty"`

	// ServiceVariants maps a generic service name to its delivery variants.
	// When a patient requests a service that has variants (e.g. "weight loss"
	// has "in-person" and "virtual"), the AI asks which they prefer before
	// fetching availability. Keys are normalized (lowercased).
	// Example: {"weight loss": ["Weight Loss Consultation - In Person", "Weight Loss Consultation - Virtual"]}
	ServiceVariants map[string][]string `json:"service_variants,omitempty"`

	// BookingPolicies are shown to the patient BEFORE the payment link so they
	// give informed consent (e.g., 24-hour cancellation, no-show fee).
	// Each string is sent as a separate line in the pre-payment SMS.
	BookingPolicies []string `json:"booking_policies,omitempty"`

	// MoxieConfig holds Moxie-specific IDs needed for direct GraphQL API booking.
	// Only used when BookingPlatform == "moxie".
	MoxieConfig *MoxieConfig `json:"moxie_config,omitempty"`

	// BookingAdapter specifies which booking adapter to use: "moxie", "manual", "boulevard".
	// Defaults to "manual" if no EMR/booking platform is configured.
	BookingAdapter string `json:"booking_adapter,omitempty"`
	// HandoffNotificationPhone is the clinic owner's phone number for manual handoff SMS alerts.
	HandoffNotificationPhone string `json:"handoff_notification_phone,omitempty"`
	// HandoffNotificationEmail is the clinic owner's email for manual handoff email alerts.
	HandoffNotificationEmail string `json:"handoff_notification_email,omitempty"`

	// Boulevard API credentials (used when BookingPlatform == "boulevard").
	BoulevardAPIKey     string `json:"boulevard_api_key,omitempty"`
	BoulevardBusinessID string `json:"boulevard_business_id,omitempty"`

	// VoiceAIEnabled controls whether inbound voice calls use Telnyx Voice AI.
	// When false (default), calls fall through to voicemail → SMS text-back flow.
	VoiceAIEnabled bool `json:"voice_ai_enabled"`
	// TelnyxAssistantID is the Telnyx AI Assistant ID configured for this clinic's voice calls.
	TelnyxAssistantID string `json:"telnyx_assistant_id,omitempty"`
	// VoiceAIConfig holds voice-specific settings for Telnyx AI Assistant integration.
	VoiceAIConfig *VoiceAIConfig `json:"voice_ai_config,omitempty"`
}

// VoiceAIConfig holds voice AI configuration for a clinic.
type VoiceAIConfig struct {
	// Greeting is spoken when the AI assistant answers the call.
	Greeting string `json:"greeting,omitempty"`
	// AfterHoursGreeting is used when the clinic is closed.
	AfterHoursGreeting string `json:"after_hours_greeting,omitempty"`
	// MaxConcurrentCalls limits how many simultaneous voice calls the clinic can handle.
	MaxConcurrentCalls int `json:"max_concurrent_calls,omitempty"`
	// RecordingEnabled controls whether calls are recorded (requires consent announcement).
	RecordingEnabled bool `json:"recording_enabled,omitempty"`
	// RecordingConsentMessage is spoken before recording starts.
	RecordingConsentMessage string `json:"recording_consent_message,omitempty"`
	// TransferNumber is the fallback phone number for live agent transfer.
	TransferNumber string `json:"transfer_number,omitempty"`
	// BusinessHoursOnly limits voice AI to business hours; after hours falls back to voicemail.
	BusinessHoursOnly bool `json:"business_hours_only,omitempty"`
}

// MoxieConfig contains Moxie platform-specific identifiers for direct API integration.
type MoxieConfig struct {
	// MedspaID is the Moxie internal ID (e.g., "1264" for Forever 22).
	MedspaID string `json:"medspa_id"`
	// MedspaSlug is the URL slug (e.g., "forever-22").
	MedspaSlug string `json:"medspa_slug"`
	// ServiceMenuItems maps normalized service names to Moxie serviceMenuItemId.
	// e.g., {"lip filler": "20425", "tox": "20424"}
	ServiceMenuItems map[string]string `json:"service_menu_items,omitempty"`
	// DefaultProviderID is used when patient has no provider preference.
	// Empty string means "no preference" (Moxie assigns one).
	DefaultProviderID string `json:"default_provider_id,omitempty"`
	// ServiceProviderCount maps service menu item IDs to the number of eligible providers.
	// When a service has >1 provider, the system asks for provider preference before booking.
	// e.g., {"18430": 2, "18427": 1}
	ServiceProviderCount map[string]int `json:"service_provider_count,omitempty"`
	// ProviderNames maps provider IDs to display names (e.g., {"33150": "Brandi Sesock"}).
	ProviderNames map[string]string `json:"provider_names,omitempty"`
	// ServiceProviders maps service menu item IDs to eligible provider IDs for that specific service.
	// e.g., {"18430": ["33150", "33151"]}
	ServiceProviders map[string][]string `json:"service_providers,omitempty"`
}

// DefaultBookingURL is the default test booking page for development/demo purposes.
const DefaultBookingURL = "https://portal-dev.aiwolfsolutions.com/booking/index.html"

// DefaultConfig returns a sensible default configuration.
func DefaultConfig(orgID string) *Config {
	return &Config{
		OrgID:                  orgID,
		Name:                   "MedSpa",
		Email:                  "",
		Phone:                  "",
		Address:                "",
		City:                   "",
		State:                  "",
		ZipCode:                "",
		WebsiteURL:             "",
		Timezone:               "America/New_York",
		ClinicInfoConfirmed:    false,
		BusinessHoursConfirmed: false,
		ServicesConfirmed:      false,
		ContactInfoConfirmed:   false,
		BusinessHours: BusinessHours{
			Monday:    &DayHours{Open: "09:00", Close: "18:00"},
			Tuesday:   &DayHours{Open: "09:00", Close: "18:00"},
			Wednesday: &DayHours{Open: "09:00", Close: "18:00"},
			Thursday:  &DayHours{Open: "09:00", Close: "18:00"},
			Friday:    &DayHours{Open: "09:00", Close: "17:00"},
			Saturday:  nil, // Closed
			Sunday:    nil, // Closed
		},
		CallbackSLAHours:          12,
		DepositAmountCents:        5000,
		ServiceDepositAmountCents: map[string]int{},
		ServicePriceText:          map[string]string{},
		Services:                  []string{"Botox", "Fillers", "Laser Treatments"},
		BookingURL:                DefaultBookingURL,
		Notifications: NotificationPrefs{
			EmailEnabled:    false, // Disabled by default until configured
			SMSEnabled:      false,
			NotifyOnPayment: true, // When enabled, notify on payment by default
			NotifyOnNewLead: false,
		},
	}
}
