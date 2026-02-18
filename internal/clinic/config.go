// Package clinic provides clinic-specific configuration and business logic.
package clinic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

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
	// BookingPlatform specifies which booking system the clinic uses: "moxie" or "square" (default: "square")
	// When "moxie", the AI will use browser automation to book through Moxie's widget
	// When "square", the AI will send a Square payment link for deposit collection
	BookingPlatform string            `json:"booking_platform,omitempty"`
	Notifications   NotificationPrefs `json:"notifications"`
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
}

// ServiceNeedsProviderPreference returns true if the given service (by normalized name)
// has more than one eligible provider.
func (c *Config) ServiceNeedsProviderPreference(serviceName string) bool {
	if c.MoxieConfig == nil || c.MoxieConfig.ServiceProviderCount == nil || c.MoxieConfig.ServiceMenuItems == nil {
		return false
	}
	resolved := c.ResolveServiceName(serviceName)
	itemID := c.MoxieConfig.ServiceMenuItems[strings.ToLower(resolved)]
	if itemID == "" {
		itemID = c.MoxieConfig.ServiceMenuItems[strings.ToLower(serviceName)]
	}
	if itemID == "" {
		return false
	}
	return c.MoxieConfig.ServiceProviderCount[itemID] > 1
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

func normalizeServiceKey(service string) string {
	return strings.ToLower(strings.TrimSpace(service))
}

// ResolveServiceName translates a patient-facing service name (e.g. "Botox") into the
// booking-platform search term using the clinic's ServiceAliases map. If no alias is
// configured the original name is returned unchanged.
func (c *Config) ResolveServiceName(service string) string {
	if c == nil || len(c.ServiceAliases) == 0 {
		return service
	}
	key := normalizeServiceKey(service)
	if alias, ok := c.ServiceAliases[key]; ok && alias != "" {
		return alias
	}
	// Try without trailing 's' (handle plurals like "lip fillers" → "lip filler")
	if strings.HasSuffix(key, "s") {
		if alias, ok := c.ServiceAliases[strings.TrimSuffix(key, "s")]; ok && alias != "" {
			return alias
		}
	}
	// Fuzzy match: check if the service contains an alias key or vice versa.
	// Prefer the longest matching key to avoid "filler" matching before "lip filler".
	bestAlias := ""
	bestKeyLen := 0
	for aliasKey, alias := range c.ServiceAliases {
		if alias == "" {
			continue
		}
		if strings.Contains(key, aliasKey) || strings.Contains(aliasKey, key) {
			if len(aliasKey) > bestKeyLen {
				bestAlias = alias
				bestKeyLen = len(aliasKey)
			}
		}
	}
	if bestAlias != "" {
		return bestAlias
	}
	return service
}

// GetServiceVariants returns the delivery variants for a service, if any.
// For example, "weight loss" might return ["Weight Loss Consultation - In Person", "Weight Loss Consultation - Virtual"].
// Returns nil if the service has no variants configured.
func (c *Config) GetServiceVariants(service string) []string {
	if c == nil || len(c.ServiceVariants) == 0 {
		return nil
	}
	key := normalizeServiceKey(service)
	// Exact match first
	if variants, ok := c.ServiceVariants[key]; ok && len(variants) > 1 {
		return variants
	}
	// Fuzzy match: check if the service contains a variant key or vice versa
	// e.g. "weight loss consultation" contains "weight loss"
	for variantKey, variants := range c.ServiceVariants {
		if len(variants) <= 1 {
			continue
		}
		if strings.Contains(key, variantKey) || strings.Contains(variantKey, key) {
			return variants
		}
	}
	return nil
}

// UsesMoxieBooking returns true if the clinic is configured to use Moxie for booking.
// When true, Square is NOT used — the patient completes payment on Moxie's Step 5 page.
func (c *Config) UsesMoxieBooking() bool {
	if c == nil {
		return false
	}
	return strings.ToLower(c.BookingPlatform) == "moxie"
}

// UsesStripePayment returns true if the clinic is configured to use Stripe for deposit collection.
func (c *Config) UsesStripePayment() bool {
	if c == nil {
		return false
	}
	return strings.ToLower(c.PaymentProvider) == "stripe"
}

// UsesSquarePayment returns true if the clinic uses Square for deposit collection.
// This is the default when no booking platform is specified. Mutually exclusive
// with Moxie — a clinic uses one or the other, never both.
func (c *Config) UsesSquarePayment() bool {
	if c == nil {
		return true // Default to Square
	}
	platform := strings.ToLower(c.BookingPlatform)
	return platform == "" || platform == "square"
}

// DepositAmountForService returns the configured deposit amount (in cents) for a service,
// falling back to the clinic default when no override is present.
func (c *Config) DepositAmountForService(service string) int {
	if c == nil {
		return 0
	}
	key := normalizeServiceKey(service)
	if key != "" && c.ServiceDepositAmountCents != nil {
		if amount, ok := c.ServiceDepositAmountCents[key]; ok && amount > 0 {
			return amount
		}
	}
	if c.DepositAmountCents > 0 {
		return c.DepositAmountCents
	}
	return 0
}

// PriceTextForService returns a configured price string for a service when available.
func (c *Config) PriceTextForService(service string) (string, bool) {
	if c == nil || c.ServicePriceText == nil {
		return "", false
	}
	key := normalizeServiceKey(service)
	if key == "" {
		return "", false
	}
	price := strings.TrimSpace(c.ServicePriceText[key])
	if price == "" {
		return "", false
	}
	return price, true
}

// GetHoursForDay returns the hours for a given weekday (0=Sunday, 6=Saturday).
func (b *BusinessHours) GetHoursForDay(weekday time.Weekday) *DayHours {
	switch weekday {
	case time.Sunday:
		return b.Sunday
	case time.Monday:
		return b.Monday
	case time.Tuesday:
		return b.Tuesday
	case time.Wednesday:
		return b.Wednesday
	case time.Thursday:
		return b.Thursday
	case time.Friday:
		return b.Friday
	case time.Saturday:
		return b.Saturday
	default:
		return nil
	}
}

// HasAnyHours returns true if at least one day has business hours configured.
func (b *BusinessHours) HasAnyHours() bool {
	return b.Sunday != nil || b.Monday != nil || b.Tuesday != nil ||
		b.Wednesday != nil || b.Thursday != nil || b.Friday != nil || b.Saturday != nil
}

// IsOpenAt checks if the clinic is open at the given time.
// If no business hours are configured, the clinic is treated as always open
// (e.g., "by appointment only" clinics with no set hours).
func (c *Config) IsOpenAt(t time.Time) bool {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		loc = time.UTC
	}
	localTime := t.In(loc)

	hours := c.BusinessHours.GetHoursForDay(localTime.Weekday())
	if hours == nil {
		// No hours configured for this day — if NO hours are configured at all,
		// assume always open (appointment-only clinic). Otherwise, closed today.
		return !c.BusinessHours.HasAnyHours()
	}

	openTime, err := time.Parse("15:04", hours.Open)
	if err != nil {
		return false
	}
	closeTime, err := time.Parse("15:04", hours.Close)
	if err != nil {
		return false
	}

	currentMinutes := localTime.Hour()*60 + localTime.Minute()
	openMinutes := openTime.Hour()*60 + openTime.Minute()
	closeMinutes := closeTime.Hour()*60 + closeTime.Minute()

	return currentMinutes >= openMinutes && currentMinutes < closeMinutes
}

