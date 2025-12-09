package payments

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
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
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// SquareOAuthService handles Square OAuth operations.
type SquareOAuthService struct {
	config SquareOAuthConfig
	db     *pgxpool.Pool
	logger *logging.Logger
}

// NewSquareOAuthService creates a new Square OAuth service.
func NewSquareOAuthService(config SquareOAuthConfig, db *pgxpool.Pool, logger *logging.Logger) *SquareOAuthService {
	if logger == nil {
		logger = logging.Default()
	}
	return &SquareOAuthService{
		config: config,
		db:     db,
		logger: logger,
	}
}

// baseURL returns the Square API base URL based on environment.
func (s *SquareOAuthService) baseURL() string {
	if s.config.Sandbox {
		return "https://connect.squareupsandbox.com"
	}
	return "https://connect.squareup.com"
}

// AuthorizationURL generates the URL to redirect clinic admins to for OAuth authorization.
// state should be a unique, unguessable string tied to the user session to prevent CSRF.
func (s *SquareOAuthService) AuthorizationURL(orgID, state string) string {
	params := url.Values{
		"client_id":    {s.config.ClientID},
		"scope":        {"PAYMENTS_WRITE PAYMENTS_READ ORDERS_WRITE ORDERS_READ MERCHANT_PROFILE_READ"},
		"state":        {state},
		"redirect_uri": {s.config.RedirectURI},
	}

	// Note: session=false is not supported in Sandbox environment
	if !s.config.Sandbox {
		params.Set("session", "false")
	}

	// Encode org_id in state for retrieval on callback
	// Format: orgID:randomState
	encodedState := fmt.Sprintf("%s:%s", orgID, state)
	params.Set("state", encodedState)

	return fmt.Sprintf("%s/oauth2/authorize?%s", s.baseURL(), params.Encode())
}

// squareTokenResponse represents the response from Square's token endpoint.
type squareTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresAt    string `json:"expires_at"` // ISO 8601 format
	MerchantID   string `json:"merchant_id"`
	RefreshToken string `json:"refresh_token"`
}

