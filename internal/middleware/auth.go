package middleware

import (
	"context"
	"fmt"
	"log/slog"
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
			// 下载类接口 / websocket 允许用 query 里的 access_token（导航式下载无法带 header）
			if authHeader == "" && (strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || strings.Contains(r.URL.Path, "/actions/export")) {
				if token := strings.TrimSpace(r.URL.Query().Get("access_token")); token != "" {
					authHeader = "Bearer " + token
				}
			}
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
			valid, err := validateRuntimeClaims(r.Context(), claims)
			if err != nil {
				// 数据库不可用等基础设施故障不是「登录失效」。返回 401 会让前端清空
				// 登录态并把用户踢回登录页（例如开发库连接数耗尽时整页请求随机 401）。
				slog.Error("auth session validation failed", "user_id", claims.UserID, "user_type", claims.UserType, "error", err)
				httputil.WriteError(w, http.StatusServiceUnavailable, "SESSION_CHECK_UNAVAILABLE", "服务暂时不可用，请稍后重试", nil)
				return
			}
			if !valid {
				httputil.WriteUnauthorized(w, "Invalid or expired token")
				return
			}

			// Add claims to context
			ctx := context.WithValue(r.Context(), userClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// validateRuntimeClaims 按 token 里的会话版本与账号状态校验会话是否仍然有效。
// 返回 (false, nil) 表示会话被明确判定为失效（撤销、停用、到期）；
// 返回 error 表示校验过程本身失败（数据库不可用等），调用方应视为临时故障而非登出。
func validateRuntimeClaims(ctx context.Context, claims *auth.Claims) (bool, error) {
	if authValidationPool == nil || claims == nil {
		return true, nil
	}

	switch claims.UserType {
	case auth.UserTypeAdmin:
		var sessionVersion int
		var isActive bool
		err := authValidationPool.QueryRow(ctx, `
			SELECT COALESCE(session_version, 1) AS session_version,
			       CASE WHEN lower(COALESCE(is_active::text, 'true')) IN ('1', 't', 'true', 'yes') THEN true ELSE false END AS is_active
			FROM operations_admins
			WHERE id = $1
		`, claims.UserID).Scan(&sessionVersion, &isActive)
		if err != nil {
			if err == pgx.ErrNoRows {
				return false, nil
			}
			return false, fmt.Errorf("admin session invalid: %w", err)
		}
		if !isActive || sessionVersion != claims.SessionVersion {
			return false, nil
		}
		return true, nil

	case auth.UserTypeEmployee, auth.UserTypeMobile:
		if claims.TenantID == nil || *claims.TenantID <= 0 {
			return false, nil
		}
		var sessionVersion int
		var employeeActive bool
		var tenantActive bool
		var tenantValidTo *time.Time
		var trialEnd *time.Time
		err := authValidationPool.QueryRow(ctx, `
			SELECT COALESCE(e.session_version, 1) AS session_version,
			       CASE WHEN lower(COALESCE(e.is_active::text, 'true')) IN ('1', 't', 'true', 'yes') THEN true ELSE false END AS employee_active,
			       CASE WHEN t.is_active::text IN ('1','t','true','TRUE') THEN true ELSE false END AS tenant_active,
			       COALESCE(t.valid_to, t.service_expired_on) AS tenant_valid_to,
			       t.new_system_trial_ends_at AS trial_end
			FROM employees e
			JOIN tenants t ON t.id = e.tenant_id
			WHERE e.id = $1
			  AND e.tenant_id = $2
			  AND e.deleted_at IS NULL
			  AND t.deleted_at IS NULL
		`, claims.UserID, *claims.TenantID).Scan(&sessionVersion, &employeeActive, &tenantActive, &tenantValidTo, &trialEnd)
		if err != nil {
			if err == pgx.ErrNoRows {
				return false, nil
			}
			return false, fmt.Errorf("employee session invalid: %w", err)
		}
		if !employeeActive || !tenantActive || sessionVersion != claims.SessionVersion {
			return false, nil
		}
		// 主服务过期但新版本体验窗口未过期的，仍放行（存量客户体验新版本）。
		if tenantValidTo != nil && tenantValidTo.Before(time.Now()) && (trialEnd == nil || trialEnd.Before(time.Now())) {
			return false, nil
		}
		return true, nil
	}

	return true, nil
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
