package emrquality

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	router.Register(mux, []router.Route{
		{Method: "GET", Path: "/api/v1/emr/quality-requirements", Handler: h.List, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/emr/quality-requirements/{id}", Handler: h.Get, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/quality-requirements", Handler: h.Create, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/emr/quality-requirements/{id}", Handler: h.Update, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/quality-requirements/{id}/disable", Handler: h.Disable, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}, router.RouteDeps{JWTSecret: jwtSecret})
}
func claimsTenant(r *http.Request) (int64, error) {
	c := middleware.GetUserClaims(r.Context())
	if c == nil {
		return 0, fmt.Errorf("invalid token")
	}
	return tenancy.RequireTenantID(c, r.URL.Query().Get("tenant_id"))
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tid, err := claimsTenant(r)
	if err != nil {
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
	tid, err := claimsTenant(r)
	if err != nil {
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
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	c := middleware.GetUserClaims(r.Context())
	if c == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tid, err := tenancy.RequireTenantID(c, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	var req SaveRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	item, err := h.service.Create(r.Context(), tid, c.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	c := middleware.GetUserClaims(r.Context())
	if c == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tid, err := tenancy.RequireTenantID(c, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		httputil.WriteBadRequest(w, "invalid quality requirement id")
		return
	}
	var req SaveRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	item, err := h.service.Update(r.Context(), tid, id, c.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}
func (h *Handler) Disable(w http.ResponseWriter, r *http.Request) {
	c := middleware.GetUserClaims(r.Context())
	if c == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tid, err := tenancy.RequireTenantID(c, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		httputil.WriteBadRequest(w, "invalid quality requirement id")
		return
	}
	if err := h.service.Disable(r.Context(), tid, id, c.UserID); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"ok": true})
}
