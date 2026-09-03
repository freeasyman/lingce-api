package complianceguard

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	store *Store
	pool  *pgxpool.Pool
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{store: NewStore(pool), pool: pool}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	router.Register(mux, []router.Route{
		{
			Method: "GET", Path: "/api/v1/compliance-guard/sources/status",
			Handler: h.GetSourceStatus, Auth: true,
			AllowedUserTypes: []string{"admin", "employee", "mobile"},
		},
		{
			Method: "GET", Path: "/api/v1/compliance-guard/sources",
			Handler: h.ListSources, Auth: true,
			AllowedUserTypes: []string{"admin", "employee", "mobile"},
		},
		{
			Method: "POST", Path: "/api/v1/compliance-guard/sources/sync",
			Handler: h.SyncSources, Auth: true,
			AllowedUserTypes: []string{"admin", "employee"},
		},
		{
			Method: "GET", Path: "/api/v1/compliance-guard/findings",
			Handler: h.ListFindings, Auth: true,
			AllowedUserTypes: []string{"admin", "employee", "mobile"},
		},
		{
			Method: "GET", Path: "/api/v1/compliance-guard/findings/{id}",
			Handler: h.GetFinding, Auth: true,
			AllowedUserTypes: []string{"admin", "employee", "mobile"},
		},
	}, router.RouteDeps{JWTSecret: jwtSecret})
}

func (h *Handler) SyncSources(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	scope, err := tenancy.ResolveScope(r.Context(), h.pool, claims, strings.TrimSpace(r.URL.Query().Get("tenant_id")))
	if err != nil {
		if err.Error() == "no tenant access" || err.Error() == "access denied" {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if scope.TenantID == nil || *scope.TenantID <= 0 {
		httputil.WriteBadRequest(w, "tenant_id is required for compliance guard source sync")
		return
	}
	result, err := h.store.SyncSources(r.Context(), *scope.TenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, result)
}

func (h *Handler) GetSourceStatus(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	scope, err := tenancy.ResolveScope(r.Context(), h.pool, claims, strings.TrimSpace(r.URL.Query().Get("tenant_id")))
	if err != nil {
		if err.Error() == "no tenant access" || err.Error() == "access denied" {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if scope.TenantID == nil || *scope.TenantID <= 0 {
		httputil.WriteBadRequest(w, "tenant_id is required for compliance guard source status")
		return
	}

	status, err := h.store.GetSourceStatus(r.Context(), *scope.TenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, status)
}

func (h *Handler) ListSources(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	scope, err := tenancy.ResolveScope(r.Context(), h.pool, claims, strings.TrimSpace(r.URL.Query().Get("tenant_id")))
	if err != nil {
		if err.Error() == "no tenant access" || err.Error() == "access denied" {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if scope.TenantID == nil || *scope.TenantID <= 0 {
		httputil.WriteBadRequest(w, "tenant_id is required for compliance guard sources")
		return
	}

	page := 1
	pageSize := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		if _, err := fmt.Sscanf(raw, "%d", &page); err != nil || page < 1 {
			page = 1
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("page_size")); raw != "" {
		if _, err := fmt.Sscanf(raw, "%d", &pageSize); err != nil || pageSize < 1 || pageSize > 100 {
			pageSize = 20
		}
	}

	items, total, err := h.store.ListSources(r.Context(), *scope.TenantID, page, pageSize)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WritePaginated(w, items, total, page, pageSize)
}

func (h *Handler) ListFindings(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	scope, err := tenancy.ResolveScope(r.Context(), h.pool, claims, strings.TrimSpace(r.URL.Query().Get("tenant_id")))
	if err != nil {
		if err.Error() == "no tenant access" || err.Error() == "access denied" {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if scope.TenantID == nil || *scope.TenantID <= 0 {
		httputil.WriteBadRequest(w, "tenant_id is required for compliance guard findings")
		return
	}
	page := parsePositiveQueryInt(r.URL.Query().Get("page"), 1)
	pageSize := parsePositiveQueryInt(r.URL.Query().Get("page_size"), 20)
	if pageSize > 100 {
		pageSize = 100
	}
	items, total, err := h.store.ListFindings(r.Context(), *scope.TenantID,
		strings.TrimSpace(r.URL.Query().Get("subject")), strings.TrimSpace(r.URL.Query().Get("status")), page, pageSize)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WritePaginated(w, items, total, page, pageSize)
}

func (h *Handler) GetFinding(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	scope, err := tenancy.ResolveScope(r.Context(), h.pool, claims, strings.TrimSpace(r.URL.Query().Get("tenant_id")))
	if err != nil {
		if err.Error() == "no tenant access" || err.Error() == "access denied" {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if scope.TenantID == nil || *scope.TenantID <= 0 {
		httputil.WriteBadRequest(w, "tenant_id is required for compliance guard finding")
		return
	}
	item, err := h.store.GetFinding(r.Context(), *scope.TenantID, strings.TrimSpace(r.PathValue("id")))
	if err == pgx.ErrNoRows {
		httputil.WriteNotFound(w, "finding not found")
		return
	}
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func parsePositiveQueryInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	var value int
	if _, err := fmt.Sscanf(raw, "%d", &value); err != nil || value < 1 {
		return fallback
	}
	return value
}
