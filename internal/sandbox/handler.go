package sandbox

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
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
	authMw := middleware.Auth(jwtSecret)
	mux.Handle("GET /api/v1/sandbox/recordings/search", authMw(http.HandlerFunc(h.SearchRecordings)))
	mux.Handle("GET /api/v1/sandbox/recordings/estimate", authMw(http.HandlerFunc(h.EstimateTransfer)))
	mux.Handle("POST /api/v1/sandbox/recordings/tasks", authMw(http.HandlerFunc(h.CreateTask)))
	mux.Handle("GET /api/v1/sandbox/recordings/tasks", authMw(http.HandlerFunc(h.ListTasks)))
	mux.Handle("GET /api/v1/sandbox/recordings/tasks/{taskId}", authMw(http.HandlerFunc(h.GetTask)))
	mux.Handle("GET /api/v1/sandbox/recordings/tasks/{taskId}/items", authMw(http.HandlerFunc(h.ListTaskItems)))
	mux.Handle("POST /api/v1/sandbox/recordings/tasks/{taskId}/rollback", authMw(http.HandlerFunc(h.RollbackTask)))
	mux.Handle("GET /api/v1/sandbox/recordings/employee-mappings", authMw(http.HandlerFunc(h.ListEmployeeMappings)))
	mux.Handle("POST /api/v1/sandbox/recordings/employee-mappings", authMw(http.HandlerFunc(h.CreateEmployeeMapping)))
	mux.Handle("DELETE /api/v1/sandbox/recordings/employee-mappings/{id}", authMw(http.HandlerFunc(h.DeleteEmployeeMapping)))
}

func (h *Handler) ensureAdmin(w http.ResponseWriter, r *http.Request) (*auth.Claims, bool) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return nil, false
	}
	if claims.UserType != auth.UserTypeAdmin {
		httputil.WriteForbidden(w, "Admin access required")
		return nil, false
	}
	return claims, true
}

func (h *Handler) SearchRecordings(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ensureAdmin(w, r); !ok {
		return
	}
	sourceTenantID, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("source_tenant_id")), 10, 64)
	if sourceTenantID <= 0 {
		httputil.WriteBadRequest(w, "source_tenant_id is required")
		return
	}
	page := parseIntDefault(r.URL.Query().Get("page"), 1)
	pageSize := parseIntDefault(r.URL.Query().Get("page_size"), 20)
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}
	req := SearchRequest{
		SourceTenantID: sourceTenantID,
		Keyword:        strings.TrimSpace(r.URL.Query().Get("keyword")),
		TranscriptQ:    strings.TrimSpace(r.URL.Query().Get("transcript_query")),
		EmployeeIDs:    parseInt64ListFromQuery(r.URL.Query()["employee_ids"], r.URL.Query().Get("employee_ids")),
		IncludeShort:   false,
		Page:           page,
		PageSize:       pageSize,
	}
	if includeShortStr := strings.TrimSpace(r.URL.Query().Get("include_short")); includeShortStr != "" {
		req.IncludeShort = includeShortStr == "true" || includeShortStr == "1"
	}
	if v := strings.TrimSpace(r.URL.Query().Get("date_from")); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			req.DateFrom = &t
		}
	}
	if v := strings.TrimSpace(r.URL.Query().Get("date_to")); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			t = t.Add(24*time.Hour - time.Second)
			req.DateTo = &t
		}
	}
	items, total, err := h.service.SearchRecordings(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WritePaginated(w, items, total, page, pageSize)
}

func (h *Handler) EstimateTransfer(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ensureAdmin(w, r); !ok {
		return
	}
	sourceTenantID, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("source_tenant_id")), 10, 64)
	recordingIDs := parseInt64ListFromQuery(r.URL.Query()["recording_ids"], r.URL.Query().Get("recording_ids"))
	data, err := h.service.Estimate(r.Context(), sourceTenantID, recordingIDs)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, data)
}

