package dealexpert

import (
	"context"
	"net/http"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/auth"
)

// dealScope 描述一次请求能看到的数据范围。
// 规则（与产品方案一致，不做个人排名）：
//   - 管理员：按 tenant_id 参数跨租户；
//   - 院长 / 销售主管：本租户全部；
//   - 咨询师：只看自己负责的接待与跟进；
//   - 随访 / 普通员工：不能进入赢客专家。
//
// 兜底：验收环境尚未完成角色分配时，没有角色记录的员工按本租户可见处理，
// 避免因为配置缺失把所有人挡在门外；角色一旦存在，就严格按角色收敛。
type dealScope struct {
	TenantID       int64
	EmployeeFilter *int64
	RoleCode       string
}

// ScopeRule 是「角色 → 数据范围」的纯决策结果，便于单测和验收脚本复用。
type ScopeRule struct {
	Allowed      bool
	AllEmployees bool
	Code         string
	Reason       string
}

// EvaluateDealScope 只根据角色判断可见范围，不依赖数据库和 HTTP。
func EvaluateDealScope(roles []string) ScopeRule {
	manager := hasRole(roles, "director") || hasRole(roles, "sales_manager")
	consultant := hasRole(roles, "consultant")
	if manager {
		return ScopeRule{Allowed: true, AllEmployees: true, Code: "manager", Reason: "主管/院长可见本租户全部接待"}
	}
	if consultant {
		return ScopeRule{Allowed: true, AllEmployees: false, Code: "consultant", Reason: "咨询师只看自己负责的接待"}
	}
	if len(roles) > 0 {
		return ScopeRule{Allowed: false, Code: "denied", Reason: "当前账号没有赢客专家权限"}
	}
	return ScopeRule{Allowed: true, AllEmployees: true, Code: "fallback", Reason: "未分配角色，按本租户可见兜底"}
}

func (s *dealScope) allowsAllEmployees() bool { return s.EmployeeFilter == nil }

func (h *Handler) employeeRoles(ctx context.Context, employeeID int64) []string {
	rows, err := h.pool.Query(ctx, `
		SELECT DISTINCT lower(trim(role_code))
		FROM institution_employee_roles
		WHERE employee_id = $1 AND trim(COALESCE(role_code, '')) <> ''`, employeeID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	roles := make([]string, 0, 3)
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return roles
		}
		roles = append(roles, code)
	}
	return roles
}

func hasRole(roles []string, target string) bool {
	for _, role := range roles {
		if role == target {
			return true
		}
	}
	return false
}

func (h *Handler) resolveDealScope(r *http.Request) (*dealScope, *tenantError) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		return nil, &tenantError{status: http.StatusUnauthorized, message: "invalid token"}
	}
	if claims.UserType == auth.UserTypeAdmin {
		scope, err := tenancy.ResolveScope(r.Context(), h.pool, claims, r.URL.Query().Get("tenant_id"))
		if err != nil {
			if strings.Contains(err.Error(), "access") {
				return nil, &tenantError{status: http.StatusForbidden, message: err.Error()}
			}
			return nil, &tenantError{status: http.StatusBadRequest, message: err.Error()}
		}
		if scope == nil || scope.TenantID == nil {
			return nil, &tenantError{status: http.StatusBadRequest, message: "tenant_id is required"}
		}
		return &dealScope{TenantID: *scope.TenantID, RoleCode: "admin"}, nil
	}

	if claims.TenantID == nil || *claims.TenantID <= 0 {
		return nil, &tenantError{status: http.StatusForbidden, message: "no tenant access"}
	}
	tenantID := *claims.TenantID
	roles := h.employeeRoles(r.Context(), claims.UserID)
	roleCode := ""
	if len(roles) > 0 {
		roleCode = strings.Join(roles, ",")
	}

	rule := EvaluateDealScope(roles)
	if !rule.Allowed {
		// 随访人员不自动看到销售顾虑与话术；普通员工没有赢客专家权限。
		return nil, &tenantError{status: http.StatusForbidden, message: rule.Reason}
	}
	if rule.AllEmployees {
		return &dealScope{TenantID: tenantID, RoleCode: roleCode}, nil
	}
	employeeID := claims.UserID
	return &dealScope{TenantID: tenantID, EmployeeFilter: &employeeID, RoleCode: roleCode}, nil
}

func (h *Handler) checkRecordingAccess(ctx context.Context, scope *dealScope, recordingID int64) *tenantError {
	if scope.allowsAllEmployees() {
		return nil
	}
	consultantID, err := h.service.RecordingConsultant(ctx, scope.TenantID, recordingID)
	if err != nil {
		return &tenantError{status: http.StatusNotFound, message: "recording not found"}
	}
	if consultantID != *scope.EmployeeFilter {
		return &tenantError{status: http.StatusForbidden, message: "无权访问该接待"}
	}
	return nil
}

func (h *Handler) checkEncounterAccess(ctx context.Context, scope *dealScope, encounterID int64) *tenantError {
	if scope.allowsAllEmployees() {
		return nil
	}
	consultantID, err := h.service.EncounterConsultant(ctx, scope.TenantID, encounterID)
	if err != nil {
		return &tenantError{status: http.StatusNotFound, message: "encounter not found"}
	}
	if consultantID != *scope.EmployeeFilter {
		return &tenantError{status: http.StatusForbidden, message: "无权访问该接待"}
	}
	return nil
}
