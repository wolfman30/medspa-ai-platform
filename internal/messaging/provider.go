package messaging

import (
	"fmt"
	"strings"

	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

const (
	// SMSProviderAuto tries Telnyx first, then Twilio.
	SMSProviderAuto = "auto"
	// SMSProviderTelnyx forces the Telnyx sender when credentials exist.
	SMSProviderTelnyx = "telnyx"
	// SMSProviderTwilio forces the Twilio sender when credentials exist.
	SMSProviderTwilio = "twilio"
)

// ProviderSelectionConfig captures the credentials required to build outbound messengers.
type ProviderSelectionConfig struct {
	Preference       string
	TelnyxAPIKey     string
	TelnyxProfileID  string
	TwilioAccountSID string
	TwilioAuthToken  string
	TwilioFromNumber string
}

// BuildReplyMessenger instantiates a ReplyMessenger based on the preferred provider.
// It returns the messenger, the provider that was selected, and a reason when no provider could be initialized.
func BuildReplyMessenger(cfg ProviderSelectionConfig, logger *logging.Logger) (conversation.ReplyMessenger, string, string) {
	if logger == nil {
		logger = logging.Default()
	}
	preference := strings.ToLower(strings.TrimSpace(cfg.Preference))
	if preference == "" {
		preference = SMSProviderAuto
	}

	missing := map[string]string{}
	var telnyxMessenger conversation.ReplyMessenger
	var twilioMessenger conversation.ReplyMessenger

	if cfg.TelnyxAPIKey != "" && cfg.TelnyxProfileID != "" {
		telnyxMessenger = NewTelnyxSender(cfg.TelnyxAPIKey, cfg.TelnyxProfileID, logger)
	} else {
		var reasons []string
		if cfg.TelnyxAPIKey == "" {
			reasons = append(reasons, "TELNYX_API_KEY missing")
		}
		if cfg.TelnyxProfileID == "" {
			reasons = append(reasons, "TELNYX_MESSAGING_PROFILE_ID missing")
		}
		missing[SMSProviderTelnyx] = strings.Join(reasons, ", ")
	}

	if cfg.TwilioAccountSID != "" && cfg.TwilioAuthToken != "" {
		twilioMessenger = NewTwilioSender(cfg.TwilioAccountSID, cfg.TwilioAuthToken, cfg.TwilioFromNumber, logger)
	} else {
		var reasons []string
		if cfg.TwilioAccountSID == "" {
			reasons = append(reasons, "TWILIO_ACCOUNT_SID missing")
		}
		if cfg.TwilioAuthToken == "" {
			reasons = append(reasons, "TWILIO_AUTH_TOKEN missing")
		}
		missing[SMSProviderTwilio] = strings.Join(reasons, ", ")
	}

	if preference != SMSProviderAuto {
		if preference == SMSProviderTelnyx && telnyxMessenger != nil {
			return telnyxMessenger, SMSProviderTelnyx, ""
		}
		if preference == SMSProviderTwilio && twilioMessenger != nil {
			return twilioMessenger, SMSProviderTwilio, ""
		}
		reason := missing[preference]
		if reason == "" {
			reason = fmt.Sprintf("%s messenger not configured", preference)
		}
		return nil, "", reason
	}

	if telnyxMessenger != nil && twilioMessenger != nil {
		return NewFailoverMessenger(telnyxMessenger, SMSProviderTelnyx, twilioMessenger, SMSProviderTwilio, logger), SMSProviderTelnyx + "+" + SMSProviderTwilio, ""
	}
	if telnyxMessenger != nil {
		return telnyxMessenger, SMSProviderTelnyx, ""
	}
	if twilioMessenger != nil {
		return twilioMessenger, SMSProviderTwilio, ""
	}

	var reasons []string
	order := resolvePreferredOrder(preference)
	for _, provider := range order {
		if msg := missing[provider]; msg != "" {
			reasons = append(reasons, fmt.Sprintf("%s: %s", provider, msg))
		}
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "no SMS providers configured")
	}
	return nil, "", strings.Join(reasons, "; ")
}

func resolvePreferredOrder(preference string) []string {
	switch preference {
	case SMSProviderTelnyx:
		return []string{SMSProviderTelnyx}
	case SMSProviderTwilio:
		return []string{SMSProviderTwilio}
	default:
		return []string{SMSProviderTelnyx, SMSProviderTwilio}
	}
}