func (h *Handler) CreateTask(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.ensureAdmin(w, r)
	if !ok {
		return
	}
	var req CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid json body")
		return
	}
	task, err := h.service.CreateTask(r.Context(), claims.UserID, req)
	if err != nil {
		if IsValidationError(err) {
			httputil.WriteBadRequest(w, err.Error())
			return
		}
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, task)
}

func (h *Handler) ListTasks(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ensureAdmin(w, r); !ok {
		return
	}
	sourceTenantID, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("source_tenant_id")), 10, 64)
	targetTenantID, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("target_tenant_id")), 10, 64)
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	page := parseIntDefault(r.URL.Query().Get("page"), 1)
	pageSize := parseIntDefault(r.URL.Query().Get("page_size"), 20)
	items, total, err := h.service.ListTasks(r.Context(), sourceTenantID, targetTenantID, status, page, pageSize)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WritePaginated(w, items, total, page, pageSize)
}

func (h *Handler) GetTask(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ensureAdmin(w, r); !ok {
		return
	}
	taskID, _ := strconv.ParseInt(r.PathValue("taskId"), 10, 64)
	if taskID <= 0 {
		httputil.WriteBadRequest(w, "invalid task id")
		return
	}
	task, err := h.service.GetTaskByID(r.Context(), taskID)
	if err != nil {
		httputil.WriteNotFound(w, "task not found")
		return
	}
	httputil.WriteSuccess(w, task)
}

func (h *Handler) ListTaskItems(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ensureAdmin(w, r); !ok {
		return
	}
	taskID, _ := strconv.ParseInt(r.PathValue("taskId"), 10, 64)
	if taskID <= 0 {
		httputil.WriteBadRequest(w, "invalid task id")
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	page := parseIntDefault(r.URL.Query().Get("page"), 1)
	pageSize := parseIntDefault(r.URL.Query().Get("page_size"), 20)
	items, total, err := h.service.ListTaskItems(r.Context(), taskID, status, page, pageSize)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WritePaginated(w, items, total, page, pageSize)
}

func (h *Handler) RollbackTask(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.ensureAdmin(w, r)
	if !ok {
		return
	}
	taskID, _ := strconv.ParseInt(r.PathValue("taskId"), 10, 64)
	if taskID <= 0 {
		httputil.WriteBadRequest(w, "invalid task id")
		return
	}
	if err := h.service.RollbackTask(r.Context(), taskID, claims.UserID); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"ok": true})
}

func (h *Handler) ListEmployeeMappings(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ensureAdmin(w, r); !ok {
		return
	}
	sourceTenantID, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("source_tenant_id")), 10, 64)
	targetTenantID, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("target_tenant_id")), 10, 64)
	if targetTenantID <= 0 {
		httputil.WriteBadRequest(w, "target_tenant_id is required")
		return
	}
	items, err := h.service.ListEmployeeMappings(r.Context(), sourceTenantID, targetTenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, items)
}

func (h *Handler) CreateEmployeeMapping(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ensureAdmin(w, r); !ok {
		return
	}
	var req EmployeeMapping
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid json body")
		return
	}
	if req.SourceTenantID <= 0 || req.SourceEmployeeID <= 0 || req.TargetTenantID <= 0 || req.TargetEmployeeID <= 0 {
		httputil.WriteBadRequest(w, "source/target tenant_id and employee_id are required")
		return
	}
	item, err := h.service.CreateEmployeeMapping(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) DeleteEmployeeMapping(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.ensureAdmin(w, r); !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if id <= 0 {
		httputil.WriteBadRequest(w, "invalid mapping id")
		return
	}
	if err := h.service.DeleteEmployeeMapping(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"ok": true})
}

func parseIntDefault(raw string, fallback int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func parseInt64ListFromQuery(values []string, single string) []int64 {
	chunks := make([]string, 0, len(values)+1)
	chunks = append(chunks, values...)
	if strings.TrimSpace(single) != "" {
		chunks = append(chunks, single)
	}
	out := make([]int64, 0)
	seen := make(map[int64]struct{})
	for _, raw := range chunks {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			v, err := strconv.ParseInt(part, 10, 64)
			if err != nil || v <= 0 {
				continue
			}
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}
