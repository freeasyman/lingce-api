package tenant

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

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

// RegisterRoutes registers tenant routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	routes := []router.Route{
		{Method: "GET", Path: "/api/v1/tenants", Handler: h.ListTenants, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/tenants/{id}", Handler: h.GetTenant, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/tenants", Handler: h.CreateTenant, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/tenants/{id}", Handler: h.UpdateTenant, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PATCH", Path: "/api/v1/tenants/{id}", Handler: h.UpdateTenant, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "DELETE", Path: "/api/v1/tenants/{id}", Handler: h.DeleteTenant, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/tenants/{id}/subscription", Handler: h.GetTenantSubscription, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/tenants/{id}/subscription/actions/{action}", Handler: h.PerformSubscriptionAction, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/tenants/{id}/subscription/events", Handler: h.GetSubscriptionEvents, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/tenants/{id}/features", Handler: h.GetTenantFeatures, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/tenants/{id}/feature-group", Handler: h.AssignFeatureGroup, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/tenants/{id}/feature-overrides", Handler: h.GetFeatureOverrides, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/tenants/{id}/feature-overrides", Handler: h.SetFeatureOverrides, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PATCH", Path: "/api/v1/tenants/{id}/feature-overrides", Handler: h.SetFeatureOverrides, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/tenants/{id}/profile", Handler: h.GetTenantProfile, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/tenants/{id}/trial-home", Handler: h.GetTrialHomeSummary, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/tenants/{id}/trial-home/demo-view", Handler: h.MarkTrialDemoViewed, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/tenants/{id}/actions/init-trial", Handler: h.InitTrialTenant, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/tenants/{id}/profile", Handler: h.UpdateTenantProfile, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PATCH", Path: "/api/v1/tenants/{id}/profile", Handler: h.UpdateTenantProfile, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/tenants/{id}/statistics", Handler: h.GetInstitutionStatistics, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/tenants/{id}/medical-specialties", Handler: h.ListMedicalSpecialties, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/tenants/{id}/validity-logs", Handler: h.GetValidityChangeLogs, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "GET", Path: "/api/v1/ops/trial-customers", Handler: h.ListTrialCustomers, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "GET", Path: "/api/v1/ops/trial-customers/{id}", Handler: h.GetTrialCustomerDetail, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "POST", Path: "/api/v1/ops/trial-customers/{id}/assign-owner", Handler: h.AssignTrialCustomerOwner, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "POST", Path: "/api/v1/ops/trial-customers/{id}/follow-ups", Handler: h.CreateTrialCustomerFollowUp, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "GET", Path: "/api/v1/ops/trial-funnel", Handler: h.GetTrialCustomerFunnel, Auth: true, AllowedUserTypes: []string{"admin"}},
	}
	router.Register(mux, routes, router.RouteDeps{JWTSecret: jwtSecret})
}

// isAdmin checks if the current user is an admin
func (h *Handler) isAdmin(r *http.Request) bool {
	claims := middleware.GetUserClaims(r.Context())
	return claims != nil && claims.UserType == auth.UserTypeAdmin
}

// ListTenants handles listing tenants
func (h *Handler) ListTenants(w http.ResponseWriter, r *http.Request) {
	var req TenantListRequest
	req.Keyword = strings.TrimSpace(r.URL.Query().Get("keyword"))
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
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}

	// Tenant-side accounts can only see their own tenant in list API.
	if !h.isAdmin(r) {
		claims := middleware.GetUserClaims(r.Context())
		if claims == nil || claims.TenantID == nil || *claims.TenantID <= 0 {
			httputil.WriteForbidden(w, "Admin access required")
			return
		}

		tenant, err := h.service.GetTenantByID(r.Context(), *claims.TenantID)
		if err != nil {
			httputil.WriteInternalError(w, err.Error())
			return
		}

		matched := true
		keyword := strings.ToLower(strings.TrimSpace(req.Keyword))
		if keyword != "" {
			haystack := strings.ToLower(strings.Join([]string{
				tenant.Name,
				tenant.Code,
				tenant.ContactName,
				tenant.ContactPhone,
				tenant.ContactEmail,
				tenant.Industry,
			}, " "))
			if !strings.Contains(haystack, keyword) {
				matched = false
			}
		}
		if name := strings.TrimSpace(req.Name); name != "" && !strings.Contains(strings.ToLower(tenant.Name), strings.ToLower(name)) {
			matched = false
		}
		if code := strings.TrimSpace(req.Code); code != "" && !strings.Contains(strings.ToLower(tenant.Code), strings.ToLower(code)) {
			matched = false
		}
		if req.IsActive != nil && tenant.IsActive != *req.IsActive {
			matched = false
		}

		items := []*Tenant{}
		total := int64(0)
		if matched {
			total = 1
			if req.Page == 1 {
				items = append(items, tenant)
			}
		}

		httputil.WriteSuccess(w, map[string]interface{}{
			"items": items,
			"total": total,
			"page":  req.Page,
		})
		return
	}

	tenants, total, err := h.service.ListTenants(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]interface{}{
		"items": tenants,
		"total": total,
		"page":  req.Page,
	})
}

