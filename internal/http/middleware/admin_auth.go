package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const adminClaimsKey contextKey = "adminClaims"

// AdminJWT enforces a simple HMAC-signed JWT for admin endpoints.
func AdminJWT(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if secret == "" {
				http.Error(w, "admin auth disabled", http.StatusUnauthorized)
				return
			}
			auth := r.Header.Get("Authorization")
			if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
				http.Error(w, "missing authorization header", http.StatusUnauthorized)
				return
			}
			tokenString := strings.TrimPrefix(auth, "Bearer ")
			claims := jwt.RegisteredClaims{}
			token, err := jwt.ParseWithClaims(tokenString, &claims, func(token *jwt.Token) (any, error) {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(secret), nil
			})
			if err != nil || !token.Valid {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), adminClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AdminClaimsFromContext returns admin JWT claims if present.
func AdminClaimsFromContext(ctx context.Context) (jwt.RegisteredClaims, bool) {
	claims, ok := ctx.Value(adminClaimsKey).(jwt.RegisteredClaims)
	return claims, ok
}
