package department

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

// RegisterRoutes registers department routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	authMw := middleware.Auth(jwtSecret)

	// Department endpoints require authentication
	mux.Handle("GET /api/v1/departments", authMw(http.HandlerFunc(h.ListDepartments)))
	mux.Handle("GET /api/v1/departments/{id}", authMw(http.HandlerFunc(h.GetDepartment)))
	mux.Handle("POST /api/v1/departments", authMw(http.HandlerFunc(h.CreateDepartment)))
	mux.Handle("PUT /api/v1/departments/{id}", authMw(http.HandlerFunc(h.UpdateDepartment)))
	mux.Handle("DELETE /api/v1/departments/{id}", authMw(http.HandlerFunc(h.DeleteDepartment)))
}

// ListDepartments handles listing departments
func (h *Handler) ListDepartments(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}

	// Parse query parameters
	var req DepartmentListRequest

	// Admin can query any tenant, employees can only query their own tenant
	if claims.UserType == auth.UserTypeAdmin {
		tenantID, _ := strconv.ParseInt(r.URL.Query().Get("tenant_id"), 10, 64)
		if tenantID == 0 {
			httputil.WriteBadRequest(w, "tenant_id is required for admin")
			return
		}
		req.TenantID = tenantID
	} else {
		if claims.TenantID == nil {
			httputil.WriteForbidden(w, "No tenant access")
			return
		}
		req.TenantID = *claims.TenantID
	}

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

	// Check tenant access for non-admin users
	if claims.UserType != auth.UserTypeAdmin {
		if claims.TenantID == nil || *claims.TenantID != department.TenantID {
			httputil.WriteForbidden(w, "Access denied")
			return
		}
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

	// Only admin can create departments
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req CreateDepartmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
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

	// Only admin can update departments
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid department ID")
		return
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

	// Only admin can delete departments
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid department ID")
		return
	}

	if err := h.service.DeleteDepartment(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Department deleted successfully"})
}
