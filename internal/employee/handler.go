package employee

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

func resolveEmployeeTenantID(claims *auth.Claims, tenantIDParam string, allowLegacyFallback bool) (*int64, error) {
	tenantID, err := tenancy.RequireTenantID(claims, tenantIDParam)
	if err != nil {
		if allowLegacyFallback && claims != nil && claims.UserType == auth.UserTypeAdmin && tenantIDParam == "" {
			fallback := int64(1)
			if claims.TenantID != nil && *claims.TenantID > 0 {
				fallback = *claims.TenantID
			}
			return &fallback, nil
		}
		return nil, err
	}
	return &tenantID, nil
}

// RegisterRoutes registers employee routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	routes := []router.Route{
		{Method: "GET", Path: "/api/v1/employees", Handler: h.ListEmployees, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/employees/{id}", Handler: h.GetEmployee, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/employees", Handler: h.CreateEmployee, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/employees/{id}", Handler: h.UpdateEmployee, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/employees/{id}/actions/reset-password", Handler: h.ResetPassword, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "POST", Path: "/api/v1/employees/{id}/reset-password", Handler: h.ResetPassword, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "DELETE", Path: "/api/v1/employees/{id}", Handler: h.DeleteEmployee, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}
	router.Register(mux, routes, router.RouteDeps{JWTSecret: jwtSecret})
}

// ListEmployees handles listing employees
func (h *Handler) ListEmployees(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Parse query parameters
	var req EmployeeListRequest

	tenantID, err := resolveEmployeeTenantID(claims, r.URL.Query().Get("tenant_id"), true)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	req.TenantID = *tenantID

	req.Username = r.URL.Query().Get("username")
	req.FullName = r.URL.Query().Get("full_name")
	req.Phone = r.URL.Query().Get("phone")
	req.Role = r.URL.Query().Get("role")

	if deptIDStr := r.URL.Query().Get("department_id"); deptIDStr != "" {
		deptID, _ := strconv.ParseInt(deptIDStr, 10, 64)
		req.DepartmentID = &deptID
	}

	if isActiveStr := r.URL.Query().Get("is_active"); isActiveStr != "" {
		isActive := isActiveStr == "true"
		req.IsActive = &isActive
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize

	employees, total, err := h.service.ListEmployees(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, employees, int64(total), req.Page, req.PageSize)
}

// GetEmployee handles getting an employee by ID
func (h *Handler) GetEmployee(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid employee ID")
		return
	}

	employee, err := h.service.GetEmployee(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	if err := tenancy.RequireSameTenant(claims, employee.TenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	httputil.WriteSuccess(w, employee)
}

// CreateEmployee handles creating a new employee
func (h *Handler) CreateEmployee(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	var req CreateEmployeeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if req.TenantID == 0 {
		tenantID, err := resolveEmployeeTenantID(claims, "", false)
		if err != nil {
			httputil.WriteBadRequest(w, err.Error())
			return
		}
		req.TenantID = *tenantID
	}

	employee, err := h.service.CreateEmployee(r.Context(), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, employee)
}

// UpdateEmployee handles updating an employee
func (h *Handler) UpdateEmployee(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid employee ID")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		existing, err := h.service.GetEmployee(r.Context(), id)
		if err != nil {
			httputil.WriteNotFound(w, err.Error())
			return
		}
		if err := tenancy.RequireSameTenant(claims, existing.TenantID); err != nil {
			httputil.WriteForbidden(w, "Access denied")
			return
		}
	}

	var req UpdateEmployeeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	employee, err := h.service.UpdateEmployee(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, employee)
}

// ResetPassword handles resetting employee password
func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Only admin can reset passwords
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid employee ID")
		return
	}

	var req ResetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if err := h.service.ResetPassword(r.Context(), id, req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Password reset successfully"})
}

// DeleteEmployee handles deleting an employee
func (h *Handler) DeleteEmployee(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid employee ID")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		existing, err := h.service.GetEmployee(r.Context(), id)
		if err != nil {
			httputil.WriteNotFound(w, err.Error())
			return
		}
		if err := tenancy.RequireSameTenant(claims, existing.TenantID); err != nil {
			httputil.WriteForbidden(w, "Access denied")
			return
		}
	}

	if err := h.service.DeleteEmployee(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Employee deleted successfully"})
}
