package content

import (
	"encoding/json"
	"net/http"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

func (h *Handler) registerContentOrgProfileRoutes(mux *http.ServeMux, deps router.RouteDeps) {
	router.Register(mux, []router.Route{
		{Method: "GET", Path: "/api/v1/content-org-profile", Handler: h.GetContentOrgProfile, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/content-org-profile", Handler: h.UpdateContentOrgProfile, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}, deps)
}

func (h *Handler) contentOrgProfileTenant(w http.ResponseWriter, r *http.Request) (int64, bool) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return 0, false
	}
	scope, err := h.resolveTenantScope(claims, r)
	if err != nil || scope == nil || scope.TenantID == nil {
		httputil.WriteForbidden(w, "no tenant access")
		return 0, false
	}
	return *scope.TenantID, true
}

func (h *Handler) GetContentOrgProfile(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.contentOrgProfileTenant(w, r)
	if !ok {
		return
	}
	profile, err := h.service.store.GetContentOrgProfile(r.Context(), tenantID)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	httputil.WriteSuccess(w, profile)
}

func (h *Handler) UpdateContentOrgProfile(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.contentOrgProfileTenant(w, r)
	if !ok {
		return
	}
	var req ContentOrgProfile
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	req.TenantID = tenantID
	profile, err := h.service.store.UpsertContentOrgProfile(r.Context(), &req)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	httputil.WriteSuccess(w, profile)
}
