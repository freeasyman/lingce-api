package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// Shared contextKey type for all middleware
type contextKey string

const (
	userClaimsKey contextKey = "user_claims"
)

// Auth middleware validates JWT token
func Auth(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract token from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				httputil.WriteUnauthorized(w, "Missing authorization header")
				return
			}

			// Check Bearer prefix
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" {
				httputil.WriteUnauthorized(w, "Invalid authorization header format")
				return
			}

			tokenString := parts[1]

			// Parse and validate token
			claims, err := auth.ParseToken(jwtSecret, tokenString)
			if err != nil {
				httputil.WriteUnauthorized(w, "Invalid or expired token")
				return
			}

			// Add claims to context
			ctx := context.WithValue(r.Context(), userClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetUserClaims retrieves user claims from context
func GetUserClaims(ctx context.Context) *auth.Claims {
	if claims, ok := ctx.Value(userClaimsKey).(*auth.Claims); ok {
		return claims
	}
	return nil
}

// GetUserID retrieves user ID from context
func GetUserID(ctx context.Context) int64 {
	if claims := GetUserClaims(ctx); claims != nil {
		return claims.UserID
	}
	return 0
}

// GetTenantID retrieves tenant ID from context
func GetTenantID(ctx context.Context) *int64 {
	if claims := GetUserClaims(ctx); claims != nil {
		return claims.TenantID
	}
	return nil
}

// GetUserType retrieves user type from context
func GetUserType(ctx context.Context) auth.UserType {
	if claims := GetUserClaims(ctx); claims != nil {
		return claims.UserType
	}
	return ""
}
