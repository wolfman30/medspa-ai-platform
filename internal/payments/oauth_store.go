package payments

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

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
		       COALESCE(location_id, '') as location_id, 
		       COALESCE(phone_number, '') as phone_number,
		       created_at, updated_at,
		       last_refresh_attempt_at,
		       last_refresh_failure_at,
		       COALESCE(last_refresh_error, '') as last_refresh_error
		FROM clinic_square_credentials
		WHERE org_id = $1
	`

	var creds SquareCredentials
	var lastAttempt pgtype.Timestamptz
	var lastFailure pgtype.Timestamptz
	var lastError string
	err := s.db.QueryRow(ctx, query, orgID).Scan(
		&creds.OrgID,
		&creds.MerchantID,
		&creds.AccessToken,
		&creds.RefreshToken,
		&creds.TokenExpiresAt,
		&creds.LocationID,
		&creds.PhoneNumber,
		&creds.CreatedAt,
		&creds.UpdatedAt,
		&lastAttempt,
		&lastFailure,
		&lastError,
	)
	if err != nil {
		return nil, fmt.Errorf("get square credentials: %w", err)
	}

	creds.LastRefreshAttemptAt = timestamptzToPtr(lastAttempt)
	creds.LastRefreshFailureAt = timestamptzToPtr(lastFailure)
	creds.LastRefreshError = lastError

	return &creds, nil
}

// GetExpiringCredentials retrieves all credentials expiring within the given duration.
func (s *SquareOAuthService) GetExpiringCredentials(ctx context.Context, within time.Duration) ([]SquareCredentials, error) {
	query := `
		SELECT org_id, merchant_id, access_token, refresh_token, token_expires_at,
		       COALESCE(location_id, '') as location_id,
		       COALESCE(phone_number, '') as phone_number,
		       created_at, updated_at,
		       last_refresh_attempt_at,
		       last_refresh_failure_at,
		       COALESCE(last_refresh_error, '') as last_refresh_error
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
		var lastAttempt pgtype.Timestamptz
		var lastFailure pgtype.Timestamptz
		var lastError string
		if err := rows.Scan(
			&creds.OrgID,
			&creds.MerchantID,
			&creds.AccessToken,
			&creds.RefreshToken,
			&creds.TokenExpiresAt,
			&creds.LocationID,
			&creds.PhoneNumber,
			&creds.CreatedAt,
			&creds.UpdatedAt,
			&lastAttempt,
			&lastFailure,
			&lastError,
		); err != nil {
			return nil, fmt.Errorf("scan credentials row: %w", err)
		}
		creds.LastRefreshAttemptAt = timestamptzToPtr(lastAttempt)
		creds.LastRefreshFailureAt = timestamptzToPtr(lastFailure)
		creds.LastRefreshError = lastError
		results = append(results, creds)
	}

	return results, nil
}

// RecordRefreshFailure stores the latest refresh failure metadata.
func (s *SquareOAuthService) RecordRefreshFailure(ctx context.Context, orgID string, err error) error {
	if s == nil || s.db == nil {
		return nil
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	query := `
		UPDATE clinic_square_credentials
		SET last_refresh_attempt_at = NOW(),
			last_refresh_failure_at = NOW(),
			last_refresh_error = NULLIF($2, ''),
			updated_at = NOW()
		WHERE org_id = $1
	`
	result, execErr := s.db.Exec(ctx, query, orgID, msg)
	if execErr != nil {
		return fmt.Errorf("record refresh failure: %w", execErr)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("no credentials found for org %s", orgID)
	}
	return nil
}

// RecordRefreshSuccess clears any stored refresh failure metadata.
func (s *SquareOAuthService) RecordRefreshSuccess(ctx context.Context, orgID string) error {
	if s == nil || s.db == nil {
		return nil
	}
	query := `
		UPDATE clinic_square_credentials
		SET last_refresh_attempt_at = NOW(),
			last_refresh_failure_at = NULL,
			last_refresh_error = NULL,
			updated_at = NOW()
		WHERE org_id = $1
	`
	result, execErr := s.db.Exec(ctx, query, orgID)
	if execErr != nil {
		return fmt.Errorf("record refresh success: %w", execErr)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("no credentials found for org %s", orgID)
	}
	return nil
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

// UpdatePhoneNumber sets the SMS from number for a clinic.
func (s *SquareOAuthService) UpdatePhoneNumber(ctx context.Context, orgID, phoneNumber string) error {
	query := `UPDATE clinic_square_credentials SET phone_number = $2, updated_at = NOW() WHERE org_id = $1`
	result, err := s.db.Exec(ctx, query, orgID, phoneNumber)
	if err != nil {
		return fmt.Errorf("update phone number: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("no credentials found for org %s", orgID)
	}
	s.logger.Info("updated clinic phone number", "org_id", orgID, "phone", phoneNumber)
	return nil
}

// GetPhoneNumber retrieves the SMS from number for a clinic.
// Returns empty string if not found or not configured.
func (s *SquareOAuthService) GetPhoneNumber(ctx context.Context, orgID string) (string, error) {
	query := `SELECT COALESCE(phone_number, '') FROM clinic_square_credentials WHERE org_id = $1`
	var phone string
	err := s.db.QueryRow(ctx, query, orgID).Scan(&phone)
	if err != nil {
		return "", fmt.Errorf("get phone number: %w", err)
	}
	return phone, nil
}
