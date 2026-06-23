package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Shared contextKey type for all middleware
type contextKey string

const (
	userClaimsKey contextKey = "user_claims"
)

var authValidationPool *pgxpool.Pool

// SetAuthValidationPool configures the DB pool for runtime token validity checks.
func SetAuthValidationPool(pool *pgxpool.Pool) {
	authValidationPool = pool
}

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
			if err := validateRuntimeClaims(r.Context(), claims); err != nil {
				httputil.WriteUnauthorized(w, "Invalid or expired token")
				return
			}

			// Add claims to context
			ctx := context.WithValue(r.Context(), userClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func validateRuntimeClaims(ctx context.Context, claims *auth.Claims) error {
	if authValidationPool == nil || claims == nil {
		return nil
	}

	switch claims.UserType {
	case auth.UserTypeAdmin:
		var sessionVersion int
		var isActive bool
		err := authValidationPool.QueryRow(ctx, `
			SELECT COALESCE(session_version, 1) AS session_version,
			       CASE WHEN COALESCE(is_active, 1) <> 0 THEN true ELSE false END AS is_active
			FROM operations_admins
			WHERE id = $1
		`, claims.UserID).Scan(&sessionVersion, &isActive)
		if err != nil {
			return fmt.Errorf("admin session invalid: %w", err)
		}
		if !isActive || sessionVersion != claims.SessionVersion {
			return fmt.Errorf("admin inactive or session revoked")
		}
		return nil

	case auth.UserTypeEmployee, auth.UserTypeMobile:
		if claims.TenantID == nil || *claims.TenantID <= 0 {
			return fmt.Errorf("tenant id missing")
		}
		var sessionVersion int
		var employeeActive bool
		var tenantActive bool
		var tenantValidTo *time.Time
		err := authValidationPool.QueryRow(ctx, `
			SELECT COALESCE(e.session_version, 1) AS session_version,
			       CASE WHEN COALESCE(e.is_active, 1) <> 0 THEN true ELSE false END AS employee_active,
			       CASE WHEN t.is_active::text IN ('1','t','true','TRUE') THEN true ELSE false END AS tenant_active,
			       COALESCE(t.valid_to, t.service_expired_on) AS tenant_valid_to
			FROM employees e
			JOIN tenants t ON t.id = e.tenant_id
			WHERE e.id = $1
			  AND e.tenant_id = $2
			  AND e.deleted_at IS NULL
			  AND t.deleted_at IS NULL
		`, claims.UserID, *claims.TenantID).Scan(&sessionVersion, &employeeActive, &tenantActive, &tenantValidTo)
		if err != nil {
			if err == pgx.ErrNoRows {
				return fmt.Errorf("employee tenant relation not found")
			}
			return fmt.Errorf("employee session invalid: %w", err)
		}
		if !employeeActive || !tenantActive || sessionVersion != claims.SessionVersion {
			return fmt.Errorf("employee inactive/tenant inactive/session revoked")
		}
		if tenantValidTo != nil && tenantValidTo.Before(time.Now()) {
			return fmt.Errorf("tenant expired")
		}
		return nil
	}

	return nil
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
