package tenant

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
	mux.Handle("POST /api/v1/tenants/{id}/subscription/actions/renew", authMw(http.HandlerFunc(h.RenewSubscription)))
	mux.Handle("POST /api/v1/tenants/{id}/subscription/actions/upgrade", authMw(http.HandlerFunc(h.UpgradeSubscription)))
	mux.Handle("POST /api/v1/tenants/{id}/subscription/actions/pause", authMw(http.HandlerFunc(h.PauseSubscription)))
	mux.Handle("POST /api/v1/tenants/{id}/subscription/actions/cancel", authMw(http.HandlerFunc(h.CancelSubscription)))
	mux.Handle("POST /api/v1/tenants/{id}/subscription/actions/activate", authMw(http.HandlerFunc(h.ActivateSubscription)))
	mux.Handle("GET /api/v1/tenants/{id}/subscription/events", authMw(http.HandlerFunc(h.GetSubscriptionEvents)))

	// Tenant features (admin only)
	mux.Handle("GET /api/v1/tenants/{id}/features", authMw(http.HandlerFunc(h.GetTenantFeatures)))
	mux.Handle("PUT /api/v1/tenants/{id}/features/group", authMw(http.HandlerFunc(h.AssignFeatureGroup)))
	mux.Handle("PUT /api/v1/tenants/{id}/features/overrides", authMw(http.HandlerFunc(h.SetFeatureOverrides)))

	// Tenant profile (tenant-scoped)
	mux.Handle("GET /api/v1/tenants/{id}/profile", authMw(http.HandlerFunc(h.GetTenantProfile)))
	mux.Handle("PUT /api/v1/tenants/{id}/profile", authMw(http.HandlerFunc(h.UpdateTenantProfile)))

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
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

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

// Subscription action handlers - placeholders for now
func (h *Handler) GetTenantSubscription(w http.ResponseWriter, r *http.Request) {
	httputil.WriteInternalError(w, "Not implemented yet")
}

func (h *Handler) RenewSubscription(w http.ResponseWriter, r *http.Request) {
	httputil.WriteInternalError(w, "Not implemented yet")
}

func (h *Handler) UpgradeSubscription(w http.ResponseWriter, r *http.Request) {
	httputil.WriteInternalError(w, "Not implemented yet")
}

func (h *Handler) PauseSubscription(w http.ResponseWriter, r *http.Request) {
	httputil.WriteInternalError(w, "Not implemented yet")
}

func (h *Handler) CancelSubscription(w http.ResponseWriter, r *http.Request) {
	httputil.WriteInternalError(w, "Not implemented yet")
}

func (h *Handler) ActivateSubscription(w http.ResponseWriter, r *http.Request) {
	httputil.WriteInternalError(w, "Not implemented yet")
}

func (h *Handler) GetSubscriptionEvents(w http.ResponseWriter, r *http.Request) {
	httputil.WriteInternalError(w, "Not implemented yet")
}

// Feature handlers - placeholders for now
func (h *Handler) GetTenantFeatures(w http.ResponseWriter, r *http.Request) {
	httputil.WriteInternalError(w, "Not implemented yet")
}

func (h *Handler) AssignFeatureGroup(w http.ResponseWriter, r *http.Request) {
	httputil.WriteInternalError(w, "Not implemented yet")
}

func (h *Handler) SetFeatureOverrides(w http.ResponseWriter, r *http.Request) {
	httputil.WriteInternalError(w, "Not implemented yet")
}

// Profile handlers - placeholders for now
func (h *Handler) GetTenantProfile(w http.ResponseWriter, r *http.Request) {
	httputil.WriteInternalError(w, "Not implemented yet")
}

func (h *Handler) UpdateTenantProfile(w http.ResponseWriter, r *http.Request) {
	httputil.WriteInternalError(w, "Not implemented yet")
}

func (h *Handler) GetValidityChangeLogs(w http.ResponseWriter, r *http.Request) {
	httputil.WriteInternalError(w, "Not implemented yet")
}
