package emrrule

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

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	routes := []router.Route{
		{Method: "POST", Path: "/api/v1/emr/records/{id}/rules/run", Handler: h.RunRecordRules, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/emr/records/{id}/rules/hits", Handler: h.ListRecordHits, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/emr/records/{id}/rules/summary", Handler: h.GetRecordSummary, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "POST", Path: "/api/v1/emr/records/{id}/rules/hits/{hit_id}/actions", Handler: h.MarkDoctorAction, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}
	router.Register(mux, routes, router.RouteDeps{JWTSecret: jwtSecret})
}

func (h *Handler) RunRecordRules(w http.ResponseWriter, r *http.Request) {
	tenantID, recordID, ok := resolveTenantAndRecord(w, r)
	if !ok {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	var req RunRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	result, err := h.service.RunRecordRules(r.Context(), tenantID, recordID, claims.UserID, req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if result == nil {
		httputil.WriteNotFound(w, "record not found")
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": result})
}

func (h *Handler) ListRecordHits(w http.ResponseWriter, r *http.Request) {
	tenantID, recordID, ok := resolveTenantAndRecord(w, r)
	if !ok {
		return
	}
	activeOnly := strings.TrimSpace(r.URL.Query().Get("active")) != "false"
	hits, err := h.service.ListRecordHits(r.Context(), tenantID, recordID, activeOnly)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": hits})
}

func (h *Handler) GetRecordSummary(w http.ResponseWriter, r *http.Request) {
	tenantID, recordID, ok := resolveTenantAndRecord(w, r)
	if !ok {
		return
	}
	summary, err := h.service.GetRecordSummary(r.Context(), tenantID, recordID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": summary})
}

func (h *Handler) MarkDoctorAction(w http.ResponseWriter, r *http.Request) {
	tenantID, recordID, ok := resolveTenantAndRecord(w, r)
	if !ok {
		return
	}
	claims := middleware.GetUserClaims(r.Context())
	hitID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("hit_id")), 10, 64)
	if err != nil || hitID <= 0 {
		httputil.WriteBadRequest(w, "invalid hit id")
		return
	}
	var req DoctorActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	if err := h.service.MarkDoctorAction(r.Context(), tenantID, recordID, hitID, claims.UserID, string(claims.UserType), req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"ok": true})
}

func resolveTenantAndRecord(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return 0, 0, false
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return 0, 0, false
	}
	recordID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || recordID <= 0 {
		httputil.WriteBadRequest(w, "invalid record id")
		return 0, 0, false
	}
	return tenantID, recordID, true
}
