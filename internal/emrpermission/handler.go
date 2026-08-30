package emrpermission

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	routes := []router.Route{
		{Method: "GET", Path: "/api/v1/emr/permissions", Handler: h.ListPermissions, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/emr/permissions/{employee_id}", Handler: h.GetPermission, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/emr/permissions/{employee_id}", Handler: h.UpsertPermission, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}
	router.Register(mux, routes, router.RouteDeps{JWTSecret: jwtSecret})
}

func (h *Handler) ListPermissions(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	access, err := h.service.Authorize(r.Context(), claims, tenantID, "quality.manage")
	if err != nil || !access.CanManagePermissions() {
		http.Error(w, "Forbidden: EMR permission management required", http.StatusForbidden)
		return
	}
	items, err := h.service.ListEmployeePermissions(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

func (h *Handler) GetPermission(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	employeeID, err := strconv.ParseInt(r.PathValue("employee_id"), 10, 64)
	if err != nil || employeeID <= 0 {
		httputil.WriteBadRequest(w, "invalid employee_id")
		return
	}
	if employeeID != claims.UserID {
		access, accessErr := h.service.Authorize(r.Context(), claims, tenantID, "quality.manage")
		if accessErr != nil || !access.CanManagePermissions() {
			http.Error(w, "Forbidden: EMR permission management required", http.StatusForbidden)
			return
		}
	}
	item, err := h.service.GetAssignment(r.Context(), tenantID, employeeID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}

func (h *Handler) UpsertPermission(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	access, err := h.service.Authorize(r.Context(), claims, tenantID, "quality.manage")
	if err != nil || !access.CanManagePermissions() {
		http.Error(w, "Forbidden: EMR permission management required", http.StatusForbidden)
		return
	}
	employeeID, err := strconv.ParseInt(r.PathValue("employee_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid employee_id")
		return
	}
	var req UpsertAssignmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	item, err := h.service.UpsertAssignment(r.Context(), tenantID, employeeID, claims.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}