// ExchangeCode exchanges an authorization code for access and refresh tokens.
func (s *SquareOAuthService) ExchangeCode(ctx context.Context, code string) (*SquareCredentials, error) {
	tokenURL := fmt.Sprintf("%s/oauth2/token", s.baseURL())

	data := url.Values{
		"client_id":     {s.config.ClientID},
		"client_secret": {s.config.ClientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {s.config.RedirectURI},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Square-Version", "2024-01-18")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		s.logger.Error("square token exchange failed", "status", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("token exchange failed: status %d", resp.StatusCode)
	}

	var tokenResp squareTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	expiresAt, err := time.Parse(time.RFC3339, tokenResp.ExpiresAt)
	if err != nil {
		// Default to 30 days if parsing fails
		expiresAt = time.Now().Add(30 * 24 * time.Hour)
	}

	return &SquareCredentials{
		MerchantID:     tokenResp.MerchantID,
		AccessToken:    tokenResp.AccessToken,
		RefreshToken:   tokenResp.RefreshToken,
		TokenExpiresAt: expiresAt,
	}, nil
}

// RefreshToken refreshes an expired or expiring access token.
func (s *SquareOAuthService) RefreshToken(ctx context.Context, refreshToken string) (*SquareCredentials, error) {
	tokenURL := fmt.Sprintf("%s/oauth2/token", s.baseURL())

	data := url.Values{
		"client_id":     {s.config.ClientID},
		"client_secret": {s.config.ClientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Square-Version", "2024-01-18")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		s.logger.Error("square token refresh failed", "status", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("token refresh failed: status %d", resp.StatusCode)
	}

	var tokenResp squareTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	expiresAt, err := time.Parse(time.RFC3339, tokenResp.ExpiresAt)
	if err != nil {
		expiresAt = time.Now().Add(30 * 24 * time.Hour)
	}

	return &SquareCredentials{
		MerchantID:     tokenResp.MerchantID,
		AccessToken:    tokenResp.AccessToken,
		RefreshToken:   tokenResp.RefreshToken,
		TokenExpiresAt: expiresAt,
	}, nil
}

// SaveCredentials stores or updates Square credentials for a clinic.
func (s *SquareOAuthService) SaveCredentials(ctx context.Context, orgID string, creds *SquareCredentials) error {
	query := `
		INSERT INTO clinic_square_credentials (
			org_id, merchant_id, access_token, refresh_token, token_expires_at, location_id, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (org_id) DO UPDATE SET
			merchant_id = EXCLUDED.merchant_id,
			access_token = EXCLUDED.access_token,
			refresh_token = EXCLUDED.refresh_token,
			token_expires_at = EXCLUDED.token_expires_at,
			location_id = COALESCE(EXCLUDED.location_id, clinic_square_credentials.location_id),
			updated_at = NOW()
	`

	_, err := s.db.Exec(ctx, query,
		orgID,
		creds.MerchantID,
		creds.AccessToken,
		creds.RefreshToken,
		creds.TokenExpiresAt,
		creds.LocationID,
	)
	if err != nil {
		return fmt.Errorf("save square credentials: %w", err)
	}

	s.logger.Info("saved square credentials", "org_id", orgID, "merchant_id", creds.MerchantID)
	return nil
}

// GetCredentials retrieves Square credentials for a clinic.
func (s *SquareOAuthService) GetCredentials(ctx context.Context, orgID string) (*SquareCredentials, error) {
	query := `
		SELECT org_id, merchant_id, access_token, refresh_token, token_expires_at, 
		       COALESCE(location_id, '') as location_id, created_at, updated_at
		FROM clinic_square_credentials
		WHERE org_id = $1
	`

	var creds SquareCredentials
	err := s.db.QueryRow(ctx, query, orgID).Scan(
		&creds.OrgID,
		&creds.MerchantID,
		&creds.AccessToken,
		&creds.RefreshToken,
		&creds.TokenExpiresAt,
		&creds.LocationID,
		&creds.CreatedAt,
		&creds.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get square credentials: %w", err)
	}

	return &creds, nil
}

// GetExpiringCredentials retrieves all credentials expiring within the given duration.
func (s *SquareOAuthService) GetExpiringCredentials(ctx context.Context, within time.Duration) ([]SquareCredentials, error) {
	query := `
		SELECT org_id, merchant_id, access_token, refresh_token, token_expires_at,
		       COALESCE(location_id, '') as location_id, created_at, updated_at
		FROM clinic_square_credentials
		WHERE token_expires_at < $1
		ORDER BY token_expires_at ASC
	`

	expiryThreshold := time.Now().Add(within)
	rows, err := s.db.Query(ctx, query, expiryThreshold)
	if err != nil {
		return nil, fmt.Errorf("query expiring credentials: %w", err)
	}
	defer rows.Close()

	var results []SquareCredentials
	for rows.Next() {
		var creds SquareCredentials
		if err := rows.Scan(
			&creds.OrgID,
			&creds.MerchantID,
			&creds.AccessToken,
			&creds.RefreshToken,
			&creds.TokenExpiresAt,
			&creds.LocationID,
			&creds.CreatedAt,
			&creds.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan credentials row: %w", err)
		}
		results = append(results, creds)
	}

	return results, nil
}

// DeleteCredentials removes Square credentials for a clinic (for disconnection).
func (s *SquareOAuthService) DeleteCredentials(ctx context.Context, orgID string) error {
	query := `DELETE FROM clinic_square_credentials WHERE org_id = $1`
	_, err := s.db.Exec(ctx, query, orgID)
	if err != nil {
		return fmt.Errorf("delete square credentials: %w", err)
	}
	s.logger.Info("deleted square credentials", "org_id", orgID)
	return nil
}

// UpdateLocationID sets the default location ID for a clinic's Square account.
func (s *SquareOAuthService) UpdateLocationID(ctx context.Context, orgID, locationID string) error {
	query := `UPDATE clinic_square_credentials SET location_id = $2, updated_at = NOW() WHERE org_id = $1`
	result, err := s.db.Exec(ctx, query, orgID, locationID)
	if err != nil {
		return fmt.Errorf("update location id: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("no credentials found for org %s", orgID)
	}
	return nil
}

// ParseState extracts orgID and random state from the combined state parameter.
func ParseState(state string) (orgID, randomState string, err error) {
	parts := strings.SplitN(state, ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid state format")
	}
	return parts[0], parts[1], nil
}

// squareLocationsResponse represents the response from Square's locations endpoint.
type squareLocationsResponse struct {
	Locations []squareLocation `json:"locations"`
}

type squareLocation struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"` // ACTIVE, INACTIVE
	Type     string `json:"type"`   // PHYSICAL, MOBILE
	Timezone string `json:"timezone"`
}

// FetchLocations retrieves all locations for a merchant from Square.
func (s *SquareOAuthService) FetchLocations(ctx context.Context, accessToken string) ([]squareLocation, error) {
	locationsURL := fmt.Sprintf("%s/v2/locations", s.baseURL())

	req, err := http.NewRequestWithContext(ctx, "GET", locationsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create locations request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Square-Version", "2024-01-18")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("locations request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read locations response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		s.logger.Error("square locations fetch failed", "status", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("locations fetch failed: status %d", resp.StatusCode)
	}

	var locResp squareLocationsResponse
	if err := json.Unmarshal(body, &locResp); err != nil {
		return nil, fmt.Errorf("parse locations response: %w", err)
	}

	return locResp.Locations, nil
}

// GetDefaultLocation returns the first active location for a merchant.
func (s *SquareOAuthService) GetDefaultLocation(ctx context.Context, accessToken string) (string, error) {
	locations, err := s.FetchLocations(ctx, accessToken)
	if err != nil {
		return "", err
	}

	// Return the first ACTIVE location
	for _, loc := range locations {
		if loc.Status == "ACTIVE" {
			s.logger.Info("found active square location", "location_id", loc.ID, "name", loc.Name)
			return loc.ID, nil
		}
	}

	// If no active, return first available
	if len(locations) > 0 {
		s.logger.Warn("no active square locations, using first available", "location_id", locations[0].ID)
		return locations[0].ID, nil
	}

	return "", fmt.Errorf("no locations found for merchant")
}
