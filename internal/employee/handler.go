package employee

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes registers employee routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	authMw := middleware.Auth(jwtSecret)

	// Employee endpoints require authentication
	mux.Handle("GET /api/v1/employees", authMw(http.HandlerFunc(h.ListEmployees)))
	mux.Handle("GET /api/v1/employees/{id}", authMw(http.HandlerFunc(h.GetEmployee)))
	mux.Handle("POST /api/v1/employees", authMw(http.HandlerFunc(h.CreateEmployee)))
	mux.Handle("PUT /api/v1/employees/{id}", authMw(http.HandlerFunc(h.UpdateEmployee)))
	mux.Handle("POST /api/v1/employees/{id}/actions/reset-password", authMw(http.HandlerFunc(h.ResetPassword)))
	mux.Handle("POST /api/v1/employees/{id}/reset-password", authMw(http.HandlerFunc(h.ResetPassword)))
	mux.Handle("DELETE /api/v1/employees/{id}", authMw(http.HandlerFunc(h.DeleteEmployee)))
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

	// Admin can query any tenant, employees can only query their own tenant
	if claims.UserType == auth.UserTypeAdmin {
		tenantID, _ := strconv.ParseInt(r.URL.Query().Get("tenant_id"), 10, 64)
		if tenantID == 0 {
			// Compatibility fallback: allow legacy requests without tenant_id.
			if claims.TenantID != nil && *claims.TenantID > 0 {
				tenantID = *claims.TenantID
			} else {
				tenantID = 1
			}
		}
		req.TenantID = tenantID
	} else {
		if claims.TenantID == nil {
			httputil.WriteForbidden(w, "No tenant access")
			return
		}
		req.TenantID = *claims.TenantID
	}

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

	// Check tenant access for non-admin users
	if claims.UserType != auth.UserTypeAdmin {
		if claims.TenantID == nil || *claims.TenantID != employee.TenantID {
			httputil.WriteForbidden(w, "Access denied")
			return
		}
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

	// Only admin can create employees
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req CreateEmployeeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
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

	// Only admin can update employees
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid employee ID")
		return
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

	// Only admin can delete employees
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid employee ID")
		return
	}

	if err := h.service.DeleteEmployee(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Employee deleted successfully"})
}
