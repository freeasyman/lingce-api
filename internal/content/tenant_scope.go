package content

import (
	"net/http"

	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/auth"
)

func (h *Handler) resolveTenantScope(claims *auth.Claims, r *http.Request) (*tenancy.Scope, error) {
	return tenancy.ResolveScope(r.Context(), h.service.store.pool, claims, r.URL.Query().Get("tenant_id"))
}
