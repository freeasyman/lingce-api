package tenant

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

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

// RegisterRoutes registers tenant routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	authMw := middleware.Auth(jwtSecret)

	// Tenant CRUD (admin only)
	mux.Handle("GET /api/v1/tenants", authMw(http.HandlerFunc(h.ListTenants)))
	mux.Handle("GET /api/v1/tenants/{id}", authMw(http.HandlerFunc(h.GetTenant)))
	mux.Handle("POST /api/v1/tenants", authMw(http.HandlerFunc(h.CreateTenant)))
	mux.Handle("PUT /api/v1/tenants/{id}", authMw(http.HandlerFunc(h.UpdateTenant)))
	mux.Handle("DELETE /api/v1/tenants/{id}", authMw(http.HandlerFunc(h.DeleteTenant)))

	// Tenant subscription actions (admin only)
	mux.Handle("GET /api/v1/tenants/{id}/subscription", authMw(http.HandlerFunc(h.GetTenantSubscription)))
	mux.Handle("POST /api/v1/tenants/{id}/subscription/actions/{action}", authMw(http.HandlerFunc(h.PerformSubscriptionAction)))
	mux.Handle("GET /api/v1/tenants/{id}/subscription/events", authMw(http.HandlerFunc(h.GetSubscriptionEvents)))

	// Tenant features (admin only)
	mux.Handle("GET /api/v1/tenants/{id}/features", authMw(http.HandlerFunc(h.GetTenantFeatures)))
	mux.Handle("POST /api/v1/tenants/{id}/feature-group", authMw(http.HandlerFunc(h.AssignFeatureGroup)))
	mux.Handle("GET /api/v1/tenants/{id}/feature-overrides", authMw(http.HandlerFunc(h.GetFeatureOverrides)))
	mux.Handle("PUT /api/v1/tenants/{id}/feature-overrides", authMw(http.HandlerFunc(h.SetFeatureOverrides)))

	// Tenant profile (tenant-scoped)
	mux.Handle("GET /api/v1/tenants/{id}/profile", authMw(http.HandlerFunc(h.GetTenantProfile)))
	mux.Handle("PUT /api/v1/tenants/{id}/profile", authMw(http.HandlerFunc(h.UpdateTenantProfile)))
	mux.Handle("GET /api/v1/tenants/{id}/statistics", authMw(http.HandlerFunc(h.GetInstitutionStatistics)))
	mux.Handle("GET /api/v1/tenants/{id}/medical-specialties", authMw(http.HandlerFunc(h.ListMedicalSpecialties)))

	// Tenant validity logs (admin only)
	mux.Handle("GET /api/v1/tenants/{id}/validity-logs", authMw(http.HandlerFunc(h.GetValidityChangeLogs)))
}

// isAdmin checks if the current user is an admin
func (h *Handler) isAdmin(r *http.Request) bool {
	claims := middleware.GetUserClaims(r.Context())
	return claims != nil && claims.UserType == auth.UserTypeAdmin
}

// ListTenants handles listing tenants
func (h *Handler) ListTenants(w http.ResponseWriter, r *http.Request) {
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
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, subscription)
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
