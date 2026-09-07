package emrquality

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/freeasyman/lingce-api/internal/emrpermission"
	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
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
		{Method: "GET", Path: "/api/v1/emr/quality-requirements", Handler: h.List, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/emr/quality-requirements/{id}", Handler: h.Get, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}, router.RouteDeps{JWTSecret: jwtSecret})
}
func claimsTenant(r *http.Request) (int64, error) {
	c := middleware.GetUserClaims(r.Context())
	if c == nil {
		return 0, fmt.Errorf("invalid token")
	}
	return tenancy.RequireTenantID(c, r.URL.Query().Get("tenant_id"))
}
func (h *Handler) authorize(r *http.Request) (int64, bool, error) {
	c := middleware.GetUserClaims(r.Context())
	tid, err := tenancy.RequireTenantID(c, r.URL.Query().Get("tenant_id"))
	if err != nil {
		return 0, false, err
	}
	if _, err := h.permissions.Authorize(r.Context(), c, tid, "quality.manage"); err != nil {
		return 0, true, err
	}
	return tid, false, nil
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tid, forbidden, err := h.authorize(r)
	if err != nil {
		if forbidden {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	items, err := h.service.List(r.Context(), ListRequest{TenantID: tid, Group: r.URL.Query().Get("quality_group"), RuleType: r.URL.Query().Get("rule_type"), Status: r.URL.Query().Get("status")})
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	tid, forbidden, err := h.authorize(r)
	if err != nil {
		if forbidden {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		httputil.WriteBadRequest(w, "invalid quality requirement id")
		return
	}
	item, err := h.service.Get(r.Context(), tid, id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if item == nil {
		httputil.WriteNotFound(w, "quality requirement not found")
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}
