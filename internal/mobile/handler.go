package mobile

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/recording"
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
		{Method: "GET", Path: "/api/v1/mobile/me", Handler: h.GetMe, Auth: true, AllowedUserTypes: []string{"employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/mobile/home", Handler: h.GetHome, Auth: true, AllowedUserTypes: []string{"employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/mobile/tasks", Handler: h.ListTasks, Auth: true, AllowedUserTypes: []string{"employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/mobile/tasks/{id}", Handler: h.GetTask, Auth: true, AllowedUserTypes: []string{"employee", "mobile"}},
		{Method: "POST", Path: "/api/v1/mobile/tasks/{id}/actions/complete", Handler: h.CompleteTask, Auth: true, AllowedUserTypes: []string{"employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/mobile/recordings", Handler: h.ListRecordings, Auth: true, AllowedUserTypes: []string{"employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/mobile/recordings/{id}", Handler: h.GetRecording, Auth: true, AllowedUserTypes: []string{"employee", "mobile"}},
	}
	router.Register(mux, routes, router.RouteDeps{JWTSecret: jwtSecret})
}

func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireEmployeeClaims(w, r)
	if !ok {
		return
	}
	resp, err := h.service.GetMe(r.Context(), claims)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

func (h *Handler) GetHome(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireEmployeeClaims(w, r)
	if !ok {
		return
	}
	resp, err := h.service.GetHome(r.Context(), claims)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

func (h *Handler) ListTasks(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireEmployeeClaims(w, r)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	params := TaskListParams{
		Page:      page,
		PageSize:  pageSize,
		Status:    strings.TrimSpace(r.URL.Query().Get("status")),
		Q:         strings.TrimSpace(r.URL.Query().Get("q")),
		Priority:  strings.TrimSpace(r.URL.Query().Get("priority")),
		TaskType:  strings.TrimSpace(r.URL.Query().Get("task_type")),
		DueBucket: strings.TrimSpace(r.URL.Query().Get("due_bucket")),
		DateFrom:  strings.TrimSpace(r.URL.Query().Get("date_from")),
		DateTo:    strings.TrimSpace(r.URL.Query().Get("date_to")),
		Sort:      strings.TrimSpace(r.URL.Query().Get("sort")),
	}
	items, total, err := h.service.ListTasks(r.Context(), claims, params)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	httputil.WritePaginated(w, items, int64(total), page, pageSize)
}

func (h *Handler) GetTask(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireEmployeeClaims(w, r)
	if !ok {
		return
	}
	taskID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || taskID <= 0 {
		httputil.WriteBadRequest(w, "invalid task id")
		return
	}
	item, err := h.service.GetTask(r.Context(), claims, taskID)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) CompleteTask(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireEmployeeClaims(w, r)
	if !ok {
		return
	}
	taskID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || taskID <= 0 {
		httputil.WriteBadRequest(w, "invalid task id")
		return
	}
	var req recording.CompleteTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	if err := h.service.CompleteTask(r.Context(), claims, taskID, req); err != nil {
		slog.Error("mobile complete task failed",
			"task_id", taskID,
			"user_id", claims.UserID,
			"tenant_id", claims.TenantID,
			"error", err.Error(),
		)
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]string{"message": "Task completed successfully"})
}

func (h *Handler) ListRecordings(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireEmployeeClaims(w, r)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	params := RecordingListParams{
		Page:           page,
		PageSize:       pageSize,
		Q:              strings.TrimSpace(r.URL.Query().Get("q")),
		AnalysisStatus: strings.TrimSpace(r.URL.Query().Get("analysis_status")),
		DateFrom:       strings.TrimSpace(r.URL.Query().Get("date_from")),
		DateTo:         strings.TrimSpace(r.URL.Query().Get("date_to")),
		TimeRange:      strings.TrimSpace(r.URL.Query().Get("time_range")),
		BusinessScope:  strings.TrimSpace(r.URL.Query().Get("business_scope")),
		Sort:           strings.TrimSpace(r.URL.Query().Get("sort")),
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("has_task")); raw != "" {
		val := raw == "true" || raw == "1"
		params.HasTask = &val
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("has_content_seed")); raw != "" {
		val := raw == "true" || raw == "1"
		params.HasContentSeed = &val
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("critical_gap")); raw != "" {
		val := raw == "true" || raw == "1"
		params.CriticalGap = &val
	}
	items, total, err := h.service.ListRecordings(r.Context(), claims, params)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	httputil.WritePaginated(w, items, int64(total), page, pageSize)
}

func (h *Handler) GetRecording(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireEmployeeClaims(w, r)
	if !ok {
		return
	}
	recordingID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || recordingID <= 0 {
		httputil.WriteBadRequest(w, "invalid recording id")
		return
	}
	item, err := h.service.GetRecording(r.Context(), claims, recordingID)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func requireEmployeeClaims(w http.ResponseWriter, r *http.Request) (*auth.Claims, bool) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "invalid token")
		return nil, false
	}
	switch claims.UserType {
	case auth.UserTypeEmployee, auth.UserTypeMobile:
		return claims, true
	default:
		httputil.WriteForbidden(w, "forbidden")
		return nil, false
	}
}
