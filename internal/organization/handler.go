package organization

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
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
	routes := []router.Route{
		{Method: "GET", Path: "/api/v1/employees/{id}/assistants", Handler: h.GetEmployeeAssistants, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/employees/{id}/assistants", Handler: h.UpdateEmployeeAssistants, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/employees/actions/sync-from-visits", Handler: h.SyncDoctorsFromVisits, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/employees/{id}/performance", Handler: h.GetDoctorPerformance, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/employees/performance/summary", Handler: h.GetDoctorPerformanceSummary, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}
	router.Register(mux, routes, router.RouteDeps{JWTSecret: jwtSecret})
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

// GetOrganizationProfile handles getting current organization profile.
func (h *Handler) GetOrganizationProfile(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveProfileTenantID(r)
	if !ok {
		httputil.WriteForbidden(w, "No tenant access")
		return
	}

	tenant, err := h.service.GetTenant(r.Context(), tenantID)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"id":         tenant.ID,
		"name":       tenant.Name,
		"org_code":   tenant.Code,
		"created_at": tenant.CreatedAt,
	})
}

// UpdateOrganizationProfile handles updating current organization profile.
func (h *Handler) UpdateOrganizationProfile(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveProfileTenantID(r)
	if !ok {
		httputil.WriteForbidden(w, "No tenant access")
		return
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	var req UpdateTenantRequest
	if name, ok := payload["name"].(string); ok && name != "" {
		req.Name = &name
	}

	tenant, err := h.service.UpdateTenant(r.Context(), tenantID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	resp := map[string]interface{}{
		"id":         tenant.ID,
		"name":       tenant.Name,
		"org_code":   tenant.Code,
		"created_at": tenant.CreatedAt,
	}
	if v, ok := payload["contact_name"]; ok {
		resp["contact_name"] = v
	}
	if v, ok := payload["contact_phone"]; ok {
		resp["contact_phone"] = v
	}
	if v, ok := payload["contact_email"]; ok {
		resp["contact_email"] = v
	}

	httputil.WriteSuccess(w, resp)
}

func (h *Handler) resolveProfileTenantID(r *http.Request) (int64, bool) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		return 0, false
	}
	if claims.TenantID != nil && *claims.TenantID > 0 {
		return *claims.TenantID, true
	}
	if claims.UserType == auth.UserTypeAdmin {
		if tenantIDStr := r.URL.Query().Get("tenant_id"); tenantIDStr != "" {
			if tenantID, err := strconv.ParseInt(tenantIDStr, 10, 64); err == nil && tenantID > 0 {
				return tenantID, true
			}
		}
		return 1, true
	}
	return 0, false
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
