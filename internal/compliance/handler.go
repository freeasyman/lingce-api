package compliance

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	routes := []router.Route{
		{Method: "GET", Path: "/api/v1/compliance/rules", Handler: h.ListRules},
		{Method: "GET", Path: "/api/v1/compliance/rules/{id}", Handler: h.GetRule},
		{Method: "POST", Path: "/api/v1/compliance/rules", Handler: h.CreateRule, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PATCH", Path: "/api/v1/compliance/rules/{id}", Handler: h.UpdateRule, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "DELETE", Path: "/api/v1/compliance/rules/{id}", Handler: h.DeleteRule, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/compliance/rules/actions/restore-builtin", Handler: h.RestoreBuiltinRules},
		{Method: "GET", Path: "/api/v1/compliance/events", Handler: h.ListEvents, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/compliance/events/{id}", Handler: h.GetEvent, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
	}
	router.Register(mux, routes, router.RouteDeps{JWTSecret: jwtSecret})
}

func (h *Handler) ListRules(w http.ResponseWriter, r *http.Request) {
	var tenantID int64 = 0
	if claims := middleware.GetUserClaims(r.Context()); claims != nil {
		resolved, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
		if err != nil {
			httputil.WriteBadRequest(w, err.Error())
			return
		}
		tenantID = resolved
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope != "" && !isValidScope(scope) {
		httputil.WriteBadRequest(w, "invalid scope")
		return
	}
	rules, err := h.service.ListRules(r.Context(), tenantID, scope)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"rules": rules})
}

func (h *Handler) GetRule(w http.ResponseWriter, r *http.Request) {
	var tenantID int64 = 0
	if claims := middleware.GetUserClaims(r.Context()); claims != nil {
		resolved, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
		if err != nil {
			httputil.WriteBadRequest(w, err.Error())
			return
		}
		tenantID = resolved
	}
	rule, err := h.service.GetRule(r.Context(), tenantID, strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, rule)
}

func (h *Handler) CreateRule(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	var req CreateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	rule, err := h.service.CreateRule(r.Context(), tenantID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, rule)
}

func (h *Handler) UpdateRule(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	var req UpdateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	rule, err := h.service.UpdateRule(r.Context(), tenantID, strings.TrimSpace(r.PathValue("id")), req)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.WriteNotFound(w, err.Error())
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, rule)
}

func (h *Handler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if err := h.service.DeleteRule(r.Context(), tenantID, strings.TrimSpace(r.PathValue("id"))); err != nil {
		if strings.Contains(err.Error(), "not found") {
			httputil.WriteNotFound(w, err.Error())
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"ok": true})
}

func (h *Handler) RestoreBuiltinRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.service.RestoreBuiltinRules(r.Context())
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"rules": rules})
}

func (h *Handler) ListEvents(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	events, err := h.service.ListEvents(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"events": events})
}

func (h *Handler) GetEvent(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	event, err := h.service.GetEvent(r.Context(), tenantID, strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, event)
}
