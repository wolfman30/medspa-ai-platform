// Package payments provides Stripe and Square payment processing.
package payments

const (
	// WebhookTimestampToleranceSec is the maximum age (in seconds) of a Stripe
	// webhook signature timestamp before it is rejected. Set to 5 minutes to
	// guard against replay attacks while tolerating minor clock skew.
	WebhookTimestampToleranceSec = 300

	// MaxOAuthLabelLen is the maximum character length for a truncated OAuth
	// account label displayed in the admin UI.
	MaxOAuthLabelLen = 180
)