// NextOpenTime returns when the clinic next opens.
// Returns the current time if already open.
func (c *Config) NextOpenTime(t time.Time) time.Time {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		loc = time.UTC
	}
	localTime := t.In(loc)

	// Check up to 7 days ahead
	for i := 0; i < 7; i++ {
		checkDate := localTime.AddDate(0, 0, i)
		hours := c.BusinessHours.GetHoursForDay(checkDate.Weekday())

		if hours == nil {
			continue // Closed this day
		}

		openTime, err := time.Parse("15:04", hours.Open)
		if err != nil {
			continue
		}

		openDateTime := time.Date(
			checkDate.Year(), checkDate.Month(), checkDate.Day(),
			openTime.Hour(), openTime.Minute(), 0, 0, loc,
		)

		// If it's today and we haven't passed opening time yet
		if i == 0 {
			closeTime, _ := time.Parse("15:04", hours.Close)
			closeDateTime := time.Date(
				checkDate.Year(), checkDate.Month(), checkDate.Day(),
				closeTime.Hour(), closeTime.Minute(), 0, 0, loc,
			)

			if localTime.Before(openDateTime) {
				return openDateTime
			}
			if localTime.Before(closeDateTime) {
				return localTime // Already open
			}
			// Past closing, check next day
			continue
		}

		return openDateTime
	}

	// Fallback: return tomorrow 9 AM
	return time.Date(localTime.Year(), localTime.Month(), localTime.Day()+1, 9, 0, 0, 0, loc)
}

