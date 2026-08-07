package recording

import (
	"net/http"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/jackc/pgx/v5/pgxpool"
)

func resolveRecordingScope(w http.ResponseWriter, r *http.Request, pool *pgxpool.Pool) (*tenancy.Scope, bool) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return nil, false
	}

	scope, err := tenancy.ResolveScope(r.Context(), pool, claims, strings.TrimSpace(r.URL.Query().Get("tenant_id")))
	if err != nil {
		if err.Error() == "no tenant access" || err.Error() == "access denied" {
			httputil.WriteForbidden(w, err.Error())
		} else {
			httputil.WriteBadRequest(w, err.Error())
		}
		return nil, false
	}
	return scope, true
}