// GetTenant handles getting a tenant by ID
func (h *Handler) GetTenant(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}

	tenant, err := h.service.GetTenantByID(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, tenant)
}

// CreateTenant handles creating a new tenant
func (h *Handler) CreateTenant(w http.ResponseWriter, r *http.Request) {
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
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, tenant)
}

// UpdateTenant handles updating a tenant
func (h *Handler) UpdateTenant(w http.ResponseWriter, r *http.Request) {
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
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, tenant)
}

// DeleteTenant handles deleting a tenant
func (h *Handler) DeleteTenant(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) GetTenantSubscription(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}
	subscription, err := h.service.GetTenantSubscription(r.Context(), id)
	if err != nil {
		tenant, tErr := h.service.GetTenantByID(r.Context(), id)
		if tErr != nil {
			httputil.WriteInternalError(w, err.Error())
			return
		}
		start := "1970-01-01"
		end := "2099-12-31"
		if tenant.ValidFrom != nil {
			start = tenant.ValidFrom.Format("2006-01-02")
		}
		if tenant.ValidTo != nil {
			end = tenant.ValidTo.Format("2006-01-02")
		}
		httputil.WriteSuccess(w, map[string]interface{}{
			"id":         int64(0),
			"tenant_id":  id,
			"status":     "active",
			"start_date": start,
			"end_date":   end,
			"created_at": tenant.CreatedAt.Format("2006-01-02T15:04:05Z"),
			"updated_at": tenant.UpdatedAt.Format("2006-01-02T15:04:05Z"),
			"fallback":   true,
		})
		return
	}
	httputil.WriteSuccess(w, subscription)
}

func (h *Handler) ListTrialCustomers(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	req := TrialCustomerListRequest{
		Keyword:          strings.TrimSpace(r.URL.Query().Get("keyword")),
		Source:           strings.TrimSpace(r.URL.Query().Get("source")),
		Stage:            strings.TrimSpace(r.URL.Query().Get("stage")),
		ActivationStatus: strings.TrimSpace(r.URL.Query().Get("activation_status")),
		Page:             1,
		PageSize:         20,
	}
	if page, _ := strconv.Atoi(r.URL.Query().Get("page")); page > 0 {
		req.Page = page
	}
	if pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size")); pageSize > 0 {
		req.PageSize = pageSize
	}
	if ownerIDStr := strings.TrimSpace(r.URL.Query().Get("owner_admin_id")); ownerIDStr != "" {
		if ownerID, err := strconv.ParseInt(ownerIDStr, 10, 64); err == nil && ownerID > 0 {
			req.OwnerAdminID = &ownerID
		}
	}
	if hasRealRecordingStr := strings.TrimSpace(r.URL.Query().Get("has_real_recording")); hasRealRecordingStr != "" {
		value := hasRealRecordingStr == "true" || hasRealRecordingStr == "1"
		req.HasRealRecording = &value
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.UserID <= 0 {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}
	if err := h.service.applyTrialCustomerScope(r.Context(), claims.UserID, &req); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}

	resp, err := h.service.ListTrialCustomers(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

func (h *Handler) GetTrialCustomerDetail(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}
	tenantID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || tenantID <= 0 {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.UserID <= 0 {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}
	if err := h.service.authorizeTrialCustomerAccess(r.Context(), claims.UserID, tenantID); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	resp, err := h.service.GetTrialCustomerDetail(r.Context(), tenantID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			httputil.WriteNotFound(w, err.Error())
			return
		}
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

func (h *Handler) AssignTrialCustomerOwner(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}
	tenantID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || tenantID <= 0 {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}
	var req TrialCustomerAssignOwnerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	var assignedBy *int64
	if claims != nil && claims.UserID > 0 {
		if err := h.service.authorizeTrialCustomerAccess(r.Context(), claims.UserID, tenantID); err != nil {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		assignedBy = &claims.UserID
	}
	assignment, err := h.service.AssignTrialCustomerOwner(r.Context(), tenantID, req, assignedBy)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, assignment)
}

func (h *Handler) CreateTrialCustomerFollowUp(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}
	tenantID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || tenantID <= 0 {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}
	var req TrialCustomerFollowUpCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	var createdBy *int64
	if claims != nil && claims.UserID > 0 {
		if err := h.service.authorizeTrialCustomerAccess(r.Context(), claims.UserID, tenantID); err != nil {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		createdBy = &claims.UserID
	}
	item, err := h.service.CreateTrialCustomerFollowUp(r.Context(), tenantID, req, createdBy)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) GetTrialCustomerFunnel(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}
	resp, err := h.service.GetTrialCustomerFunnel(r.Context())
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

func (h *Handler) PerformSubscriptionAction(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}
	action := r.PathValue("action")
	if action == "" {
		httputil.WriteBadRequest(w, "Invalid action")
		return
	}
	var req SubscriptionActionRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			httputil.WriteBadRequest(w, "Invalid request body")
			return
		}
	}
	if err := h.service.PerformSubscriptionAction(r.Context(), id, action, req); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "invalid action") ||
			strings.Contains(msg, "plan not found") ||
			strings.Contains(msg, "plan_id is required") ||
			strings.Contains(msg, "extend_days must be positive") {
			httputil.WriteBadRequest(w, msg)
			return
		}
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Subscription action performed successfully"})
}

