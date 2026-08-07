package sysconfig

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

// RegisterRoutes registers sysconfig routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	routes := []router.Route{
		{Method: "GET", Path: "/api/v1/subscription-plans", Handler: h.ListSubscriptionPlans, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "POST", Path: "/api/v1/subscription-plans", Handler: h.CreateSubscriptionPlan, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "PUT", Path: "/api/v1/subscription-plans/{id}", Handler: h.UpdateSubscriptionPlan, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "GET", Path: "/api/v1/feature-groups", Handler: h.ListFeatureGroups, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "GET", Path: "/api/v1/feature-groups/{id}", Handler: h.GetFeatureGroup, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "POST", Path: "/api/v1/feature-groups", Handler: h.CreateFeatureGroup, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "PUT", Path: "/api/v1/feature-groups/{id}", Handler: h.UpdateFeatureGroup, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "DELETE", Path: "/api/v1/feature-groups/{id}", Handler: h.DeleteFeatureGroup, Auth: true, AllowedUserTypes: []string{"admin"}},
		{Method: "GET", Path: "/api/v1/feature-groups/options", Handler: h.GetFeatureOptions, Auth: true, AllowedUserTypes: []string{"admin"}},
	}
	router.Register(mux, routes, router.RouteDeps{JWTSecret: jwtSecret})
}

// isAdmin checks if the current user is an admin
func (h *Handler) isAdmin(r *http.Request) bool {
	claims := middleware.GetUserClaims(r.Context())
	return claims != nil && claims.UserType == auth.UserTypeAdmin
}

// Subscription Plan handlers

// ListSubscriptionPlans handles listing subscription plans
func (h *Handler) ListSubscriptionPlans(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var isActive *bool
	if isActiveStr := r.URL.Query().Get("is_active"); isActiveStr != "" {
		active := isActiveStr == "true"
		isActive = &active
	}

	plans, err := h.service.ListSubscriptionPlans(r.Context(), isActive)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, plans)
}

// CreateSubscriptionPlan handles creating a subscription plan
func (h *Handler) CreateSubscriptionPlan(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req CreateSubscriptionPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	plan, err := h.service.CreateSubscriptionPlan(r.Context(), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, plan)
}

// UpdateSubscriptionPlan handles updating a subscription plan
func (h *Handler) UpdateSubscriptionPlan(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid plan ID")
		return
	}

	var req UpdateSubscriptionPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	plan, err := h.service.UpdateSubscriptionPlan(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, plan)
}

// Subscription handlers

// GetTenantSubscription handles getting a tenant's subscription
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

// PerformSubscriptionAction handles performing an action on a subscription
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

	var req SubscriptionActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if err := h.service.PerformSubscriptionAction(r.Context(), id, req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Subscription action performed successfully"})
}

// GetSubscriptionEvents handles getting subscription events
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

// GetValidityChangeLogs handles getting validity change logs
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

// Feature Group handlers

// ListFeatureGroups handles listing feature groups
func (h *Handler) ListFeatureGroups(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var isActive *bool
	if isActiveStr := r.URL.Query().Get("is_active"); isActiveStr != "" {
		active := isActiveStr == "true"
		isActive = &active
	}

	groups, err := h.service.ListFeatureGroups(r.Context(), isActive)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, httputil.Response{Data: groups})
}

// GetFeatureGroup handles getting a feature group by ID
func (h *Handler) GetFeatureGroup(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid feature group ID")
		return
	}

	group, err := h.service.GetFeatureGroup(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, group)
}

// CreateFeatureGroup handles creating a feature group
func (h *Handler) CreateFeatureGroup(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req CreateFeatureGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	group, err := h.service.CreateFeatureGroup(r.Context(), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, group)
}

// UpdateFeatureGroup handles updating a feature group
func (h *Handler) UpdateFeatureGroup(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid feature group ID")
		return
	}

	var req UpdateFeatureGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	group, err := h.service.UpdateFeatureGroup(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, group)
}

// DeleteFeatureGroup handles deleting a feature group
func (h *Handler) DeleteFeatureGroup(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid feature group ID")
		return
	}

	if err := h.service.DeleteFeatureGroup(r.Context(), id); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Feature group deleted successfully"})
}

// Feature Control handlers

// AssignFeatureGroup handles assigning a feature group to a tenant
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

// SetFeatureOverrides handles setting feature overrides for a tenant
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

// GetFeatureOverrides handles getting feature overrides for a tenant
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

// GetEffectiveFeaturePolicy handles getting the effective feature policy for a tenant
func (h *Handler) GetEffectiveFeaturePolicy(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid tenant ID")
		return
	}

	policy, err := h.service.GetEffectiveFeaturePolicy(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, policy)
}

// GetFeatureOptions handles getting all available feature codes
func (h *Handler) GetFeatureOptions(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	options, err := h.service.GetFeatureOptions(r.Context())
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, httputil.Response{Data: options})
}
