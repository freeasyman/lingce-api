package recording

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
	"github.com/jackc/pgx/v5"
)

func (h *Handler) RegisterAnalysisRoutes(mux *http.ServeMux, jwtSecret string) {
	authMw := middleware.Auth(jwtSecret)
	mux.Handle("GET /api/v1/analysis/routes/options/roles", authMw(http.HandlerFunc(h.ListAnalysisRoleOptions)))
	mux.Handle("GET /api/v1/analysis/routes/options/pipelines", authMw(http.HandlerFunc(h.ListAnalysisPipelineOptions)))
	mux.Handle("GET /api/v1/analysis/routes", authMw(http.HandlerFunc(h.ListAnalysisRoutes)))
	mux.Handle("GET /api/v1/analysis/routes/{id}", authMw(http.HandlerFunc(h.GetAnalysisRoute)))
	mux.Handle("POST /api/v1/analysis/routes", authMw(http.HandlerFunc(h.CreateAnalysisRoute)))
	mux.Handle("PUT /api/v1/analysis/routes/{id}", authMw(http.HandlerFunc(h.UpdateAnalysisRoute)))
	mux.Handle("POST /api/v1/analysis/routes/{id}/publish", authMw(http.HandlerFunc(h.PublishAnalysisRoute)))
	mux.Handle("POST /api/v1/analysis/routes/{id}/rollback", authMw(http.HandlerFunc(h.RollbackAnalysisRoute)))
	mux.Handle("GET /api/v1/analysis/runs", authMw(http.HandlerFunc(h.ListAnalysisRuns)))
	mux.Handle("GET /api/v1/analysis/runs/{id}", authMw(http.HandlerFunc(h.GetAnalysisRun)))
	mux.Handle("GET /api/v1/analysis/runs/{id}/steps", authMw(http.HandlerFunc(h.ListAnalysisRunSteps)))
}

func (h *Handler) ListAnalysisRoleOptions(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolveAnalysisScope(w, r)
	if !ok {
		return
	}
	items, err := h.service.ListAnalysisRoleOptions(r.Context(), scope.TenantIDs)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, items)
}

func (h *Handler) ListAnalysisPipelineOptions(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.resolveAnalysisScope(w, r); !ok {
		return
	}
	items, err := h.service.ListAnalysisPipelineOptions(r.Context())
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, items)
}

func (h *Handler) ListAnalysisRoutes(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolveAnalysisScope(w, r)
	if !ok {
		return
	}
	page := parsePositiveInt(r.URL.Query().Get("page"), 1)
	pageSize := parsePositiveInt(r.URL.Query().Get("page_size"), 20)
	var roleID *int64
	if raw := strings.TrimSpace(r.URL.Query().Get("role_id")); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v <= 0 {
			httputil.WriteBadRequest(w, "invalid role_id")
			return
		}
		roleID = &v
	}
	var enabled *bool
	if raw := strings.TrimSpace(r.URL.Query().Get("is_enabled")); raw != "" {
		v := raw == "1" || strings.EqualFold(raw, "true")
		enabled = &v
	}
	items, total, err := h.service.ListAnalysisRoutes(r.Context(), AnalysisRouteListRequest{
		TenantIDs: scope.TenantIDs,
		RoleID:    roleID,
		SceneCode: strings.TrimSpace(r.URL.Query().Get("scene_code")),
		Keyword:   strings.TrimSpace(r.URL.Query().Get("keyword")),
		Enabled:   enabled,
		Page:      page,
		PageSize:  pageSize,
	})
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WritePaginated(w, items, total, page, pageSize)
}

