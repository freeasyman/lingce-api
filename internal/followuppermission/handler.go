package followuppermission

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	router.Register(mux, []router.Route{
		{Method: "GET", Path: "/api/v1/followup/permissions", Handler: h.List, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/followup/permissions/{employee_id}", Handler: h.Get, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "PUT", Path: "/api/v1/followup/permissions/{employee_id}", Handler: h.Upsert, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
	}, router.RouteDeps{JWTSecret: jwtSecret})
}

func (h *Handler) tenantAndClaims(w http.ResponseWriter, r *http.Request) (*auth.Claims, int64, bool) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return nil, 0, false
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return nil, 0, false
	}
	return claims, tenantID, true
}

func (h *Handler) requireManager(w http.ResponseWriter, r *http.Request, claims *auth.Claims, tenantID int64) bool {
	if claims.UserType == auth.UserTypeAdmin {
		return true
	}
	access, err := h.service.Authorize(r.Context(), claims, tenantID, AbilityPermission)
	if err != nil || !access.CanManagePermissions() {
		httputil.WriteForbidden(w, "followup permission management required")
		return false
	}
	return true
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.tenantAndClaims(w, r)
	if !ok || !h.requireManager(w, r, claims, tenantID) {
		return
	}
	items, err := h.service.List(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.tenantAndClaims(w, r)
	if !ok {
		return
	}
	employeeID, err := strconv.ParseInt(r.PathValue("employee_id"), 10, 64)
	if err != nil || employeeID <= 0 {
		httputil.WriteBadRequest(w, "invalid employee_id")
		return
	}
	if employeeID != claims.UserID && !h.requireManager(w, r, claims, tenantID) {
		return
	}
	item, err := h.service.Get(r.Context(), tenantID, employeeID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}

func (h *Handler) Upsert(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.tenantAndClaims(w, r)
	if !ok || !h.requireManager(w, r, claims, tenantID) {
		return
	}
	employeeID, err := strconv.ParseInt(r.PathValue("employee_id"), 10, 64)
	if err != nil || employeeID <= 0 {
		httputil.WriteBadRequest(w, "invalid employee_id")
		return
	}
	var req UpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.service.Upsert(r.Context(), tenantID, employeeID, claims.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}
