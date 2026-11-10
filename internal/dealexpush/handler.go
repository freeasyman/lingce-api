package dealexpush

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/dealexpertpermission"
	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service   *Service
	access    *dealexpertpermission.Service
	jwtSecret string
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service, access: dealexpertpermission.NewService(service.pool)}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	h.jwtSecret = jwtSecret
	router.Register(mux, []router.Route{
		// 短链解析：公开访问，无鉴权。
		{Method: "GET", Path: "/s/{code}", Handler: h.ResolveRedirect},
		// H5 短链换会话：用短链员工身份签发 7 天 JWT（只含 employee/tenant，不提升管理员）。
		{Method: "POST", Path: "/api/v1/deal-expert/h5/session", Handler: h.CreateH5Session},
		// 推送事件列表：管理员只读，便于验收。
		{Method: "GET", Path: "/api/v1/deal-expert/push-events", Handler: h.ListEvents, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		// 单号补发：管理员显式触发一条事件真实发送（白名单内），用于单号测试。
		{Method: "POST", Path: "/api/v1/deal-expert/push-events/{id}/send", Handler: h.SendEvent, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		// Expo 设备 token 注册/注销：员工 App 登录（mobile/employee）与管理员均可调用。
		{Method: "POST", Path: "/api/v1/deal-expert/push-devices", Handler: h.RegisterPushDevice, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
		{Method: "DELETE", Path: "/api/v1/deal-expert/push-devices/{token}", Handler: h.UnregisterPushDevice, Auth: true, AllowedUserTypes: []string{"admin", "employee", "mobile"}},
	}, router.RouteDeps{JWTSecret: jwtSecret})
}

// ResolveRedirect 解析短链并 302 到 H5 接待详情；不存在 404，过期 410。
func (h *Handler) ResolveRedirect(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	item, err := h.service.ResolveShortLink(r.Context(), code)
	if err != nil {
		switch {
		case errors.Is(err, ErrShortLinkExpired):
			httputil.WriteError(w, http.StatusGone, "SHORT_LINK_EXPIRED", "短链已过期", nil)
		case errors.Is(err, ErrShortLinkNotFound):
			httputil.WriteNotFound(w, "短链不存在或已失效")
		default:
			httputil.WriteInternalError(w, err.Error())
		}
		return
	}
	base := strings.TrimRight(strings.TrimSpace(h.service.cfg.H5BaseURL), "/")
	if base == "" {
		base = "https://employee.khgl.xyz"
	}
	page := "/h5/encounters/"
	if h.service.EmployeeIsManager(r.Context(), item.EmployeeID) {
		page = "/h5/intervene/"
	}
	target := base + page + strconv.FormatInt(item.RecordingID, 10) + "?code=" + item.Code
	http.Redirect(w, r, target, http.StatusFound)
}

// H5SessionRequest 是短链换会话的请求体。
type H5SessionRequest struct {
	Code string `json:"code"`
}

// CreateH5Session 用短链员工身份签发 7 天 H5 JWT；短链不存在 404、过期 410。
// 只含 employee/tenant，UserType 为 employee，不提升管理员。
func (h *Handler) CreateH5Session(w http.ResponseWriter, r *http.Request) {
	var body H5SessionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	item, err := h.service.ResolveShortLink(r.Context(), strings.TrimSpace(body.Code))
	if err != nil {
		switch {
		case errors.Is(err, ErrShortLinkExpired):
			httputil.WriteError(w, http.StatusGone, "SHORT_LINK_EXPIRED", "短链已过期", nil)
		case errors.Is(err, ErrShortLinkNotFound):
			httputil.WriteNotFound(w, "短链不存在或已失效")
		default:
			httputil.WriteInternalError(w, err.Error())
		}
		return
	}
	var (
		employeeID     int64
		employeeName   string
		employeeRole   string
		tenantID       int64
		sessionVersion int
	)
	if err := h.service.pool.QueryRow(r.Context(), `
		SELECT id, COALESCE(NULLIF(full_name,''), name, ''), COALESCE(role,''), tenant_id, COALESCE(session_version,1)
		FROM employees WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
	`, item.EmployeeID, item.TenantID).Scan(&employeeID, &employeeName, &employeeRole, &tenantID, &sessionVersion); err != nil {
		httputil.WriteNotFound(w, "短链对应员工不存在")
		return
	}
	token, expiresUnix, err := auth.GenerateToken(h.jwtSecret, employeeID, auth.UserTypeEmployee, &tenantID, sessionVersion, 24*7)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	expiresAt := time.Unix(expiresUnix, 0).Format(time.RFC3339)
	httputil.WriteSuccess(w, map[string]any{
		"token": token,
		"employee": map[string]any{
			"id": employeeID, "name": employeeName, "role": employeeRole,
		},
		"expires_at":   expiresAt,
		"recording_id": item.RecordingID,
	})
}

// ListEvents 管理员只读推送事件列表（供验收核对干跑结果）。
func (h *Handler) ListEvents(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	access, err := h.access.AccessFor(r.Context(), claims, tenantID)
	if err != nil || !access.CanManagePermissions() {
		httputil.WriteForbidden(w, "deal expert permission management required")
		return
	}
	recordingID, _ := strconv.ParseInt(r.URL.Query().Get("recording_id"), 10, 64)
	items, err := h.service.ListEvents(r.Context(), tenantID, recordingID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items, "total": len(items)})
}

// SendEvent 管理员显式发送一条事件（单号测试用）：
// 校验事件存在、同租户、enabled=true、收件人在白名单；已 sent 幂等返回，不重发。
func (h *Handler) SendEvent(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	access, err := h.access.AccessFor(r.Context(), claims, tenantID)
	if err != nil || !access.CanManagePermissions() {
		httputil.WriteForbidden(w, "deal expert permission management required")
		return
	}
	eventID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || eventID <= 0 {
		httputil.WriteBadRequest(w, "invalid event id")
		return
	}
	if !h.service.cfg.Enabled {
		httputil.WriteError(w, http.StatusBadRequest, "PUSH_NOT_ENABLED", "推送未开启（enabled=false），仅干跑", nil)
		return
	}
	row, err := h.service.loadEvent(r.Context(), eventID)
	if err != nil {
		httputil.WriteNotFound(w, "推送事件不存在")
		return
	}
	if row.TenantID != tenantID {
		httputil.WriteForbidden(w, "无权发送该事件")
		return
	}
	if row.Status == "sent" {
		httputil.WriteSuccess(w, map[string]any{"status": "sent", "biz_id": row.ProviderBizID, "error": ""})
		return
	}
	phone, err := h.service.EmployeePhone(r.Context(), row.RecipientEmployeeID)
	if err != nil || phone == "" {
		httputil.WriteBadRequest(w, "收件人没有手机号")
		return
	}
	if !whitelisted(h.service.cfg.Whitelist, phone) {
		httputil.WriteError(w, http.StatusBadRequest, "NOT_IN_WHITELIST", "收件人不在白名单，拒绝发送", nil)
		return
	}
	status, bizID, sendErr := h.service.sendEvent(r.Context(), eventID)
	if sendErr != nil {
		httputil.WriteSuccess(w, map[string]any{"status": "failed", "biz_id": "", "error": sendErr.Error()})
		return
	}
	httputil.WriteSuccess(w, map[string]any{"status": status, "biz_id": bizID, "error": ""})
}

// RegisterPushDevice 注册 Expo 设备 token；按 tenant+employee+token 幂等 upsert。
func (h *Handler) RegisterPushDevice(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}

	var body RegisterPushDeviceRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	device, err := h.service.RegisterDevice(r.Context(), tenantID, claims.UserID, body)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidPushToken):
			httputil.WriteBadRequest(w, "token 不能为空、不能包含空白且长度不能超过 255")
		case errors.Is(err, ErrInvalidPushPlatform):
			httputil.WriteBadRequest(w, "platform 仅支持 ios/android")
		default:
			httputil.WriteInternalError(w, err.Error())
		}
		return
	}
	httputil.WriteSuccess(w, map[string]any{
		"ok":        true,
		"id":        device.ID,
		"platform":  device.Platform,
		"is_active": device.IsActive,
	})
}

// UnregisterPushDevice 注销当前员工的一条 token；不存在也返回 ok（幂等）。
func (h *Handler) UnregisterPushDevice(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	token := strings.TrimSpace(r.PathValue("token"))
	if token == "" {
		httputil.WriteBadRequest(w, "token 不能为空")
		return
	}
	if err := h.service.UnregisterDevice(r.Context(), tenantID, claims.UserID, token); err != nil {
		if errors.Is(err, ErrInvalidPushToken) {
			httputil.WriteBadRequest(w, "token 不能为空")
			return
		}
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"ok": true})
}