// BusinessHoursContext generates a string describing current status for the LLM.
func (c *Config) BusinessHoursContext(t time.Time) string {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		loc = time.UTC
	}
	localTime := t.In(loc)

	isOpen := c.IsOpenAt(t)
	nextOpen := c.NextOpenTime(t)

	status := "CLOSED"
	if isOpen {
		status = "OPEN"
	}

	hours := c.BusinessHours.GetHoursForDay(localTime.Weekday())
	todayHours := "Closed today"
	if hours != nil {
		todayHours = fmt.Sprintf("%s - %s", hours.Open, hours.Close)
	}

	ctx := fmt.Sprintf(
		"Clinic: %s\n"+
			"Current time: %s (%s)\n"+
			"Status: %s\n"+
			"Today's hours: %s\n",
		c.Name,
		localTime.Format("Monday, January 2, 2006 3:04 PM"),
		c.Timezone,
		status,
		todayHours,
	)

	if !isOpen {
		ctx += fmt.Sprintf("Next open: %s\n", nextOpen.Format("Monday at 3:04 PM"))
		// Calculate callback expectation for after-hours messages
		callbackTime := c.ExpectedCallbackTime(t)
		ctx += fmt.Sprintf("CALLBACK INSTRUCTION: When the clinic is closed, tell patients our team will reach out around %s. NEVER say '24 hours' if we're closed for the weekend or holiday.\n", callbackTime)
	} else {
		ctx += "CALLBACK INSTRUCTION: We're currently open! Our team can reach out shortly.\n"
	}

	ctx += fmt.Sprintf("Callback SLA: %d business hours\n", c.CallbackSLAHours)

	return ctx
}

// ExpectedCallbackTime returns a human-friendly string for when the patient can expect a callback.
// It accounts for business hours and provides a realistic expectation.
func (c *Config) ExpectedCallbackTime(t time.Time) string {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		loc = time.UTC
	}
	localTime := t.In(loc)
	nextOpen := c.NextOpenTime(t)

	// If we're open now, callback within the hour
	if c.IsOpenAt(t) {
		return "shortly"
	}

	// Calculate days until next open
	daysUntil := int(nextOpen.Sub(localTime).Hours() / 24)
	nextOpenLocal := nextOpen.In(loc)

	// Same day (later today)
	if localTime.YearDay() == nextOpenLocal.YearDay() && localTime.Year() == nextOpenLocal.Year() {
		return fmt.Sprintf("this %s around %s", strings.ToLower(nextOpenLocal.Format("Monday")), nextOpenLocal.Format("3 PM"))
	}

	// Tomorrow
	tomorrow := localTime.AddDate(0, 0, 1)
	if tomorrow.YearDay() == nextOpenLocal.YearDay() && tomorrow.Year() == nextOpenLocal.Year() {
		return fmt.Sprintf("tomorrow (%s) around %s", nextOpenLocal.Format("Monday"), nextOpenLocal.Format("3 PM"))
	}

	// This week (2-6 days out)
	if daysUntil <= 6 {
		return fmt.Sprintf("on %s around %s", nextOpenLocal.Format("Monday"), nextOpenLocal.Format("3 PM"))
	}

	// Fallback for longer periods
	return fmt.Sprintf("on %s around %s", nextOpenLocal.Format("Monday, January 2"), nextOpenLocal.Format("3 PM"))
}

