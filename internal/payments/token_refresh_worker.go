package payments

import (
	"context"
	"time"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// TokenRefreshWorker periodically refreshes Square OAuth tokens before they expire.
type TokenRefreshWorker struct {
	oauthService *SquareOAuthService
	logger       *logging.Logger
	interval     time.Duration
	refreshBefore time.Duration // Refresh tokens this long before they expire
}

// NewTokenRefreshWorker creates a new token refresh worker.
func NewTokenRefreshWorker(oauthService *SquareOAuthService, logger *logging.Logger) *TokenRefreshWorker {
	if logger == nil {
		logger = logging.Default()
	}
	return &TokenRefreshWorker{
		oauthService:  oauthService,
		logger:        logger,
		interval:      1 * time.Hour,       // Check every hour
		refreshBefore: 7 * 24 * time.Hour,  // Refresh 7 days before expiry
	}
}

// WithInterval sets the check interval.
func (w *TokenRefreshWorker) WithInterval(interval time.Duration) *TokenRefreshWorker {
	w.interval = interval
	return w
}

// WithRefreshBefore sets how long before expiry to refresh.
func (w *TokenRefreshWorker) WithRefreshBefore(d time.Duration) *TokenRefreshWorker {
	w.refreshBefore = d
	return w
}

// Start runs the token refresh worker. Blocks until context is cancelled.
func (w *TokenRefreshWorker) Start(ctx context.Context) {
	w.logger.Info("starting square token refresh worker",
		"interval", w.interval.String(),
		"refresh_before", w.refreshBefore.String(),
	)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// Run once immediately on startup
	w.refreshExpiringTokens(ctx)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("token refresh worker shutting down")
			return
		case <-ticker.C:
			w.refreshExpiringTokens(ctx)
		}
	}
}

// refreshExpiringTokens finds and refreshes all tokens expiring within refreshBefore.
func (w *TokenRefreshWorker) refreshExpiringTokens(ctx context.Context) {
	creds, err := w.oauthService.GetExpiringCredentials(ctx, w.refreshBefore)
	if err != nil {
		w.logger.Error("failed to get expiring credentials", "error", err)
		return
	}

	if len(creds) == 0 {
		w.logger.Debug("no tokens need refresh")
		return
	}

	w.logger.Info("refreshing expiring square tokens", "count", len(creds))

	for _, cred := range creds {
		if err := w.refreshToken(ctx, cred); err != nil {
			w.logger.Error("failed to refresh token",
				"org_id", cred.OrgID,
				"merchant_id", cred.MerchantID,
				"error", err,
			)
			continue
		}
		w.logger.Info("refreshed square token",
			"org_id", cred.OrgID,
			"merchant_id", cred.MerchantID,
		)
	}
}

// refreshToken refreshes a single token and saves the new credentials.
func (w *TokenRefreshWorker) refreshToken(ctx context.Context, cred SquareCredentials) error {
	newCreds, err := w.oauthService.RefreshToken(ctx, cred.RefreshToken)
	if err != nil {
		return err
	}

	// Preserve the location ID from the original credentials
	newCreds.LocationID = cred.LocationID

	return w.oauthService.SaveCredentials(ctx, cred.OrgID, newCreds)
}

// RunOnce performs a single refresh check. Useful for testing or manual triggers.
func (w *TokenRefreshWorker) RunOnce(ctx context.Context) error {
	w.refreshExpiringTokens(ctx)
	return nil
}
