package content

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

func (h *Handler) registerAccountRoutes(mux *http.ServeMux, deps router.RouteDeps) {
	router.Register(mux, []router.Route{
		{Method: http.MethodGet, Path: "/api/v1/content-accounts", Handler: h.ListContentAccounts, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: http.MethodPost, Path: "/api/v1/content-accounts", Handler: h.CreateContentAccount, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: http.MethodGet, Path: "/api/v1/content-accounts/{id}", Handler: h.GetContentAccount, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: http.MethodPut, Path: "/api/v1/content-accounts/{id}", Handler: h.UpdateContentAccount, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: http.MethodDelete, Path: "/api/v1/content-accounts/{id}", Handler: h.DeleteContentAccount, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: http.MethodGet, Path: "/api/v1/content-accounts/{id}/columns", Handler: h.ListAccountColumns, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: http.MethodPost, Path: "/api/v1/content-accounts/{id}/columns", Handler: h.CreateAccountColumn, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: http.MethodGet, Path: "/api/v1/content-columns", Handler: h.ListContentColumns, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: http.MethodPut, Path: "/api/v1/content-columns/{id}", Handler: h.UpdateContentColumn, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: http.MethodDelete, Path: "/api/v1/content-columns/{id}", Handler: h.DeleteContentColumn, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: http.MethodGet, Path: "/api/v1/content-plans", Handler: h.ListContentPlans, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: http.MethodPost, Path: "/api/v1/content-plans/weeks/{week_start}/generate", Handler: h.GenerateContentPlanWeek, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: http.MethodPut, Path: "/api/v1/content-plans/slots/{id}", Handler: h.UpdateContentPlanSlot, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: http.MethodPost, Path: "/api/v1/content-plans/slots/{id}/assign-content", Handler: h.AssignContentToPlanSlot, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}, deps)
}

func (h *Handler) ListContentAccounts(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireContentTenant(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListContentAccounts(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, items)
}

func (h *Handler) GetContentAccount(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireContentTenant(w, r)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	item, err := h.service.GetContentAccount(r.Context(), tenantID, id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) CreateContentAccount(w http.ResponseWriter, r *http.Request) {
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
	var req CreateContentAccountRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	item, err := h.service.CreateContentAccount(r.Context(), tenantID, claims.UserID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) UpdateContentAccount(w http.ResponseWriter, r *http.Request) {
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
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req UpdateContentAccountRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	item, err := h.service.UpdateContentAccount(r.Context(), tenantID, id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) DeleteContentAccount(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireContentTenant(w, r)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := h.service.DeleteContentAccount(r.Context(), tenantID, id); err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"status": "deleted"})
}

func (h *Handler) ListAccountColumns(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireContentTenant(w, r)
	if !ok {
		return
	}
	accountID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	items, err := h.service.ListContentColumns(r.Context(), tenantID, accountID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, items)
}

func (h *Handler) ListContentColumns(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireContentTenant(w, r)
	if !ok {
		return
	}
	accountID, _ := strconv.ParseInt(r.URL.Query().Get("account_id"), 10, 64)
	items, err := h.service.ListContentColumns(r.Context(), tenantID, accountID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, items)
}

func (h *Handler) CreateAccountColumn(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireContentTenant(w, r)
	if !ok {
		return
	}
	accountID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req CreateContentColumnRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	item, err := h.service.CreateContentColumn(r.Context(), tenantID, accountID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) UpdateContentColumn(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireContentTenant(w, r)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req UpdateContentColumnRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	item, err := h.service.UpdateContentColumn(r.Context(), tenantID, id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) DeleteContentColumn(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireContentTenant(w, r)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := h.service.DeleteContentColumn(r.Context(), tenantID, id); err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"status": "deleted"})
}

func (h *Handler) ListContentPlans(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireContentTenant(w, r)
	if !ok {
		return
	}
	weekStart := strings.TrimSpace(r.URL.Query().Get("week_start"))
	if weekStart == "" {
		httputil.WriteBadRequest(w, "week_start is required")
		return
	}
	items, err := h.service.ListContentPlanSlots(r.Context(), tenantID, weekStart)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, items)
}

func (h *Handler) GenerateContentPlanWeek(w http.ResponseWriter, r *http.Request) {
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
	weekStart := strings.TrimSpace(r.PathValue("week_start"))
	items, err := h.service.GenerateContentPlanWeek(r.Context(), tenantID, claims.UserID, weekStart)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, items)
}

func (h *Handler) UpdateContentPlanSlot(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireContentTenant(w, r)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		TargetCount *int    `json:"target_count"`
		Status      *string `json:"status"`
		Notes       *string `json:"notes"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	item, err := h.service.UpdateContentPlanSlot(r.Context(), tenantID, id, req.TargetCount, req.Status, req.Notes)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) AssignContentToPlanSlot(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireContentTenant(w, r)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		ContentID int64 `json:"content_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if err := h.service.AssignContentToPlanSlot(r.Context(), tenantID, id, req.ContentID); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"status": "assigned", "slot_id": id, "content_id": req.ContentID})
}

func requireContentTenant(w http.ResponseWriter, r *http.Request) (int64, bool) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return 0, false
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return 0, false
	}
	return tenantID, true
}
