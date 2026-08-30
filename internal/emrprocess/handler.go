package emrprocess

import (
	"net/http"
	"strings"

	"github.com/freeasyman/lingce-api/internal/emrpermission"
	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service     *Service
	permissions *emrpermission.Service
}

func NewHandler(service *Service, permissions *emrpermission.Service) *Handler {
	return &Handler{service: service, permissions: permissions}
}
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	router.Register(mux, []router.Route{{Method: "GET", Path: "/api/v1/emr/records/{id}/process-records", Handler: h.List, Auth: true, AllowedUserTypes: []string{"admin", "employee"}}}, router.RouteDeps{JWTSecret: jwtSecret})
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	recordID := strings.TrimSpace(r.PathValue("id"))
	if recordID == "" {
		httputil.WriteBadRequest(w, "invalid record id")
		return
	}
	access, err := h.permissions.Authorize(r.Context(), claims, tenantID, "record.history.read")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	allowed, err := h.permissions.CanAccessRecord(r.Context(), access, recordID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if !allowed {
		httputil.WriteForbidden(w, "emr record access denied")
		return
	}
	items, err := h.service.List(r.Context(), tenantID, recordID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}
