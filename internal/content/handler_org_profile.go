package content

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

func (h *Handler) registerContentOrgProfileRoutes(mux *http.ServeMux, deps router.RouteDeps) {
	router.Register(mux, []router.Route{
		{Method: "GET", Path: "/api/v1/content-org-profile", Handler: h.GetContentOrgProfile, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/content-org-profile", Handler: h.UpdateContentOrgProfile, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/content-org-profile/assets", Handler: h.UploadContentOrgAsset, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
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

func (h *Handler) UploadContentOrgAsset(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.contentOrgProfileTenant(w, r)
	if !ok {
		return
	}
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		httputil.WriteBadRequest(w, "invalid multipart form")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		httputil.WriteBadRequest(w, "file is required")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 20<<20+1))
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	if len(data) > 20<<20 {
		httputil.WriteBadRequest(w, "图片不能超过 20MB")
		return
	}
	contentType := http.DetectContentType(data)
	if contentType != "image/png" && contentType != "image/jpeg" {
		httputil.WriteBadRequest(w, "only PNG or JPEG is supported")
		return
	}
	kind := strings.TrimSpace(r.FormValue("kind"))
	if kind != "logo" && kind != "qr" {
		httputil.WriteBadRequest(w, "kind must be logo or qr")
		return
	}
	url, err := h.service.UploadContentOrgAsset(r.Context(), tenantID, kind, data, contentType)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	httputil.WriteSuccess(w, map[string]string{"url": url})
}
