package department

import (
	"context"
	"encoding/json"
	"fmt"
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

func resolveDepartmentTenantID(claims *auth.Claims, tenantIDParam string) (*int64, error) {
	tenantID, err := tenancy.RequireTenantID(claims, tenantIDParam)
	if err != nil {
		return nil, err
	}
	return &tenantID, nil
}

func (h *Handler) requireInstitutionMenuAccess(ctx context.Context, claims *auth.Claims, menuCode string) error {
	if claims == nil {
		return fmt.Errorf("invalid token")
	}
	if claims.UserType == auth.UserTypeAdmin {
		return nil
	}
	if claims.UserType != auth.UserTypeEmployee && claims.UserType != auth.UserTypeMobile {
		return nil
	}
	allowed, err := h.service.store.EmployeeHasInstitutionMenuAccess(ctx, claims.UserID, menuCode)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("menu %s access denied", menuCode)
	}
	return nil
}

// RegisterRoutes registers department routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	routes := []router.Route{
		{Method: "GET", Path: "/api/v1/departments", Handler: h.ListDepartments, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/departments/{id}", Handler: h.GetDepartment, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "POST", Path: "/api/v1/departments", Handler: h.CreateDepartment, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "PUT", Path: "/api/v1/departments/{id}", Handler: h.UpdateDepartment, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "DELETE", Path: "/api/v1/departments/{id}", Handler: h.DeleteDepartment, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/departments/health", Handler: h.DepartmentHealthCheck, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "POST", Path: "/api/v1/departments/actions/sync-from-visits", Handler: h.SyncDepartmentsFromVisits, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/departments/{id}/performance", Handler: h.GetDepartmentPerformance, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
	}
	router.Register(mux, routes, router.RouteDeps{JWTSecret: jwtSecret})
}

// ListDepartments handles listing departments
func (h *Handler) ListDepartments(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "departments"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}

	// Parse query parameters
	var req DepartmentListRequest

	tenantID, err := resolveDepartmentTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	req.TenantID = *tenantID

	req.Name = r.URL.Query().Get("name")
	req.Code = r.URL.Query().Get("code")

	if parentIDStr := r.URL.Query().Get("parent_id"); parentIDStr != "" {
		parentID, _ := strconv.ParseInt(parentIDStr, 10, 64)
		req.ParentID = &parentID
	}

	if isActiveStr := r.URL.Query().Get("is_active"); isActiveStr != "" {
		isActive := isActiveStr == "true"
		req.IsActive = &isActive
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize

	departments, total, err := h.service.ListDepartments(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, departments, int64(total), req.Page, req.PageSize)
}

// GetDepartment handles getting a department by ID
func (h *Handler) GetDepartment(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "departments"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid department ID")
		return
	}

	department, err := h.service.GetDepartment(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if err := tenancy.RequireSameTenant(claims, department.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	httputil.WriteSuccess(w, department)
}

// CreateDepartment handles creating a new department
func (h *Handler) CreateDepartment(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "departments"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}

	var req CreateDepartmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if req.TenantID == 0 {
		tenantID, err := resolveDepartmentTenantID(claims, "")
		if err != nil {
			httputil.WriteBadRequest(w, err.Error())
			return
		}
		req.TenantID = *tenantID
	}

	department, err := h.service.CreateDepartment(r.Context(), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, department)
}

// UpdateDepartment handles updating a department
func (h *Handler) UpdateDepartment(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "departments"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid department ID")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		if claims.TenantID == nil {
			httputil.WriteForbidden(w, "No tenant access")
			return
		}
		existing, err := h.service.GetDepartment(r.Context(), id)
		if err != nil {
			httputil.WriteNotFound(w, err.Error())
			return
		}
		if err := tenancy.RequireSameTenant(claims, existing.TenantID); err != nil {
			httputil.WriteForbidden(w, "Access denied")
			return
		}
	}

	var req UpdateDepartmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	department, err := h.service.UpdateDepartment(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, department)
}

// DeleteDepartment handles deleting a department
func (h *Handler) DeleteDepartment(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "departments"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid department ID")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		if claims.TenantID == nil {
			httputil.WriteForbidden(w, "No tenant access")
			return
		}
		existing, err := h.service.GetDepartment(r.Context(), id)
		if err != nil {
			httputil.WriteNotFound(w, err.Error())
			return
		}
		if err := tenancy.RequireSameTenant(claims, existing.TenantID); err != nil {
			httputil.WriteForbidden(w, "Access denied")
			return
		}
	}

	if err := h.service.DeleteDepartment(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Department deleted successfully"})
}

func (h *Handler) DepartmentHealthCheck(w http.ResponseWriter, r *http.Request) {
	httputil.WriteSuccess(w, map[string]string{
		"status": "ok",
		"module": "departments",
	})
}

func (h *Handler) SyncDepartmentsFromVisits(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	var tenantID *int64
	if rawTenantID, ok := req["tenant_id"]; ok {
		switch v := rawTenantID.(type) {
		case float64:
			tid := int64(v)
			tenantID = &tid
		case int64:
			tid := v
			tenantID = &tid
		}
	}

	result, err := h.service.SyncDepartmentsFromVisits(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"synced":  result["synced"],
		"created": result["created"],
		"updated": result["updated"],
		"message": "Departments synced successfully",
	})
}

func (h *Handler) GetDepartmentPerformance(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "departments"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid department ID")
		return
	}

	period := r.URL.Query().Get("period")
	if period == "" {
		period = "month"
	}

	stats, err := h.service.GetDepartmentPerformance(r.Context(), id, period)
	if err != nil {
		if err.Error() == "department not found" {
			httputil.WriteNotFound(w, err.Error())
			return
		}
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, stats)
}
