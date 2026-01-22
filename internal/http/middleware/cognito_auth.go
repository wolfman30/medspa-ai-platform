package middleware

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// CognitoConfig holds AWS Cognito configuration for JWT validation.
type CognitoConfig struct {
	Region     string
	UserPoolID string
	ClientID   string // App client ID for audience validation
}

// CognitoClaims represents the claims in a Cognito JWT.
type CognitoClaims struct {
	jwt.RegisteredClaims
	Email           string   `json:"email"`
	EmailVerified   bool     `json:"email_verified"`
	CognitoGroups   []string `json:"cognito:groups"`
	TokenUse        string   `json:"token_use"`
	ClientID        string   `json:"client_id"`
	Username        string   `json:"username"`
	CognitoUsername string   `json:"cognito:username"`
}

const cognitoClaimsKey contextKey = "cognitoClaims"

// jwksCache caches the JWKS keys from Cognito.
type jwksCache struct {
	mu      sync.RWMutex
	keys    map[string]*rsa.PublicKey
	expires time.Time
	issuer  string
}

var jwksCaches = make(map[string]*jwksCache)
var jwksCachesMu sync.RWMutex

// CognitoJWT validates JWTs issued by AWS Cognito.
// It accepts both ID tokens and access tokens.
func CognitoJWT(cfg CognitoConfig) func(http.Handler) http.Handler {
	if cfg.Region == "" || cfg.UserPoolID == "" {
		// Return a middleware that rejects all requests if not configured
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, `{"error":"cognito auth not configured"}`, http.StatusUnauthorized)
			})
		}
	}

	issuer := fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", cfg.Region, cfg.UserPoolID)
	jwksURL := fmt.Sprintf("%s/.well-known/jwks.json", issuer)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
				http.Error(w, `{"error":"missing authorization header"}`, http.StatusUnauthorized)
				return
			}

			tokenString := strings.TrimPrefix(auth, "Bearer ")

			// Parse the token header to get the key ID
			token, _, err := jwt.NewParser().ParseUnverified(tokenString, &CognitoClaims{})
			if err != nil {
				http.Error(w, `{"error":"invalid token format"}`, http.StatusUnauthorized)
				return
			}

			kid, ok := token.Header["kid"].(string)
			if !ok {
				http.Error(w, `{"error":"missing key id in token"}`, http.StatusUnauthorized)
				return
			}

			// Get the public key from JWKS
			pubKey, err := getPublicKey(jwksURL, kid, issuer)
			if err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"failed to get public key: %s"}`, err.Error()), http.StatusUnauthorized)
				return
			}

			// Parse and validate the token
			claims := &CognitoClaims{}
			validatedToken, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
				}
				return pubKey, nil
			}, jwt.WithIssuer(issuer), jwt.WithExpirationRequired())

			if err != nil || !validatedToken.Valid {
				http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
				return
			}

			// Validate audience/client_id for ID tokens
			if cfg.ClientID != "" && claims.TokenUse == "id" {
				aud, _ := claims.GetAudience()
				validAud := false
				for _, a := range aud {
					if a == cfg.ClientID {
						validAud = true
						break
					}
				}
				if !validAud {
					http.Error(w, `{"error":"invalid audience"}`, http.StatusUnauthorized)
					return
				}
			}

			// For access tokens, validate client_id claim
			if claims.TokenUse == "access" && cfg.ClientID != "" {
				if claims.ClientID != cfg.ClientID {
					http.Error(w, `{"error":"invalid client_id"}`, http.StatusUnauthorized)
					return
				}
			}

			ctx := context.WithValue(r.Context(), cognitoClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CognitoClaimsFromContext retrieves Cognito claims from the request context.
func CognitoClaimsFromContext(ctx context.Context) (*CognitoClaims, bool) {
	claims, ok := ctx.Value(cognitoClaimsKey).(*CognitoClaims)
	return claims, ok
}

// getPublicKey fetches and caches the public key from Cognito JWKS.
func getPublicKey(jwksURL, kid, issuer string) (*rsa.PublicKey, error) {
	jwksCachesMu.RLock()
	cache, exists := jwksCaches[issuer]
	jwksCachesMu.RUnlock()

	if exists {
		cache.mu.RLock()
		if time.Now().Before(cache.expires) {
			if key, ok := cache.keys[kid]; ok {
				cache.mu.RUnlock()
				return key, nil
			}
		}
		cache.mu.RUnlock()
	}

	// Fetch fresh JWKS
	keys, err := fetchJWKS(jwksURL)
	if err != nil {
		return nil, err
	}

	// Update cache
	jwksCachesMu.Lock()
	jwksCaches[issuer] = &jwksCache{
		keys:    keys,
		expires: time.Now().Add(1 * time.Hour),
		issuer:  issuer,
	}
	jwksCachesMu.Unlock()

	key, ok := keys[kid]
	if !ok {
		return nil, fmt.Errorf("key %s not found in JWKS", kid)
	}
	return key, nil
}

// jwksResponse represents the JWKS response from Cognito.
type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// fetchJWKS fetches the JWKS from the given URL.
func fetchJWKS(url string) (map[string]*rsa.PublicKey, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS request failed with status %d", resp.StatusCode)
	}

	var jwks jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("failed to decode JWKS: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey)
	for _, key := range jwks.Keys {
		if key.Kty != "RSA" {
			continue
		}

		pubKey, err := parseRSAPublicKey(key.N, key.E)
		if err != nil {
			continue
		}
		keys[key.Kid] = pubKey
	}

	if len(keys) == 0 {
		return nil, fmt.Errorf("no valid RSA keys found in JWKS")
	}

	return keys, nil
}

// parseRSAPublicKey parses RSA public key components from base64url-encoded strings.
func parseRSAPublicKey(nStr, eStr string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode modulus: %w", err)
	}

	eBytes, err := base64.RawURLEncoding.DecodeString(eStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode exponent: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := 0
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}

	return &rsa.PublicKey{N: n, E: e}, nil
}

// CognitoOrAdminJWT allows either Cognito JWT or legacy admin JWT.
// This enables gradual migration from the old auth system.
func CognitoOrAdminJWT(cognitoCfg CognitoConfig, adminSecret string) func(http.Handler) http.Handler {
	cognitoMW := CognitoJWT(cognitoCfg)
	adminMW := AdminJWT(adminSecret)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
				http.Error(w, `{"error":"missing authorization header"}`, http.StatusUnauthorized)
				return
			}

			tokenString := strings.TrimPrefix(auth, "Bearer ")

			// Try to determine if it's a Cognito JWT (has 3 parts, starts with eyJ)
			// Cognito tokens are larger and have specific structure
			parts := strings.Split(tokenString, ".")
			if len(parts) == 3 {
				// Try parsing header to check for Cognito indicators
				headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
				if err == nil {
					var header map[string]interface{}
					if json.Unmarshal(headerBytes, &header) == nil {
						// Cognito tokens use RS256 and have a kid
						if alg, ok := header["alg"].(string); ok && alg == "RS256" {
							if _, hasKid := header["kid"]; hasKid {
								// Looks like a Cognito token
								cognitoMW(next).ServeHTTP(w, r)
								return
							}
						}
					}
				}
			}

			// Fall back to legacy admin JWT
			adminMW(next).ServeHTTP(w, r)
		})
	}
}
