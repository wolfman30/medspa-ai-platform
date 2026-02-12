package payments

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// StripeConnectHandler manages the Stripe Connect OAuth flow for clinic onboarding.
// Clinics link their existing Stripe account so deposits go directly to them.
type StripeConnectHandler struct {
	clientID    string
	secretKey   string
	baseURL     string // Stripe API base URL
	redirectURI string // Our callback URL
	httpClient  *http.Client
	logger      *logging.Logger
	configSaver StripeConfigSaver
	dryRun      bool
}

// StripeConfigSaver persists the connected Stripe account ID back to clinic config.
type StripeConfigSaver interface {
	SaveStripeAccountID(ctx context.Context, orgID, accountID string) error
}

// NewStripeConnectHandler creates a handler for the Stripe Connect OAuth flow.
func NewStripeConnectHandler(clientID, secretKey, redirectURI string, configSaver StripeConfigSaver, logger *logging.Logger) *StripeConnectHandler {
	if logger == nil {
		logger = logging.Default()
	}
	dryRun := strings.EqualFold(os.Getenv("STRIPE_DRY_RUN"), "true") || os.Getenv("STRIPE_DRY_RUN") == "1"
	return &StripeConnectHandler{
		clientID:    clientID,
		secretKey:   secretKey,
		baseURL:     "https://api.stripe.com",
		redirectURI: redirectURI,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		logger:      logger,
		configSaver: configSaver,
		dryRun:      dryRun,
	}
}

// WithBaseURL overrides the Stripe API URL (for testing).
func (h *StripeConnectHandler) WithBaseURL(baseURL string) *StripeConnectHandler {
	if baseURL != "" {
		h.baseURL = strings.TrimRight(baseURL, "/")
	}
	return h
}

// HandleAuthorize redirects the clinic to Stripe's Connect authorization page.
// GET /stripe/connect?org_id=<org_id>
func (h *StripeConnectHandler) HandleAuthorize(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")
	if orgID == "" {
		http.Error(w, "org_id is required", http.StatusBadRequest)
		return
	}

	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", h.clientID)
	params.Set("scope", "read_write")
	params.Set("redirect_uri", h.redirectURI)
	params.Set("state", orgID) // Pass org_id as state for the callback
	params.Set("stripe_landing", "login")

	authorizeURL := "https://connect.stripe.com/oauth/authorize?" + params.Encode()
	http.Redirect(w, r, authorizeURL, http.StatusTemporaryRedirect)
}

// HandleCallback handles the OAuth callback from Stripe Connect.
// GET /stripe/connect/callback?code=<auth_code>&state=<org_id>
func (h *StripeConnectHandler) HandleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	orgID := r.URL.Query().Get("state")
	errParam := r.URL.Query().Get("error")

	if errParam != "" {
		errDesc := r.URL.Query().Get("error_description")
		h.logger.Error("stripe connect authorization denied", "error", errParam, "description", errDesc, "org_id", orgID)
		http.Error(w, fmt.Sprintf("Authorization denied: %s", errDesc), http.StatusBadRequest)
		return
	}

	if code == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}
	if orgID == "" {
		http.Error(w, "missing state (org_id)", http.StatusBadRequest)
		return
	}

	if h.dryRun {
		h.logger.Info("stripe connect dry run: skipping token exchange", "org_id", orgID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":     "dry_run",
			"org_id":     orgID,
			"account_id": "acct_dryrun_placeholder",
		})
		return
	}

	// Exchange authorization code for connected account ID
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)

	apiURL := h.baseURL + "/v1/oauth/token"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		h.logger.Error("stripe connect request build failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Authorization", "Bearer "+h.secretKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		h.logger.Error("stripe connect token exchange failed", "error", err)
		http.Error(w, "failed to connect Stripe account", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		h.logger.Error("stripe connect token exchange error", "status", resp.StatusCode, "body", string(body))
		http.Error(w, "Stripe account connection failed", http.StatusBadGateway)
		return
	}

	var tokenResp struct {
		StripeUserID string `json:"stripe_user_id"`
		AccessToken  string `json:"access_token"`
		Scope        string `json:"scope"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		h.logger.Error("stripe connect response decode failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if tokenResp.StripeUserID == "" {
		h.logger.Error("stripe connect response missing stripe_user_id")
		http.Error(w, "invalid Stripe response", http.StatusBadGateway)
		return
	}

	// Save the connected account ID
	if h.configSaver != nil {
		if err := h.configSaver.SaveStripeAccountID(r.Context(), orgID, tokenResp.StripeUserID); err != nil {
			h.logger.Error("failed to save stripe account id", "error", err, "org_id", orgID, "account_id", tokenResp.StripeUserID)
			http.Error(w, "failed to save connection", http.StatusInternalServerError)
			return
		}
	}

	h.logger.Info("stripe connect account linked", "org_id", orgID, "stripe_account_id", tokenResp.StripeUserID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":     "connected",
		"org_id":     orgID,
		"account_id": tokenResp.StripeUserID,
	})
}

// HandleStatus returns the Stripe Connect status for a clinic.
// GET /admin/clinics/{orgID}/stripe/status
func (h *StripeConnectHandler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		http.Error(w, "missing orgID", http.StatusBadRequest)
		return
	}

	// Check if configSaver also implements StripeStatusGetter (clinic.Store does)
	type stripeStatusGetter interface {
		GetStripeAccountID(ctx context.Context, orgID string) (string, error)
	}
	if getter, ok := h.configSaver.(stripeStatusGetter); ok {
		accountID, err := getter.GetStripeAccountID(r.Context(), orgID)
		if err != nil {
			h.logger.Error("failed to get stripe status", "error", err, "org_id", orgID)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"org_id":            orgID,
			"connected":         accountID != "",
			"stripe_account_id": accountID,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"org_id":    orgID,
		"connected": false,
		"message":   "status check not available",
	})
}
