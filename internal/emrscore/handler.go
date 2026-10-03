package emrscore

import (
	"fmt"
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
	router.Register(mux, []router.Route{
		{Method: "GET", Path: "/api/v1/emr/records/{id}/score-current", Handler: h.Current, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}, router.RouteDeps{JWTSecret: jwtSecret})
}

func (h *Handler) authorizeRecord(r *http.Request) (int64, string, error) {
	claims := middleware.GetUserClaims(r.Context())
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		return 0, "", err
	}
	access, err := h.permissions.Authorize(r.Context(), claims, tenantID, "record.read")
	if err != nil {
		return 0, "", err
	}
	recordID := strings.TrimSpace(r.PathValue("id"))
	if recordID == "" {
		return 0, "", fmt.Errorf("invalid record id")
	}
	allowed, err := h.permissions.CanAccessRecord(r.Context(), access, recordID)
	if err != nil {
		return 0, "", err
	}
	if !allowed {
		return 0, "", fmt.Errorf("emr record access denied")
	}
	return tenantID, recordID, nil
}

func (h *Handler) Current(w http.ResponseWriter, r *http.Request) {
	tenantID, recordID, err := h.authorizeRecord(r)
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	item, err := h.service.Current(r.Context(), tenantID, recordID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if item == nil {
		httputil.WriteNotFound(w, "current score not found")
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}
