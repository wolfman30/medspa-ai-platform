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
)

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
		detail := formatSquareError(body)
		s.logger.Error("square token exchange failed", "status", resp.StatusCode, "body", string(body))
		if detail != "" {
			return nil, fmt.Errorf("token exchange failed: status %d: %s", resp.StatusCode, detail)
		}
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
		detail := formatSquareError(body)
		s.logger.Error("square token refresh failed", "status", resp.StatusCode, "body", string(body))
		if detail != "" {
			return nil, fmt.Errorf("token refresh failed: status %d: %s", resp.StatusCode, detail)
		}
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
