package tenancy

import (
	"net/http"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TenantScopeMiddleware creates a middleware that resolves tenant scope and injects it into context
func TenantScopeMiddleware(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := middleware.GetUserClaims(r.Context())
			if claims == nil {
				httputil.WriteUnauthorized(w, "Invalid token")
				return
			}

			// Get tenant_id from query parameter
			tenantIDParam := r.URL.Query().Get("tenant_id")

			scope, err := ResolveScope(r.Context(), pool, claims, tenantIDParam)
			if err != nil {
				if err.Error() == "no tenant access" || err.Error() == "access denied" {
					httputil.WriteForbidden(w, err.Error())
					return
				}
				httputil.WriteBadRequest(w, err.Error())
				return
			}

			// Inject scope into context
			ctx := WithScope(r.Context(), scope)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
