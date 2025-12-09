package payments

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// OAuthHandler handles Square OAuth HTTP endpoints.
type OAuthHandler struct {
	oauthService *SquareOAuthService
	logger       *logging.Logger
	successURL   string                // URL to redirect to after successful OAuth
	stateStore   map[string]stateEntry // In-memory state store (use Redis in production)
}

type stateEntry struct {
	orgID     string
	expiresAt time.Time
}

// NewOAuthHandler creates a new OAuth HTTP handler.
func NewOAuthHandler(oauthService *SquareOAuthService, successURL string, logger *logging.Logger) *OAuthHandler {
	if logger == nil {
		logger = logging.Default()
	}
	return &OAuthHandler{
		oauthService: oauthService,
		logger:       logger,
		successURL:   successURL,
		stateStore:   make(map[string]stateEntry),
	}
}

// Routes returns a chi router with OAuth routes.
func (h *OAuthHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/square/callback", h.HandleCallback)
	return r
}

// AdminRoutes returns routes that require admin authentication.
func (h *OAuthHandler) AdminRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/{orgID}/square/connect", h.HandleConnect)
	r.Get("/{orgID}/square/status", h.HandleStatus)
	r.Delete("/{orgID}/square/disconnect", h.HandleDisconnect)
	r.Post("/{orgID}/square/sync-location", h.HandleSyncLocation)
	r.Put("/{orgID}/phone", h.HandleUpdatePhone)
	return r
}

// HandleConnect initiates the Square OAuth flow for a clinic.
// GET /admin/clinics/{orgID}/square/connect
func (h *OAuthHandler) HandleConnect(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		http.Error(w, `{"error": "org_id required"}`, http.StatusBadRequest)
		return
	}

	// Generate random state for CSRF protection
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		h.logger.Error("failed to generate state", "error", err)
		http.Error(w, `{"error": "internal server error"}`, http.StatusInternalServerError)
		return
	}
	randomState := hex.EncodeToString(stateBytes)

	// Store state for verification on callback (expires in 10 minutes)
	h.stateStore[randomState] = stateEntry{
		orgID:     orgID,
		expiresAt: time.Now().Add(10 * time.Minute),
	}

	// Clean up expired states
	h.cleanExpiredStates()

	// Generate authorization URL and redirect
	authURL := h.oauthService.AuthorizationURL(orgID, randomState)

	h.logger.Info("initiating square oauth", "org_id", orgID)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// HandleCallback handles the OAuth callback from Square.
// GET /oauth/square/callback?code=...&state=...
func (h *OAuthHandler) HandleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	errorParam := r.URL.Query().Get("error")
	errorDesc := r.URL.Query().Get("error_description")

	// Handle error response from Square
	if errorParam != "" {
		h.logger.Error("square oauth error", "error", errorParam, "description", errorDesc)
		http.Error(w, `{"error": "`+errorParam+`", "description": "`+errorDesc+`"}`, http.StatusBadRequest)
		return
	}

	if code == "" || state == "" {
		http.Error(w, `{"error": "missing code or state"}`, http.StatusBadRequest)
		return
	}

	// Parse state to get orgID
	orgID, randomState, err := ParseState(state)
	if err != nil {
		h.logger.Error("invalid state format", "state", state, "error", err)
		http.Error(w, `{"error": "invalid state"}`, http.StatusBadRequest)
		return
	}

	// Verify state exists and hasn't expired
	entry, ok := h.stateStore[randomState]
	if !ok {
		h.logger.Error("state not found", "state", randomState)
		http.Error(w, `{"error": "invalid or expired state"}`, http.StatusBadRequest)
		return
	}

	if time.Now().After(entry.expiresAt) {
		delete(h.stateStore, randomState)
		h.logger.Error("state expired", "state", randomState)
		http.Error(w, `{"error": "state expired"}`, http.StatusBadRequest)
		return
	}

	// Verify orgID matches
	if entry.orgID != orgID {
		h.logger.Error("org_id mismatch", "expected", entry.orgID, "got", orgID)
		http.Error(w, `{"error": "state mismatch"}`, http.StatusBadRequest)
		return
	}

	// Clean up used state
	delete(h.stateStore, randomState)

	// Exchange code for tokens
	creds, err := h.oauthService.ExchangeCode(r.Context(), code)
	if err != nil {
		h.logger.Error("token exchange failed", "org_id", orgID, "error", err)
		http.Error(w, `{"error": "token exchange failed"}`, http.StatusInternalServerError)
		return
	}

	// Save credentials
	if err := h.oauthService.SaveCredentials(r.Context(), orgID, creds); err != nil {
		h.logger.Error("save credentials failed", "org_id", orgID, "error", err)
		http.Error(w, `{"error": "failed to save credentials"}`, http.StatusInternalServerError)
		return
	}

	// Fetch and save the default location ID
	locationID, err := h.oauthService.GetDefaultLocation(r.Context(), creds.AccessToken)
	if err != nil {
		h.logger.Warn("failed to fetch location, payments may not work", "org_id", orgID, "error", err)
	} else if locationID != "" {
		if err := h.oauthService.UpdateLocationID(r.Context(), orgID, locationID); err != nil {
			h.logger.Warn("failed to save location_id", "org_id", orgID, "error", err)
		} else {
			h.logger.Info("saved square location", "org_id", orgID, "location_id", locationID)
		}
	}

	h.logger.Info("square oauth completed", "org_id", orgID, "merchant_id", creds.MerchantID, "location_id", locationID)

	// Redirect to success URL or return JSON
	if h.successURL != "" {
		successURL := strings.Replace(h.successURL, "{org_id}", orgID, 1)
		http.Redirect(w, r, successURL, http.StatusFound)
		return
	}

	// Return JSON response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"org_id":      orgID,
		"merchant_id": creds.MerchantID,
		"location_id": locationID,
		"message":     "Square account connected successfully",
	})
}

