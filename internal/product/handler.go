package product

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
		{Method: "GET", Path: "/api/v1/products", Handler: h.List, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/products/{id}", Handler: h.Get, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "POST", Path: "/api/v1/products", Handler: h.Create, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "PUT", Path: "/api/v1/products/{id}", Handler: h.Update, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "DELETE", Path: "/api/v1/products/{id}", Handler: h.Delete, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
	}
	router.Register(mux, routes, router.RouteDeps{JWTSecret: jwtSecret})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return
	}

	var tenantID int64
	if claims.UserType == auth.UserTypeAdmin {
		if tidStr := r.URL.Query().Get("tenant_id"); tidStr != "" {
			tenantID, _ = strconv.ParseInt(tidStr, 10, 64)
		}
	} else if claims.TenantID != nil {
		tenantID = *claims.TenantID
	}

	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("page_size"))
	var isTemplate *bool
	if raw := q.Get("is_template"); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err == nil {
			isTemplate = &value
		}
	}
	req := ListRequest{
		TenantID:   tenantID,
		Industry:   q.Get("industry"),
		Status:     q.Get("status"),
		Keyword:    q.Get("keyword"),
		IsTemplate: isTemplate,
		Page:       page,
		PageSize:   pageSize,
	}
	items, total, err := h.service.List(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if items == nil {
		items = []*Product{}
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
		httputil.WriteNotFound(w, "product not found")
		return
	}
	tenantID, ok := h.resolveWritableTenantID(w, r, false)
	if !ok {
		return
	}
	if tenantID > 0 && item.TenantID != tenantID {
		httputil.WriteNotFound(w, "product not found")
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveWritableTenantID(w, r, true)
	if !ok {
		return
	}
	var req CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	req.TenantID = tenantID
	item, err := h.service.Create(r.Context(), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, item)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveWritableTenantID(w, r, true)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	existing, err := h.service.GetByID(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if existing == nil || existing.TenantID != tenantID {
		httputil.WriteNotFound(w, "product not found")
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

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.resolveWritableTenantID(w, r, true)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid id")
		return
	}
	existing, err := h.service.GetByID(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if existing == nil || existing.TenantID != tenantID {
		httputil.WriteNotFound(w, "product not found")
		return
	}
	if err := h.service.Delete(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"status": "deleted"})
}

func (h *Handler) resolveWritableTenantID(w http.ResponseWriter, r *http.Request, required bool) (int64, bool) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return 0, false
	}
	if claims.UserType == auth.UserTypeAdmin {
		tidStr := r.URL.Query().Get("tenant_id")
		if tidStr == "" {
			if required {
				httputil.WriteBadRequest(w, "tenant_id is required")
				return 0, false
			}
			return 0, true
		}
		tid, err := strconv.ParseInt(tidStr, 10, 64)
		if err != nil || tid < 0 {
			httputil.WriteBadRequest(w, "invalid tenant_id")
			return 0, false
		}
		if required && tid == 0 {
			httputil.WriteBadRequest(w, "tenant_id is required")
			return 0, false
		}
		return tid, true
	}
	if claims.TenantID == nil || *claims.TenantID <= 0 {
		httputil.WriteUnauthorized(w, "missing tenant scope")
		return 0, false
	}
	return *claims.TenantID, true
}
