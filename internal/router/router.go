package router

import (
	"net/http"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/rbac"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Route defines a declarative route with authentication and authorization requirements
type Route struct {
	Method           string
	Path             string
	Handler          http.HandlerFunc
	Auth             bool     // Whether authentication is required
	AllowedUserTypes []string // Allowed user types: admin, employee, mobile
	Permission       string   // Permission code (e.g., "recording:write"), empty string skips permission check
	TenantScoped     bool     // Whether tenant isolation is required
}

// RouteDeps contains dependencies for route registration
type RouteDeps struct {
	JWTSecret   string
	PermChecker *rbac.PermissionChecker
	Pool        *pgxpool.Pool // For tenant scope resolution
}

// Register registers routes with declarative authentication and authorization
func Register(mux *http.ServeMux, routes []Route, deps RouteDeps) {
	for _, route := range routes {
		handler := route.Handler

		// Apply middleware chain (innermost to outermost)
		if route.Auth {
			// 1. Tenant scope (if required)
			if route.TenantScoped && deps.Pool != nil {
				tenantScopeMw := tenancy.TenantScopeMiddleware(deps.Pool)
				handler = wrapHandler(tenantScopeMw(http.HandlerFunc(handler)))
			}

			// 2. Permission check (if specified)
			if route.Permission != "" && deps.PermChecker != nil {
				handler = permissionMiddleware(handler, route.Permission, deps.PermChecker)
			}

			// 3. User type check (if specified)
			if len(route.AllowedUserTypes) > 0 {
				handler = userTypeMiddleware(handler, route.AllowedUserTypes)
			}

			// 4. JWT authentication (outermost)
			authMw := middleware.Auth(deps.JWTSecret)
			handler = wrapHandler(authMw(http.HandlerFunc(handler)))
		}

		// Register route
		pattern := route.Method + " " + route.Path
		mux.HandleFunc(pattern, handler)
	}
}

// userTypeMiddleware checks if the user type is allowed
func userTypeMiddleware(next http.HandlerFunc, allowedTypes []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := middleware.GetUserClaims(r.Context())
		if claims == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Check if user type is allowed
		allowed := false
		for _, t := range allowedTypes {
			if string(claims.UserType) == t {
				allowed = true
				break
			}
		}

		if !allowed {
			http.Error(w, "Forbidden: insufficient privileges", http.StatusForbidden)
			return
		}

		next(w, r)
	}
}

// permissionMiddleware checks if the user has the required permission
func permissionMiddleware(next http.HandlerFunc, permission string, checker *rbac.PermissionChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := middleware.GetUserClaims(r.Context())
		if claims == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Parse permission (format: "resource:action")
		// For now, we'll implement the check in S0-3
		// This is a placeholder that will be filled when PermissionChecker is implemented
		if checker != nil {
			hasPermission, err := checker.HasPermission(r.Context(), claims.UserID, string(claims.UserType), permission)
			if err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			if !hasPermission {
				http.Error(w, "Forbidden: permission denied", http.StatusForbidden)
				return
			}
		}

		next(w, r)
	}
}

// wrapHandler converts http.Handler to http.HandlerFunc
func wrapHandler(h http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
	}
}