// HandleStatus returns the Square connection status for a clinic.
// GET /admin/clinics/{orgID}/square/status
func (h *OAuthHandler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		http.Error(w, `{"error": "org_id required"}`, http.StatusBadRequest)
		return
	}

	creds, err := h.oauthService.GetCredentials(r.Context(), orgID)
	if err != nil {
		// No credentials found = not connected
		if strings.Contains(err.Error(), "no rows") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"connected": false,
				"org_id":    orgID,
			})
			return
		}
		h.logger.Error("get credentials failed", "org_id", orgID, "error", err)
		http.Error(w, `{"error": "internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"connected":        true,
		"org_id":           orgID,
		"merchant_id":      creds.MerchantID,
		"location_id":      creds.LocationID,
		"phone_number":     creds.PhoneNumber,
		"token_expires_at": creds.TokenExpiresAt,
		"connected_at":     creds.CreatedAt,
	})
}

// HandleDisconnect removes Square credentials for a clinic.
// DELETE /admin/clinics/{orgID}/square/disconnect
func (h *OAuthHandler) HandleDisconnect(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		http.Error(w, `{"error": "org_id required"}`, http.StatusBadRequest)
		return
	}

	if err := h.oauthService.DeleteCredentials(r.Context(), orgID); err != nil {
		h.logger.Error("delete credentials failed", "org_id", orgID, "error", err)
		http.Error(w, `{"error": "internal server error"}`, http.StatusInternalServerError)
		return
	}

	h.logger.Info("square disconnected", "org_id", orgID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"org_id":  orgID,
		"message": "Square account disconnected",
	})
}

// HandleSyncLocation fetches and updates the location ID for a clinic.
// POST /admin/clinics/{orgID}/square/sync-location
func (h *OAuthHandler) HandleSyncLocation(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		http.Error(w, `{"error": "org_id required"}`, http.StatusBadRequest)
		return
	}

	// Get current credentials
	creds, err := h.oauthService.GetCredentials(r.Context(), orgID)
	if err != nil {
		h.logger.Error("get credentials failed", "org_id", orgID, "error", err)
		http.Error(w, `{"error": "not connected to Square"}`, http.StatusNotFound)
		return
	}

	// Fetch location from Square
	locationID, err := h.oauthService.GetDefaultLocation(r.Context(), creds.AccessToken)
	if err != nil {
		h.logger.Error("fetch location failed", "org_id", orgID, "error", err)
		http.Error(w, `{"error": "failed to fetch location from Square"}`, http.StatusInternalServerError)
		return
	}

	// Update location in database
	if err := h.oauthService.UpdateLocationID(r.Context(), orgID, locationID); err != nil {
		h.logger.Error("update location failed", "org_id", orgID, "error", err)
		http.Error(w, `{"error": "failed to save location"}`, http.StatusInternalServerError)
		return
	}

	h.logger.Info("synced square location", "org_id", orgID, "location_id", locationID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"org_id":      orgID,
		"location_id": locationID,
		"message":     "Location synced successfully",
	})
}

// HandleUpdatePhone updates the SMS from number for a clinic.
// PUT /admin/clinics/{orgID}/phone
func (h *OAuthHandler) HandleUpdatePhone(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		http.Error(w, `{"error": "org_id required"}`, http.StatusBadRequest)
		return
	}

	var req struct {
		PhoneNumber string `json:"phone_number"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error": "invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.PhoneNumber == "" {
		http.Error(w, `{"error": "phone_number required"}`, http.StatusBadRequest)
		return
	}

	// Normalize phone number to E.164 format if not already
	phone := req.PhoneNumber
	if !strings.HasPrefix(phone, "+") {
		phone = "+" + phone
	}

	if err := h.oauthService.UpdatePhoneNumber(r.Context(), orgID, phone); err != nil {
		if strings.Contains(err.Error(), "no credentials found") {
			http.Error(w, `{"error": "clinic not found or Square not connected"}`, http.StatusNotFound)
			return
		}
		h.logger.Error("update phone failed", "org_id", orgID, "error", err)
		http.Error(w, `{"error": "failed to update phone number"}`, http.StatusInternalServerError)
		return
	}

	h.logger.Info("updated clinic phone", "org_id", orgID, "phone", phone)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"org_id":       orgID,
		"phone_number": phone,
		"message":      "Phone number updated successfully",
	})
}

// cleanExpiredStates removes expired state entries.
func (h *OAuthHandler) cleanExpiredStates() {
	now := time.Now()
	for state, entry := range h.stateStore {
		if now.After(entry.expiresAt) {
			delete(h.stateStore, state)
		}
	}
}
