package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/freeasyman/lingce-api/internal/contract"
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
		// 会话校验与服务期校验彼此独立，并发执行：远程库每次往返约 70-90ms。
		var (
			sessionVersion int
			employeeActive bool
			tenantActive   bool
			tenantPaused   bool
			tenantAllowed  bool
			empErr         error
			wg             sync.WaitGroup
		)
		if claims.Entry == auth.EntryPortal {
			// 门户（新产品）：服务期看合同行。
			var canAccess bool
			var accessErr error
			wg.Add(2)
			go func() {
				defer wg.Done()
				empErr = authValidationPool.QueryRow(ctx, `
			SELECT COALESCE(e.session_version, 1) AS session_version,
			       CASE WHEN lower(COALESCE(e.is_active::text, 'true')) IN ('1', 't', 'true', 'yes') THEN true ELSE false END AS employee_active,
			       CASE WHEN t.is_active::text IN ('1','t','true','TRUE') THEN true ELSE false END AS tenant_active
			FROM employees e
			JOIN tenants t ON t.id = e.tenant_id
			WHERE e.id = $1
			  AND e.tenant_id = $2
			  AND e.deleted_at IS NULL
			  AND t.deleted_at IS NULL
		`, claims.UserID, *claims.TenantID).Scan(&sessionVersion, &employeeActive, &tenantActive)
			}()
			go func() {
				defer wg.Done()
				canAccess, accessErr = contract.CanAccess(ctx, authValidationPool, *claims.TenantID, time.Now())
			}()
			wg.Wait()
			if empErr != nil {
				if empErr == pgx.ErrNoRows {
					return false, nil
				}
				return false, fmt.Errorf("employee session invalid: %w", empErr)
			}
			if !employeeActive || !tenantActive || sessionVersion != claims.SessionVersion {
				return false, nil
			}
			if accessErr != nil {
				return false, fmt.Errorf("tenant validity check failed: %w", accessErr)
			}
			if !canAccess {
				return false, nil
			}
			return true, nil
		}
		// 机构端/移动端（老产品）：合同优先、老表兜底，与计费闸门/登录同口径。
		// 有服务期记录按合同判定；没有服务期记录才回退 tenants.valid_to。
		err := authValidationPool.QueryRow(ctx, `
			SELECT COALESCE(e.session_version, 1) AS session_version,
			       CASE WHEN lower(COALESCE(e.is_active::text, 'true')) IN ('1', 't', 'true', 'yes') THEN true ELSE false END AS employee_active,
			       CASE WHEN t.is_active::text IN ('1','t','true','TRUE') THEN true ELSE false END AS tenant_active,
			       (t.paused_at IS NOT NULL) AS tenant_paused,
			       CASE
			         WHEN EXISTS (SELECT 1 FROM subscription_periods sp WHERE sp.tenant_id = t.id)
			           THEN EXISTS (
			                  SELECT 1 FROM subscription_periods sp
			                  WHERE sp.tenant_id = t.id
			                    AND sp.status = 'active'
			                    AND sp.started_on <= CURRENT_DATE
			                    AND sp.expired_on >= CURRENT_DATE
			                )
			         ELSE COALESCE(t.valid_to, t.service_expired_on) > NOW()
			       END AS tenant_allowed
			FROM employees e
			JOIN tenants t ON t.id = e.tenant_id
			WHERE e.id = $1
			  AND e.tenant_id = $2
			  AND e.deleted_at IS NULL
			  AND t.deleted_at IS NULL
		`, claims.UserID, *claims.TenantID).Scan(&sessionVersion, &employeeActive, &tenantActive, &tenantPaused, &tenantAllowed)
		if err != nil {
			if err == pgx.ErrNoRows {
				return false, nil
			}
			return false, fmt.Errorf("employee session invalid: %w", err)
		}
		if !employeeActive || !tenantActive || sessionVersion != claims.SessionVersion {
			return false, nil
		}
		if tenantPaused || !tenantAllowed {
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