func (h *Handler) GetAnalysisRoute(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolveAnalysisScope(w, r)
	if !ok {
		return
	}
	id, ok := parsePathID(w, r.PathValue("id"), "route id")
	if !ok {
		return
	}
	item, err := h.service.GetAnalysisRoute(r.Context(), scope.TenantIDs, id)
	if err != nil {
		writeAnalysisError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) CreateAnalysisRoute(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolveAnalysisScope(w, r)
	if !ok {
		return
	}
	var req CreateAnalysisRouteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.service.CreateAnalysisRoute(r.Context(), scope.TenantIDs, middleware.GetUserID(r.Context()), req)
	if err != nil {
		writeAnalysisError(w, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, item)
}

func (h *Handler) UpdateAnalysisRoute(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolveAnalysisScope(w, r)
	if !ok {
		return
	}
	id, ok := parsePathID(w, r.PathValue("id"), "route id")
	if !ok {
		return
	}
	var req UpdateAnalysisRouteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.service.UpdateAnalysisRoute(r.Context(), scope.TenantIDs, middleware.GetUserID(r.Context()), id, req)
	if err != nil {
		writeAnalysisError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) PublishAnalysisRoute(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolveAnalysisScope(w, r)
	if !ok {
		return
	}
	id, ok := parsePathID(w, r.PathValue("id"), "route id")
	if !ok {
		return
	}
	var req PublishAnalysisRouteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.service.PublishAnalysisRoute(r.Context(), scope.TenantIDs, middleware.GetUserID(r.Context()), id, req)
	if err != nil {
		writeAnalysisError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) RollbackAnalysisRoute(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolveAnalysisScope(w, r)
	if !ok {
		return
	}
	id, ok := parsePathID(w, r.PathValue("id"), "route id")
	if !ok {
		return
	}
	item, err := h.service.RollbackAnalysisRoute(r.Context(), scope.TenantIDs, middleware.GetUserID(r.Context()), id)
	if err != nil {
		writeAnalysisError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) ListAnalysisRuns(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolveAnalysisScope(w, r)
	if !ok {
		return
	}
	page := parsePositiveInt(r.URL.Query().Get("page"), 1)
	pageSize := parsePositiveInt(r.URL.Query().Get("page_size"), 20)
	var recordingID *int64
	if raw := strings.TrimSpace(r.URL.Query().Get("recording_id")); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v <= 0 {
			httputil.WriteBadRequest(w, "invalid recording_id")
			return
		}
		recordingID = &v
	}
	items, total, err := h.service.ListAnalysisRuns(r.Context(), AnalysisRunListRequest{
		TenantIDs:     scope.TenantIDs,
		Status:        strings.TrimSpace(r.URL.Query().Get("status")),
		RoleCode:      strings.TrimSpace(r.URL.Query().Get("role_code")),
		PipelineCode:  strings.TrimSpace(r.URL.Query().Get("pipeline_code")),
		TriggerSource: strings.TrimSpace(r.URL.Query().Get("trigger_source")),
		RecordingID:   recordingID,
		TraceID:       strings.TrimSpace(r.URL.Query().Get("trace_id")),
		Keyword:       strings.TrimSpace(r.URL.Query().Get("keyword")),
		Page:          page,
		PageSize:      pageSize,
	})
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WritePaginated(w, items, total, page, pageSize)
}

func (h *Handler) GetAnalysisRun(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolveAnalysisScope(w, r)
	if !ok {
		return
	}
	id, ok := parsePathID(w, r.PathValue("id"), "run id")
	if !ok {
		return
	}
	item, err := h.service.GetAnalysisRun(r.Context(), scope.TenantIDs, id)
	if err != nil {
		writeAnalysisError(w, err)
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) ListAnalysisRunSteps(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolveAnalysisScope(w, r)
	if !ok {
		return
	}
	id, ok := parsePathID(w, r.PathValue("id"), "run id")
	if !ok {
		return
	}
	items, err := h.service.ListAnalysisRunSteps(r.Context(), scope.TenantIDs, id)
	if err != nil {
		writeAnalysisError(w, err)
		return
	}
	httputil.WriteSuccess(w, items)
}

func (h *Handler) resolveAnalysisScope(w http.ResponseWriter, r *http.Request) (*tenancy.Scope, bool) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return nil, false
	}
	scope, err := tenancy.ResolveScope(r.Context(), h.service.store.pool, claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		if err.Error() == "no tenant access" || err.Error() == "access denied" {
			httputil.WriteForbidden(w, err.Error())
		} else {
			httputil.WriteBadRequest(w, err.Error())
		}
		return nil, false
	}
	return scope, true
}

func parsePositiveInt(raw string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v <= 0 {
		return def
	}
	return v
}

func parsePathID(w http.ResponseWriter, raw, label string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "invalid "+label)
		return 0, false
	}
	return id, true
}

func writeAnalysisError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		httputil.WriteNotFound(w, "resource not found")
		return
	}
	message := err.Error()
	if strings.Contains(message, "access denied") {
		httputil.WriteForbidden(w, message)
		return
	}
	if strings.Contains(message, "required") || strings.Contains(message, "invalid") || strings.Contains(message, "not found") || strings.Contains(message, "no rollback target") {
		httputil.WriteBadRequest(w, message)
		return
	}
	httputil.WriteInternalError(w, message)
}
