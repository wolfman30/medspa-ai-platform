package payments

import (
	"fmt"
	"strings"
	"time"
)

// SquareOAuthConfig holds configuration for Square OAuth flow.
type SquareOAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string // e.g., "https://api.aiwolfsolutions.com/oauth/square/callback"
	Sandbox      bool   // Use sandbox URLs if true
}

// SquareCredentials represents stored OAuth credentials for a clinic.
type SquareCredentials struct {
	OrgID          string
	MerchantID     string
	AccessToken    string
	RefreshToken   string
	TokenExpiresAt time.Time
	LocationID     string
	PhoneNumber    string // E.164 format, used as "from" number for SMS confirmations
	CreatedAt      time.Time
	UpdatedAt      time.Time
	// Refresh diagnostics (optional)
	LastRefreshAttemptAt *time.Time
	LastRefreshFailureAt *time.Time
	LastRefreshError     string
}

// squareTokenResponse represents the response from Square's token endpoint.
type squareTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresAt    string `json:"expires_at"` // ISO 8601 format
	MerchantID   string `json:"merchant_id"`
	RefreshToken string `json:"refresh_token"`
}

// squareLocationsResponse represents the response from Square's locations endpoint.
type squareLocationsResponse struct {
	Locations []squareLocation `json:"locations"`
}

// squareLocation represents a single Square merchant location.
type squareLocation struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"` // ACTIVE, INACTIVE
	Type     string `json:"type"`   // PHYSICAL, MOBILE
	Timezone string `json:"timezone"`
}

// ParseState extracts orgID and random state from the combined state parameter.
// The expected format is "orgID:randomState".
func ParseState(state string) (orgID, randomState string, err error) {
	parts := strings.SplitN(state, ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid state format")
	}
	return parts[0], parts[1], nil
}
