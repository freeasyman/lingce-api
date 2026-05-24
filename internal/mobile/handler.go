package mobile

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/recording"
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
	mux.Handle("GET /api/v1/mobile/me", authMw(http.HandlerFunc(h.GetMe)))
	mux.Handle("GET /api/v1/mobile/home", authMw(http.HandlerFunc(h.GetHome)))
	mux.Handle("GET /api/v1/mobile/tasks", authMw(http.HandlerFunc(h.ListTasks)))
	mux.Handle("GET /api/v1/mobile/tasks/{id}", authMw(http.HandlerFunc(h.GetTask)))
	mux.Handle("POST /api/v1/mobile/tasks/{id}/actions/complete", authMw(http.HandlerFunc(h.CompleteTask)))
	mux.Handle("GET /api/v1/mobile/recordings", authMw(http.HandlerFunc(h.ListRecordings)))
	mux.Handle("GET /api/v1/mobile/recordings/{id}", authMw(http.HandlerFunc(h.GetRecording)))
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
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	items, total, err := h.service.ListTasks(r.Context(), claims, status, page, pageSize)
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
	items, total, err := h.service.ListRecordings(r.Context(), claims, page, pageSize)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
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
