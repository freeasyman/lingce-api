package emrpermission

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/internal/support"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type actionLogWriter interface {
	CreateSystemActionLog(context.Context, support.SystemActionLogWriteRequest, string, string, string) (*support.SystemActionLogResponse, error)
}

type Handler struct {
	service *Service
	logs    actionLogWriter
}

func NewHandler(service *Service, logs ...actionLogWriter) *Handler {
	var logger actionLogWriter
	if len(logs) > 0 {
		logger = logs[0]
	}
	return &Handler{service: service, logs: logger}
}

func (h *Handler) recordLog(r *http.Request, tenantID, actorID, employeeID int64, result string, cause error, before, after, attempted any) error {
	if h.logs == nil {
		return fmt.Errorf("permission action log writer is unavailable")
	}
	message := ""
	if cause != nil {
		message = cause.Error()
	}
	requestID := middleware.GetRequestID(r.Context())
	code, name := "emr_permission_update", "修改电子病历权限"
	if r.Method == http.MethodGet {
		code, name = "emr_permission_load", "读取人员权限"
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	var objectName *string
	var tenantName, actorName, actorRoleCode, actorRoleName *string
	if h.service != nil && h.service.store != nil {
		var targetName string
		if employeeID > 0 {
			if err := h.service.store.pool.QueryRow(ctx, `SELECT COALESCE(NULLIF(full_name,''), NULLIF(name,''), username) FROM employees WHERE tenant_id=$1 AND id=$2`, tenantID, employeeID).Scan(&targetName); err == nil {
				objectName = stringPtr(targetName)
			}
		}
		claims := middleware.GetUserClaims(r.Context())
		if claims != nil && claims.UserType == "admin" {
			var adminName, institutionName string
			if err := h.service.store.pool.QueryRow(ctx, `SELECT COALESCE(NULLIF(real_name,''), username) FROM operations_admins WHERE id=$1`, actorID).Scan(&adminName); err == nil {
				actorName = stringPtr(adminName)
			}
			if err := h.service.store.pool.QueryRow(ctx, `SELECT name FROM tenants WHERE id=$1`, tenantID).Scan(&institutionName); err == nil {
				tenantName = stringPtr(institutionName)
			}
			actorRoleCode, actorRoleName = stringPtr("admin"), stringPtr("管理员")
		}
	}
	_, logErr := h.logs.CreateSystemActionLog(ctx, support.SystemActionLogWriteRequest{
		TenantID: &tenantID, ActorID: &actorID, LogType: "action",
		TenantName: tenantName, ActorName: actorName, ActorRoleCode: actorRoleCode, ActorRoleName: actorRoleName,
		ActionCode: code, ActionName: name,
		RoutePath: stringPtr(r.URL.Path), Result: result, ErrorMessage: stringPtr(message),
		ObjectType: stringPtr("employee"), ObjectID: employeeID, ObjectName: objectName,
		RequestSummary: support.JSONObject{"employee_id": employeeID, "attempted_permissions": attempted},
		BeforeSummary:  toJSONObject(before), AfterSummary: toJSONObject(after), RequestID: stringPtr(requestID),
	}, permissionClientIP(r), r.UserAgent(), permissionDeviceType(r.UserAgent()))
	if logErr != nil {
		slog.Error("permission action log write failed", "request_id", requestID, "tenant_id", tenantID, "actor_id", actorID, "employee_id", employeeID, "result", result, "operation_error", message, "log_error", logErr)
	}
	return logErr
}

func permissionClientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return ip
	}
	return r.RemoteAddr
}

func permissionDeviceType(userAgent string) string {
	ua := strings.ToLower(userAgent)
	if ua == "" {
		return "unknown"
	}
	if strings.Contains(ua, "ipad") || strings.Contains(ua, "tablet") {
		return "tablet"
	}
	if strings.Contains(ua, "mobile") || strings.Contains(ua, "iphone") || strings.Contains(ua, "android") {
		return "mobile"
	}
	return "desktop"
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func toJSONObject(value any) support.JSONObject {
	if value == nil {
		return support.JSONObject{}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return support.JSONObject{}
	}
	var object support.JSONObject
	_ = json.Unmarshal(data, &object)
	if object == nil {
		return support.JSONObject{}
	}
	return object
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	routes := []router.Route{
		{Method: "GET", Path: "/api/v1/emr/permissions", Handler: h.ListPermissions, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "GET", Path: "/api/v1/emr/permissions/{employee_id}", Handler: h.GetPermission, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
		{Method: "PUT", Path: "/api/v1/emr/permissions/{employee_id}", Handler: h.UpsertPermission, Auth: true, AllowedUserTypes: []string{"admin", "employee"}},
	}
	router.Register(mux, routes, router.RouteDeps{JWTSecret: jwtSecret})
}

func (h *Handler) ListPermissions(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "登录已失效，请重新登录。")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, "无法确认当前机构，请重新登录后重试。")
		return
	}
	access, err := h.service.Authorize(r.Context(), claims, tenantID, "quality.manage")
	if err != nil || !access.CanManagePermissions() {
		if err == nil {
			err = fmt.Errorf("permission management role required")
		}
		h.recordLog(r, tenantID, claims.UserID, 0, "failure", err, nil, nil, nil)
		httputil.WriteForbidden(w, "你没有配置人员权限的权限，请联系管理员。")
		return
	}
	items, err := h.service.ListEmployeePermissions(r.Context(), tenantID)
	if err != nil {
		h.recordLog(r, tenantID, claims.UserID, 0, "failure", err, nil, nil, nil)
		httputil.WriteInternalError(w, "人员权限加载失败，请重试。")
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}