func (h *Handler) GetSubscriptionEvents(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}
	events, err := h.service.GetSubscriptionEvents(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, events)
}

func (h *Handler) GetTenantFeatures(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}
	if !h.isAdmin(r) {
		claims := middleware.GetUserClaims(r.Context())
		if claims == nil || claims.TenantID == nil || *claims.TenantID <= 0 || *claims.TenantID != id {
			httputil.WriteForbidden(w, "Admin access required")
			return
		}
	}
	policy, err := h.service.GetTenantFeatures(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, httputil.Response{Data: policy})
}

func (h *Handler) AssignFeatureGroup(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}
	var req AssignFeatureGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if err := h.service.AssignFeatureGroup(r.Context(), id, req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Feature group assigned successfully"})
}

func (h *Handler) GetFeatureOverrides(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}
	overrides, err := h.service.GetFeatureOverrides(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, overrides)
}

func (h *Handler) SetFeatureOverrides(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}
	var req FeatureOverrideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if err := h.service.SetFeatureOverrides(r.Context(), id, req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Feature overrides set successfully"})
}

func (h *Handler) GetTenantProfile(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}
	if claims.UserType != auth.UserTypeAdmin && (claims.TenantID == nil || *claims.TenantID != id) {
		httputil.WriteForbidden(w, "No tenant access")
		return
	}
	profile, err := h.service.GetTenantProfile(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, profile)
}

func (h *Handler) UpdateTenantProfile(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}
	if claims.UserType != auth.UserTypeAdmin && (claims.TenantID == nil || *claims.TenantID != id) {
		httputil.WriteForbidden(w, "No tenant access")
		return
	}
	var req UpdateTenantProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	profile, err := h.service.UpdateTenantProfile(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, profile)
}

func (h *Handler) InitTrialTenant(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}
	var req TrialInitRequest
	if r.Body != nil {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		if len(strings.TrimSpace(string(body))) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				httputil.WriteBadRequest(w, "Invalid request body")
				return
			}
		}
	}
	resp, err := h.service.InitTrialTenant(r.Context(), id, req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, httputil.Response{Data: resp})
}

func (h *Handler) GetTrialHomeSummary(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}
	if !h.isAdmin(r) {
		claims := middleware.GetUserClaims(r.Context())
		if claims == nil || claims.TenantID == nil || *claims.TenantID <= 0 || *claims.TenantID != id {
			httputil.WriteForbidden(w, "Admin access required")
			return
		}
	}
	summary, err := h.service.GetTrialHomeSummary(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, httputil.Response{Data: summary})
}

func (h *Handler) MarkTrialDemoViewed(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}
	if claims.UserType != auth.UserTypeAdmin && (claims.TenantID == nil || *claims.TenantID != id) {
		httputil.WriteForbidden(w, "No tenant access")
		return
	}
	var req struct {
		RoleCode string `json:"role_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		httputil.WriteBadRequest(w, "invalid payload")
		return
	}
	if strings.TrimSpace(req.RoleCode) == "" {
		httputil.WriteBadRequest(w, "role_code is required")
		return
	}
	if err := h.service.MarkTrialDemoViewed(r.Context(), id, req.RoleCode, claims.UserID); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"ok": true})
}

func (h *Handler) GetInstitutionStatistics(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}
	if claims.UserType != auth.UserTypeAdmin && (claims.TenantID == nil || *claims.TenantID != id) {
		httputil.WriteForbidden(w, "No tenant access")
		return
	}
	stats, err := h.service.GetInstitutionStatistics(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, stats)
}

func (h *Handler) ListMedicalSpecialties(w http.ResponseWriter, r *http.Request) {
	_, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}
	specialties, err := h.service.ListMedicalSpecialties(r.Context())
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, specialties)
}

func (h *Handler) GetValidityChangeLogs(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}
	logs, err := h.service.GetValidityChangeLogs(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, logs)
}
