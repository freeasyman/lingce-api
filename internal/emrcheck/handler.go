package emrcheck

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
		{Method: "POST", Path: "/api/v1/emr/records/{id}/checks", Handler: h.RunManual, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/emr/records/{id}/checks", Handler: h.ListRuns, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/emr/records/{id}/checks/{check_id}", Handler: h.GetRun, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}, router.RouteDeps{JWTSecret: jwtSecret})
}

func (h *Handler) RunManual(w http.ResponseWriter, r *http.Request) {
	tenantID, actorID, err := h.authorizeRecord(r, "record.edit")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	recordID := strings.TrimSpace(r.PathValue("id"))
	if recordID == "" {
		httputil.WriteBadRequest(w, "invalid record id")
		return
	}
	item, err := h.service.RunManual(r.Context(), tenantID, actorID, recordID)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}

func (h *Handler) tenant(r *http.Request) (int64, error) {
	claims := middleware.GetUserClaims(r.Context())
	return tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
}

func (h *Handler) authorizeRecord(r *http.Request, ability string) (int64, int64, error) {
	claims := middleware.GetUserClaims(r.Context())
	tenantID, err := h.tenant(r)
	if err != nil {
		return 0, 0, err
	}
	access, err := h.permissions.Authorize(r.Context(), claims, tenantID, ability)
	if err != nil {
		return 0, 0, err
	}
	recordID := strings.TrimSpace(r.PathValue("id"))
	allowed, err := h.permissions.CanAccessRecord(r.Context(), access, recordID)
	if err != nil {
		return 0, 0, err
	}
	if !allowed {
		return 0, 0, fmt.Errorf("emr record access denied")
	}
	return tenantID, access.UserID, nil
}

func (h *Handler) ListRuns(w http.ResponseWriter, r *http.Request) {
	tenantID, _, err := h.authorizeRecord(r, "record.read")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	recordID := strings.TrimSpace(r.PathValue("id"))
	if recordID == "" {
		httputil.WriteBadRequest(w, "invalid record id")
		return
	}
	items, err := h.service.ListRuns(r.Context(), tenantID, recordID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

func (h *Handler) GetRun(w http.ResponseWriter, r *http.Request) {
	tenantID, _, err := h.authorizeRecord(r, "record.read")
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	recordID := strings.TrimSpace(r.PathValue("id"))
	checkID := strings.TrimSpace(r.PathValue("check_id"))
	if recordID == "" || checkID == "" {
		httputil.WriteBadRequest(w, "invalid check run id")
		return
	}
	item, err := h.service.GetRun(r.Context(), tenantID, recordID, checkID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if item == nil {
		httputil.WriteNotFound(w, "check run not found")
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}
