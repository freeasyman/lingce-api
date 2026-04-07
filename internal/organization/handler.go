package organization

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

// RegisterRoutes registers organization routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	authMw := middleware.Auth(jwtSecret)

	// Tenant management (admin only)
	mux.Handle("GET /api/v1/tenants", authMw(http.HandlerFunc(h.ListTenants)))
	mux.Handle("GET /api/v1/tenants/{id}", authMw(http.HandlerFunc(h.GetTenant)))
	mux.Handle("POST /api/v1/tenants", authMw(http.HandlerFunc(h.CreateTenant)))
	mux.Handle("PUT /api/v1/tenants/{id}", authMw(http.HandlerFunc(h.UpdateTenant)))
	mux.Handle("DELETE /api/v1/tenants/{id}", authMw(http.HandlerFunc(h.DeleteTenant)))

	// Medical specialties (authenticated users)
	mux.Handle("GET /api/v1/organization/medical-specialties", authMw(http.HandlerFunc(h.ListMedicalSpecialties)))

	// Employee assistants (authenticated users)
	mux.Handle("GET /api/v1/organization/employees/{id}/assistants", authMw(http.HandlerFunc(h.GetEmployeeAssistants)))
	mux.Handle("PUT /api/v1/organization/employees/{id}/assistants", authMw(http.HandlerFunc(h.UpdateEmployeeAssistants)))

	// Institution statistics (admin only)
	mux.Handle("GET /api/v1/institutions/statistics", authMw(http.HandlerFunc(h.GetInstitutionStatistics)))
}

// ListTenants handles listing tenants
func (h *Handler) ListTenants(w http.ResponseWriter, r *http.Request) {
	// Check admin permission
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	// Parse query parameters
	var req TenantListRequest
	req.Name = r.URL.Query().Get("name")
	req.Code = r.URL.Query().Get("code")

	if isActiveStr := r.URL.Query().Get("is_active"); isActiveStr != "" {
		isActive := isActiveStr == "true"
		req.IsActive = &isActive
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize

	tenants, total, err := h.service.ListTenants(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, tenants, int64(total), req.Page, req.PageSize)
}

// GetTenant handles getting a tenant by ID
func (h *Handler) GetTenant(w http.ResponseWriter, r *http.Request) {
	// Check admin permission
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}

	tenant, err := h.service.GetTenant(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, tenant)
}

// CreateTenant handles creating a new tenant
func (h *Handler) CreateTenant(w http.ResponseWriter, r *http.Request) {
	// Check admin permission
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req CreateTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	tenant, err := h.service.CreateTenant(r.Context(), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, tenant)
}

// UpdateTenant handles updating a tenant
func (h *Handler) UpdateTenant(w http.ResponseWriter, r *http.Request) {
	// Check admin permission
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}

	var req UpdateTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	tenant, err := h.service.UpdateTenant(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, tenant)
}

// DeleteTenant handles deleting a tenant
func (h *Handler) DeleteTenant(w http.ResponseWriter, r *http.Request) {
	// Check admin permission
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}

	if err := h.service.DeleteTenant(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Tenant deleted successfully"})
}

// isAdmin checks if the current user is an admin
func (h *Handler) isAdmin(r *http.Request) bool {
	claims := middleware.GetUserClaims(r.Context())
	return claims != nil && claims.UserType == auth.UserTypeAdmin
}

// ListMedicalSpecialties handles listing medical specialties
func (h *Handler) ListMedicalSpecialties(w http.ResponseWriter, r *http.Request) {
	specialties, err := h.service.ListMedicalSpecialties(r.Context())
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, specialties)
}

// GetEmployeeAssistants handles getting assistants for an employee
func (h *Handler) GetEmployeeAssistants(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid employee ID")
		return
	}

	assistants, err := h.service.GetEmployeeAssistants(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, assistants)
}

// UpdateEmployeeAssistants handles updating assistant bindings for an employee
func (h *Handler) UpdateEmployeeAssistants(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid employee ID")
		return
	}

	var req UpdateAssistantsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if err := h.service.UpdateEmployeeAssistants(r.Context(), id, req); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Assistants updated successfully"})
}

// GetInstitutionStatistics handles getting institution statistics
func (h *Handler) GetInstitutionStatistics(w http.ResponseWriter, r *http.Request) {
	// Check admin permission
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	stats, err := h.service.GetInstitutionStatistics(r.Context())
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, stats)
}