// AIPersonaContext generates a string describing the AI persona for the LLM.
// This is injected into the conversation context to customize the AI's voice.
func (c *Config) AIPersonaContext() string {
	if c == nil {
		return ""
	}

	persona := c.AIPersona
	var parts []string

	// If solo operator, add context about the provider
	if persona.IsSoloOperator && persona.ProviderName != "" {
		parts = append(parts, fmt.Sprintf(
			"CLINIC CONTEXT - SOLO PRACTITIONER:\n"+
				"This clinic is operated by %s as a solo practitioner. You are %s's AI assistant (digital assistant).\n"+
				"IMPORTANT: Always identify yourself as %s's AI assistant or virtual assistant - never pretend to BE %s.\n"+
				"- Example greeting: 'Hi! This is %s's AI assistant at %s. %s is currently with a patient...'\n"+
				"- The patient should know they're texting with an AI system, not directly with %s\n"+
				"- The provider handles ALL patient care personally - there is no front desk staff\n"+
				"- This is a boutique, personality-driven practice where clients come specifically for %s",
			persona.ProviderName, persona.ProviderName, persona.ProviderName, persona.ProviderName,
			persona.ProviderName, c.Name, persona.ProviderName, persona.ProviderName, persona.ProviderName,
		))
	} else if persona.ProviderName != "" {
		parts = append(parts, fmt.Sprintf("Primary provider: %s. You are the clinic's AI assistant.", persona.ProviderName))
	}

	// Custom greetings for initial contact (business hours vs after hours)
	if persona.CustomGreeting != "" || persona.AfterHoursGreeting != "" {
		var greetingParts []string
		if persona.CustomGreeting != "" {
			greetingParts = append(greetingParts, fmt.Sprintf(
				"DURING BUSINESS HOURS greeting: \"%s\"", persona.CustomGreeting))
		}
		if persona.AfterHoursGreeting != "" {
			greetingParts = append(greetingParts, fmt.Sprintf(
				"AFTER HOURS greeting (evenings/weekends/closed): \"%s\"", persona.AfterHoursGreeting))
		}
		if len(greetingParts) > 0 {
			parts = append(parts, "GREETING STYLE:\n"+strings.Join(greetingParts, "\n")+
				"\nIMPORTANT: Check the clinic status above to determine which greeting to use.")
		}
	}

	// Busy message for why the provider can't answer
	if persona.BusyMessage != "" {
		parts = append(parts, fmt.Sprintf("BUSY MESSAGE: If explaining why we couldn't answer a call: \"%s\"", persona.BusyMessage))
	}

	// Tone guidance
	if persona.Tone != "" {
		switch strings.ToLower(persona.Tone) {
		case "clinical":
			parts = append(parts, "TONE: Clinical and professional. Focus on medical accuracy and patient safety.")
		case "warm":
			parts = append(parts, "TONE: Warm and approachable. Make patients feel comfortable while maintaining professionalism.")
		case "professional":
			parts = append(parts, "TONE: Straightforward and professional. Efficient communication focused on booking.")
		}
	}

	// Special non-cosmetic medical services
	if len(persona.SpecialServices) > 0 {
		parts = append(parts, fmt.Sprintf(
			"MEDICAL SERVICES NOTE: This clinic also offers medical treatments beyond cosmetics: %s. "+
				"These are functional medical treatments - handle inquiries with appropriate medical sensitivity.",
			strings.Join(persona.SpecialServices, ", "),
		))
	}

	if len(parts) == 0 {
		return ""
	}

	return "\n\n" + strings.Join(parts, "\n\n")
}

// Store provides persistence for clinic configurations.
type Store struct {
	redis *redis.Client
}

// NewStore creates a new clinic config store.
func NewStore(redisClient *redis.Client) *Store {
	return &Store{redis: redisClient}
}

func (s *Store) key(orgID string) string {
	return fmt.Sprintf("clinic:config:%s", orgID)
}

// Get retrieves clinic config, returning default if not found.
func (s *Store) Get(ctx context.Context, orgID string) (*Config, error) {
	data, err := s.redis.Get(ctx, s.key(orgID)).Bytes()
	if err == redis.Nil {
		return DefaultConfig(orgID), nil
	}
	if err != nil {
		return nil, fmt.Errorf("clinic: get config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("clinic: unmarshal config: %w", err)
	}

	return &cfg, nil
}

// GetStripeAccountID retrieves the Stripe account ID for a clinic.
func (s *Store) GetStripeAccountID(ctx context.Context, orgID string) (string, error) {
	cfg, err := s.Get(ctx, orgID)
	if err != nil {
		return "", fmt.Errorf("clinic: get stripe account: %w", err)
	}
	return cfg.StripeAccountID, nil
}

// SaveStripeAccountID updates the clinic's Stripe account ID and sets the payment provider to "stripe".
// This satisfies the payments.StripeConfigSaver interface for Stripe Connect onboarding.
func (s *Store) SaveStripeAccountID(ctx context.Context, orgID, accountID string) error {
	cfg, err := s.Get(ctx, orgID)
	if err != nil {
		return fmt.Errorf("clinic: save stripe account: get: %w", err)
	}
	cfg.StripeAccountID = accountID
	cfg.PaymentProvider = "stripe"
	return s.Set(ctx, cfg)
}

// Set saves clinic config.
func (s *Store) Set(ctx context.Context, cfg *Config) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("clinic: marshal config: %w", err)
	}

	if err := s.redis.Set(ctx, s.key(cfg.OrgID), data, 0).Err(); err != nil {
		return fmt.Errorf("clinic: set config: %w", err)
	}

	return nil
}
