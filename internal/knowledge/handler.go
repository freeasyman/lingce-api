package knowledge

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

func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	routes := []router.Route{
		{Method: "GET", Path: "/api/v1/knowledge-items", Handler: h.List, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/knowledge-items/{id}", Handler: h.Get, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "POST", Path: "/api/v1/knowledge-items", Handler: h.Create, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "PUT", Path: "/api/v1/knowledge-items/{id}", Handler: h.Update, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "PATCH", Path: "/api/v1/knowledge-items/{id}/status", Handler: h.UpdateStatus, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "POST", Path: "/api/v1/knowledge-items/batch-status", Handler: h.BatchUpdateStatus, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "DELETE", Path: "/api/v1/knowledge-items/{id}", Handler: h.Delete, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
	}
	router.Register(mux, routes, router.RouteDeps{JWTSecret: jwtSecret})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	// For List, admin can pass tenant_id=0 to query all tenants
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}

	var tenantID int64
	if claims.UserType == auth.UserTypeAdmin {
		tidStr := r.URL.Query().Get("tenant_id")
		if tidStr != "" {
			tid, _ := strconv.ParseInt(tidStr, 10, 64)
			tenantID = tid // 0 means all tenants
		}
	} else {
		if claims.TenantID != nil {
			tenantID = *claims.TenantID
		}
	}

	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("page_size"))
	req := ListRequest{
		TenantID:    tenantID,
		Scope:       q.Get("scope"),
		Category:    q.Get("category"),
		Status:      q.Get("status"),
		ProductName: q.Get("product_name"),
		Keyword:     q.Get("keyword"),
		SourceType:  q.Get("source_type"),
		Page:        page,
		PageSize:    pageSize,
	}

	items, total, err := h.service.List(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if items == nil {
		items = []*KnowledgeItem{}
	}
	httputil.WritePaginated(w, items, int64(total), req.Page, req.PageSize)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}

	item, err := h.service.GetByID(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if item == nil {
		httputil.WriteNotFound(w, "knowledge item not found")
		return
	}

	// Tenant isolation check
	tenantID := h.resolveTenantID(w, r)
	if tenantID == 0 {
		return
	}
	if item.TenantID != tenantID {
		httputil.WriteNotFound(w, "knowledge item not found")
		return
	}

	httputil.WriteSuccess(w, item)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveTenantID(w, r)
	if tenantID == 0 {
		return
	}

	var req CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	req.TenantID = tenantID

	if req.SourceType == "" {
		req.SourceType = SourceManual
	}

	item, err := h.service.Create(r.Context(), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, item)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveTenantID(w, r)
	if tenantID == 0 {
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}

	// Check ownership
	existing, err := h.service.GetByID(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if existing == nil || existing.TenantID != tenantID {
		httputil.WriteNotFound(w, "knowledge item not found")
		return
	}

	var req UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}

	item, err := h.service.Update(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveTenantID(w, r)
	if tenantID == 0 {
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}

	// Check ownership
	existing, err := h.service.GetByID(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if existing == nil || existing.TenantID != tenantID {
		httputil.WriteNotFound(w, "knowledge item not found")
		return
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}

	if err := h.service.UpdateStatus(r.Context(), id, body.Status); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"status": "ok"})
}

func (h *Handler) BatchUpdateStatus(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveTenantID(w, r)
	if tenantID == 0 {
		return
	}

	var req BatchStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}

	affected, err := h.service.BatchUpdateStatus(r.Context(), req.IDs, req.Status)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]int64{"affected": affected})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID := h.resolveTenantID(w, r)
	if tenantID == 0 {
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}

	// Check ownership
	existing, err := h.service.GetByID(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if existing == nil || existing.TenantID != tenantID {
		httputil.WriteNotFound(w, "knowledge item not found")
		return
	}

	if err := h.service.Delete(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"status": "deleted"})
}

// resolveTenantID extracts tenant_id from JWT claims or query param (admin)
func (h *Handler) resolveTenantID(w http.ResponseWriter, r *http.Request) int64 {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return 0
	}

	if claims.UserType == auth.UserTypeAdmin {
		tidStr := r.URL.Query().Get("tenant_id")
		if tidStr == "" {
			httputil.WriteBadRequest(w, "tenant_id is required for admin")
			return 0
		}
		tid, _ := strconv.ParseInt(tidStr, 10, 64)
		if tid <= 0 {
			httputil.WriteBadRequest(w, "invalid tenant_id")
			return 0
		}
		return tid
	}

	if claims.TenantID == nil {
		httputil.WriteForbidden(w, "no tenant access")
		return 0
	}
	return *claims.TenantID
}
