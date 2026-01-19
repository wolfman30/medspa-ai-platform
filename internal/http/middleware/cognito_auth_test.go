package middleware

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCognitoJWTNotConfigured(t *testing.T) {
	mw := CognitoJWT(CognitoConfig{})
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()

	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestCognitoOrAdminJWTFallsBackToAdmin(t *testing.T) {
	token := signedAdminToken(t, "secret")
	mw := CognitoOrAdminJWT(CognitoConfig{}, "secret")

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	called := false
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)

	if !called {
		t.Fatalf("expected handler to be called via admin fallback")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestParseRSAPublicKeyRoundTrip(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	n := base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(intToBytes(key.PublicKey.E))

	parsed, err := parseRSAPublicKey(n, e)
	if err != nil {
		t.Fatalf("parse rsa key: %v", err)
	}
	if parsed.N.Cmp(key.PublicKey.N) != 0 || parsed.E != key.PublicKey.E {
		t.Fatalf("parsed key does not match original")
	}
}

func TestFetchJWKSReturnsKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	n := base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(intToBytes(key.PublicKey.E))

	payload := jwksResponse{
		Keys: []jwkKey{{
			Kid: "test-kid",
			Kty: "RSA",
			Alg: "RS256",
			Use: "sig",
			N:   n,
			E:   e,
		}},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal jwks: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	defer server.Close()

	keys, err := fetchJWKS(server.URL)
	if err != nil {
		t.Fatalf("fetch jwks: %v", err)
	}
	if got := keys["test-kid"]; got == nil {
		t.Fatalf("expected key to be present")
	} else if got.N.Cmp(key.PublicKey.N) != 0 || got.E != key.PublicKey.E {
		t.Fatalf("returned key does not match original")
	}
}

func TestFetchJWKSReturnsErrorOnBadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	if _, err := fetchJWKS(server.URL); err == nil {
		t.Fatalf("expected error for non-200 response")
	}
}

func TestCognitoClaimsFromContext(t *testing.T) {
	claims := &CognitoClaims{Email: "user@example.com"}
	ctx := context.WithValue(context.Background(), cognitoClaimsKey, claims)
	got, ok := CognitoClaimsFromContext(ctx)
	if !ok || got.Email != "user@example.com" {
		t.Fatalf("expected claims from context")
	}
}

func intToBytes(v int) []byte {
	if v == 0 {
		return []byte{0}
	}
	out := []byte{}
	for v > 0 {
		out = append([]byte{byte(v & 0xff)}, out...)
		v >>= 8
	}
	return out
}
