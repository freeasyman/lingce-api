package dealintervention

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/dealexpertpermission"
	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service *Service
	access  *dealexpertpermission.Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service, access: dealexpertpermission.NewService(service.pool)}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	router.Register(mux, []router.Route{
		{Method: "GET", Path: "/api/v1/deal-expert/interventions", Handler: h.List, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "GET", Path: "/api/v1/deal-expert/interventions/by-recording/{recordingID}", Handler: h.GetByRecording, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "POST", Path: "/api/v1/deal-expert/interventions/{recordingID}/actions", Handler: h.Act, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
	}, router.RouteDeps{JWTSecret: jwtSecret})
}

// visibility 返回统一的数据可见范围：管理员全机构，其余按 data_scope（self/custom/tenant）。
func (h *Handler) visibility(r *http.Request, claims *auth.Claims, tenantID int64) (*dealexpertpermission.Visibility, error) {
	if claims.UserType == auth.UserTypeAdmin {
		return &dealexpertpermission.Visibility{Scope: dealexpertpermission.DataScopeTenant, All: true, EmployeeIDs: []int64{}}, nil
	}
	return h.access.VisibilityFor(r.Context(), tenantID, claims.UserID)
}

func (h *Handler) tenantAndClaims(w http.ResponseWriter, r *http.Request) (*auth.Claims, int64, bool) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return nil, 0, false
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return nil, 0, false
	}
	return claims, tenantID, true
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.tenantAndClaims(w, r)
	if !ok {
		return
	}
	visibility, err := h.visibility(r, claims, tenantID)
	if err != nil {
		httputil.WriteInternalError(w, "无法解析数据可见范围")
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	items, total, err := h.service.List(r.Context(), tenantID, status, visibility.VisibilityEmployeeIDs(), page, pageSize)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items, "total": total, "page": page, "page_size": pageSize})
}

func (h *Handler) GetByRecording(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.tenantAndClaims(w, r)
	if !ok {
		return
	}
	visibility, err := h.visibility(r, claims, tenantID)
	if err != nil {
		httputil.WriteInternalError(w, "无法解析数据可见范围")
		return
	}
	recordingID, err := strconv.ParseInt(r.PathValue("recordingID"), 10, 64)
	if err != nil || recordingID <= 0 {
		httputil.WriteBadRequest(w, "invalid recording id")
		return
	}
	item, err := h.service.GetByRecording(r.Context(), tenantID, recordingID)
	if err != nil {
		if errors.Is(err, ErrSignalNotFound) {
			httputil.WriteSuccess(w, map[string]any{"signal": nil})
			return
		}
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if !visibility.Allows(item.ConsultantID) {
		httputil.WriteForbidden(w, "无权查看该介入信号")
		return
	}
	httputil.WriteSuccess(w, map[string]any{"signal": item})
}

func (h *Handler) Act(w http.ResponseWriter, r *http.Request) {
	claims, tenantID, ok := h.tenantAndClaims(w, r)
	if !ok {
		return
	}
	visibility, err := h.visibility(r, claims, tenantID)
	if err != nil {
		httputil.WriteInternalError(w, "无法解析数据可见范围")
		return
	}
	recordingID, err := strconv.ParseInt(r.PathValue("recordingID"), 10, 64)
	if err != nil || recordingID <= 0 {
		httputil.WriteBadRequest(w, "invalid recording id")
		return
	}
	var req ActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	current, err := h.service.GetByRecording(r.Context(), tenantID, recordingID)
	if err != nil {
		if errors.Is(err, ErrSignalNotFound) {
			httputil.WriteNotFound(w, "介入信号不存在")
			return
		}
		httputil.WriteInternalError(w, err.Error())
		return
	}
	if !visibility.Allows(current.ConsultantID) {
		httputil.WriteForbidden(w, "无权处理该介入信号")
		return
	}
	item, err := h.service.Act(r.Context(), tenantID, recordingID, claims.UserID, "", req.Action, strings.TrimSpace(req.Note))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"signal": item})
}
