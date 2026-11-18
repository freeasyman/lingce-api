package dealexpush

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/dealexpertpermission"
	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// canAccessAlertRoutes 与前端 /alert-settings 守卫一致：管理员/负责人或具备
// workbenchRead 的员工可读写邮件路由；无权限的普通员工仍返回 403。
func canAccessAlertRoutes(access *dealexpertpermission.Access) bool {
	if access == nil || !access.Enabled {
		return false
	}
	return access.CanManagePermissions() || access.Has(dealexpertpermission.AbilityWorkbenchRead)
}

// requireAlertAccess 校验登录态、租户与 /alert-settings 页面守卫权限。
func (h *Handler) requireAlertAccess(w http.ResponseWriter, r *http.Request) (int64, *auth.Claims, bool) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return 0, nil, false
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteForbidden(w, err.Error())
		return 0, nil, false
	}
	access, err := h.access.AccessFor(r.Context(), claims, tenantID)
	if err != nil || !canAccessAlertRoutes(access) {
		httputil.WriteForbidden(w, "deal expert alert settings permission required")
		return 0, nil, false
	}
	return tenantID, claims, true
}

// ListAlertRoutes 返回邮件路由配置与员工选择器；可按 owner_employee_id 过滤。
func (h *Handler) ListAlertRoutes(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := h.requireAlertAccess(w, r)
	if !ok {
		return
	}
	ownerEmployeeID, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("owner_employee_id")), 10, 64)
	routes, employees, err := h.service.ListAlertRoutes(r.Context(), tenantID, ownerEmployeeID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{
		"items":     routes,
		"employees": employees,
	})
}

type alertRouteRequest struct {
	ToEmployeeIDs []int64 `json:"to_employee_ids"`
	CcEmployeeIDs []int64 `json:"cc_employee_ids"`
}

// UpdateAlertRoute 保存某负责人/事件类型的收件人与抄送人。
func (h *Handler) UpdateAlertRoute(w http.ResponseWriter, r *http.Request) {
	tenantID, claims, ok := h.requireAlertAccess(w, r)
	if !ok {
		return
	}
	ownerEmployeeID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("owner_employee_id")), 10, 64)
	if err != nil || ownerEmployeeID <= 0 {
		httputil.WriteBadRequest(w, "invalid owner_employee_id")
		return
	}
	eventType := strings.ToLower(strings.TrimSpace(r.PathValue("event_type")))
	if eventType != "consultant" && eventType != "manager" {
		httputil.WriteBadRequest(w, "event_type must be consultant or manager")
		return
	}
	var body alertRouteRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.WriteBadRequest(w, "invalid request body")
		return
	}
	route, err := h.service.UpsertAlertRoute(r.Context(), tenantID, ownerEmployeeID, eventType, body.ToEmployeeIDs, body.CcEmployeeIDs, claims.UserID)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, route)
}

// SendEventEmail 管理员/测试用：按当前路由给一条事件补发邮件（强制白名单）。
func (h *Handler) SendEventEmail(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := h.requireAlertAccess(w, r)
	if !ok {
		return
	}
	eventID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || eventID <= 0 {
		httputil.WriteBadRequest(w, "invalid event id")
		return
	}
	if !h.service.emailCfg.Enabled {
		httputil.WriteError(w, http.StatusBadRequest, "EMAIL_NOT_ENABLED", "邮件提醒未开启（enabled=false），仅干跑", nil)
		return
	}
	if !h.service.emailCfg.IsDryRun() && h.service.mailer == nil {
		httputil.WriteError(w, http.StatusBadRequest, "EMAIL_SMTP_NOT_CONFIGURED", "SMTP 未配置（[deal.email] 与 external.ticket_notify 均不完整）", nil)
		return
	}
	results, err := h.service.SendEventEmail(r.Context(), eventID, tenantID)
	if err != nil {
		switch {
		case errors.Is(err, ErrEmailEventNotFound):
			httputil.WriteNotFound(w, "推送事件不存在")
		case errors.Is(err, ErrEmailDisabled):
			httputil.WriteError(w, http.StatusBadRequest, "EMAIL_NOT_ENABLED", "邮件提醒未开启（enabled=false），仅干跑", nil)
		default:
			httputil.WriteInternalError(w, err.Error())
		}
		return
	}
	if results == nil {
		results = []*EmailDeliveryResult{}
	}
	httputil.WriteSuccess(w, map[string]any{
		"items":   results,
		"dry_run": h.service.emailCfg.IsDryRun(),
	})
}