func (h *Handler) GetPermission(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "登录已失效，请重新登录。")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, "无法确认当前机构，请重新登录后重试。")
		return
	}
	employeeID, err := strconv.ParseInt(r.PathValue("employee_id"), 10, 64)
	if err != nil || employeeID <= 0 {
		httputil.WriteBadRequest(w, "员工信息有误，请刷新人员列表后重试。")
		return
	}
	if employeeID != claims.UserID {
		access, accessErr := h.service.Authorize(r.Context(), claims, tenantID, "quality.manage")
		if accessErr != nil || !access.CanManagePermissions() {
			if accessErr == nil {
				accessErr = fmt.Errorf("permission management role required")
			}
			h.recordLog(r, tenantID, claims.UserID, employeeID, "failure", accessErr, nil, nil, nil)
			httputil.WriteForbidden(w, "你没有配置人员权限的权限，请联系管理员。")
			return
		}
	}
	item, err := h.service.GetAssignment(r.Context(), tenantID, employeeID)
	if err != nil {
		h.recordLog(r, tenantID, claims.UserID, employeeID, "failure", err, nil, nil, nil)
		httputil.WriteInternalError(w, "员工权限读取失败，请重试。")
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}

func (h *Handler) UpsertPermission(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "登录已失效，请重新登录。")
		return
	}
	tenantID, err := tenancy.RequireTenantID(claims, r.URL.Query().Get("tenant_id"))
	if err != nil {
		httputil.WriteBadRequest(w, "无法确认当前机构，请重新登录后重试。")
		return
	}
	access, err := h.service.Authorize(r.Context(), claims, tenantID, "quality.manage")
	if err != nil || !access.CanManagePermissions() {
		if err == nil {
			err = fmt.Errorf("permission management role required")
		}
		h.recordLog(r, tenantID, claims.UserID, 0, "failure", err, nil, nil, nil)
		httputil.WriteForbidden(w, "你没有配置人员权限的权限，请联系管理员。")
		return
	}
	employeeID, err := strconv.ParseInt(r.PathValue("employee_id"), 10, 64)
	if err != nil || employeeID <= 0 {
		httputil.WriteBadRequest(w, "员工信息有误，请刷新人员列表后重试。")
		return
	}
	var req UpsertAssignmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.recordLog(r, tenantID, claims.UserID, employeeID, "failure", err, nil, nil, nil)
		httputil.WriteBadRequest(w, "提交的信息无法识别，请刷新页面后重新填写。")
		return
	}
	var before, after *AssignmentResponse
	item, err := h.service.UpsertAssignment(r.Context(), tenantID, employeeID, claims.UserID, req, func(old, saved *AssignmentResponse) { before, after = old, saved })
	if err != nil {
		h.recordLog(r, tenantID, claims.UserID, employeeID, "failure", err, before, before, req)
		message := mapPermissionError(err)
		if message == "权限保存失败，请重试。" {
			httputil.WriteInternalError(w, message)
		} else {
			httputil.WriteBadRequest(w, message)
		}
		return
	}
	if err := h.recordLog(r, tenantID, claims.UserID, employeeID, "success", nil, before, after, req); err != nil {
		httputil.WriteInternalError(w, "权限已保存，但操作日志未能记录，请联系管理员。")
		return
	}
	httputil.WriteSuccess(w, map[string]any{"data": item})
}

func mapPermissionError(err error) string {
	text := err.Error()
	switch {
	case strings.Contains(text, "emr_role_code is required"):
		return "请选择电子病历角色。"
	case strings.Contains(text, "scope is required"):
		return "请选择数据范围。"
	case strings.Contains(text, "invalid emr_role_code"):
		return "角色选项已失效，请刷新页面后重新选择。"
	case strings.Contains(text, "invalid scope"):
		return "数据范围选项已失效，请刷新页面后重新选择。"
	case strings.Contains(text, "abilities is required"):
		return "请勾选“查看病案”，再选择该员工需要的其他能力。"
	case strings.Contains(text, "record.read is required"):
		return "启用电子病历需要勾选“查看病案”。"
	case strings.Contains(text, "employee not found"):
		return "未找到这名员工，请刷新人员列表后重试。"
	case strings.Contains(text, "after save"):
		return "暂时无法确认保存结果，请刷新后检查。"
	default:
		return "权限保存失败，请重试。"
	}
}
