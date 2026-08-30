package emrtemplate

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/freeasyman/lingce-api/internal/emrpermission"
	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service     *Service
	permissions *emrpermission.Service
}

func NewHandler(service *Service, permissions *emrpermission.Service) *Handler {
	return &Handler{service: service, permissions: permissions}
}
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	router.Register(mux, []router.Route{
		{Method: "GET", Path: "/api/v1/emr/templates", Handler: h.List, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/templates", Handler: h.Create, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/emr/templates/{id}", Handler: h.Get, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/templates/{id}/versions", Handler: h.CreateVersion, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/emr/templates/{id}/versions", Handler: h.ListVersions, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/emr/template-versions/{id}", Handler: h.GetVersion, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/emr/template-versions/{id}", Handler: h.UpdateVersion, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/emr/template-versions/{id}/sections", Handler: h.SaveSections, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/emr/template-versions/{id}/quality-requirements", Handler: h.SaveBindings, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/template-versions/{id}/publish", Handler: h.Publish, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/template-versions/{id}/disable", Handler: h.Disable, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}, router.RouteDeps{JWTSecret: jwtSecret})
}
func (h *Handler) auth(r *http.Request) (*auth.Claims, int64, error) {
	c := middleware.GetUserClaims(r.Context())
	if c == nil {
		return nil, 0, fmt.Errorf("invalid token")
	}
	tid, err := tenancy.RequireTenantID(c, r.URL.Query().Get("tenant_id"))
	return c, tid, err
}
func (h *Handler) authorize(r *http.Request, ability string) (*auth.Claims, int64, error) {
	claims, tenantID, err := h.auth(r)
	if err != nil {
		return nil, 0, err
	}
	if _, err := h.permissions.Authorize(r.Context(), claims, tenantID, ability); err != nil {
		return nil, 0, err
	}
	return claims, tenantID, nil
}
func parseID(r *http.Request) string      { return strings.TrimSpace(r.PathValue("id")) }
func decode(r *http.Request, v any) error { return json.NewDecoder(r.Body).Decode(v) }
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	_, tid, err := h.authorize(r, "record.read")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	items, err := h.service.List(r.Context(), tid)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	_, tid, err := h.authorize(r, "record.read")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	item, err := h.service.Get(r.Context(), tid, parseID(r))
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if item == nil {
		httputil.WriteNotFound(w, "template not found")
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	c, tid, err := h.authorize(r, "template.manage")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	var req SaveTemplateRequest
	if decode(r, &req) != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.service.Create(r.Context(), tid, c.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}
func (h *Handler) CreateVersion(w http.ResponseWriter, r *http.Request) {
	c, tid, err := h.authorize(r, "template.manage")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	var req SaveVersionRequest
	if decode(r, &req) != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.service.CreateVersion(r.Context(), tid, c.UserID, parseID(r), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}
func (h *Handler) ListVersions(w http.ResponseWriter, r *http.Request) {
	_, tid, err := h.authorize(r, "record.read")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	items, err := h.service.ListVersions(r.Context(), tid, parseID(r))
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}
func (h *Handler) GetVersion(w http.ResponseWriter, r *http.Request) {
	_, tid, err := h.authorize(r, "record.read")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	v, err := h.service.GetVersion(r.Context(), tid, parseID(r))
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if v == nil {
		httputil.WriteNotFound(w, "template version not found")
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": v})
}
func (h *Handler) UpdateVersion(w http.ResponseWriter, r *http.Request) {
	_, tid, err := h.authorize(r, "template.manage")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	var req SaveVersionRequest
	if decode(r, &req) != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	v, err := h.service.UpdateVersion(r.Context(), tid, parseID(r), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": v})
}
func (h *Handler) SaveSections(w http.ResponseWriter, r *http.Request) {
	_, tid, err := h.authorize(r, "template.manage")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	var req SaveSectionsRequest
	if decode(r, &req) != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	items, err := h.service.SaveSections(r.Context(), tid, parseID(r), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}
func (h *Handler) SaveBindings(w http.ResponseWriter, r *http.Request) {
	_, tid, err := h.authorize(r, "template.manage")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	var req SaveBindingsRequest
	if decode(r, &req) != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	items, err := h.service.SaveBindings(r.Context(), tid, parseID(r), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}
func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	c, tid, err := h.authorize(r, "template.manage")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if err := h.service.Publish(r.Context(), tid, parseID(r), c.UserID); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"ok": true})
}
func (h *Handler) Disable(w http.ResponseWriter, r *http.Request) {
	c, tid, err := h.authorize(r, "template.manage")
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if err := h.service.Disable(r.Context(), tid, parseID(r), c.UserID); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"ok": true})
}
