package router

import (
	"context"
	"net/http"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
)

// OpsMenuCodeResolver 返回运营端账号可访问的菜单 code；空切片表示「未配置 = 全量可见」。
type OpsMenuCodeResolver func(ctx context.Context, adminID int64) ([]string, error)

var opsMenuCodeResolver OpsMenuCodeResolver

// SetOpsMenuCodeResolver 在启动时注入解析器（读 operations_admins.menu_codes）。
func SetOpsMenuCodeResolver(r OpsMenuCodeResolver) { opsMenuCodeResolver = r }

// opsMenuCodeRules 定义运营管理台 API 前缀 → 所需菜单 code（按声明顺序、长前缀优先）。
// 只对 user_type=admin 的请求生效；employee/mobile 有自己的菜单体系，不在此拦截。
// 未命中的路径不拦截（共享/下拉/其它 app 接口放行）。
// 低敏感度的账号/角色列表接口（/roles、/menus、/employees、/departments 等）不在此拦截，
// 以免「机构 → 员工账号」等页面因角色/科室下拉被误伤；账号写操作走 /roles/admins。
//
// 菜单 code 与运营管理台前端 menuCatalog 对齐。
var opsMenuCodeRules = []struct{ prefix, menuCode string }{
	// 机构
	{"/api/v1/ops/billing/institutions", "tenants"},
	{"/api/v1/ops/billing/documents", "tenants"},
	{"/api/v1/tenants", "tenants"},
	// 财务
	{"/api/v1/ops/billing/payments", "billing_payments"},
	{"/api/v1/ops/billing/invoices", "billing_invoices"},
	{"/api/v1/ops/billing/orders", "billing_invoices"},
	{"/api/v1/ops/billing/export", "billing_payments"},
	// 运营配置
	{"/api/v1/ops/billing/pricing", "billing_pricing"},
	{"/api/v1/ops/billing/catalog", "billing_pricing"},
	{"/api/v1/ops/billing/channels", "billing_channels"},
	{"/api/v1/ops/billing/sales", "billing_channels"},
	{"/api/v1/ops/billing/dictionaries", "billing_dictionaries"},
	// 机构（邮件投递）
	{"/api/v1/email-deliveries", "email_deliveries"},
	// AI 方案（创造方案）
	{"/api/v1/recordings/prompts", "prompts"},
	// 录音库
	{"/api/v1/recordings/doctor", "recordings_doctor"},
	{"/api/v1/recordings/consultant", "recordings_consultant"},
	{"/api/v1/recordings/playback-config", "recordings_playback"},
	// LLM 管理
	{"/api/v1/llm/records", "llm_records"},
	{"/api/v1/llm/analysis", "llm_analysis"},
	{"/api/v1/llm/models", "llm_models"},
	// 工牌管理
	{"/api/v1/badges/receipts", "badges_receipts"},
	{"/api/v1/badges/tickets", "badges_tickets"},
	{"/api/v1/badges", "badges_ledger"},
	// 系统管理（账号权限 / 账号管理）
	{"/api/v1/roles/admins", "system_admins"},
}

func opsMenuCodeForPath(path string) string {
	for _, rule := range opsMenuCodeRules {
		if strings.HasPrefix(path, rule.prefix) {
			return rule.menuCode
		}
	}
	return ""
}

// opsMenuCodeMiddleware 仅拦截需要菜单授权的运营端接口：
//   - 路径未命中规则 → 放行；
//   - 非 admin 用户 → 放行（employee/mobile 有自己的菜单体系）；
//   - admin 的 menu_codes 为空 → 放行（未配置 = 全量）；
//   - admin 的 menu_codes 包含所需 code → 放行；否则 403。
func opsMenuCodeMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := opsMenuCodeForPath(r.URL.Path)
		if code == "" {
			next(w, r)
			return
		}
		claims := middleware.GetUserClaims(r.Context())
		if claims == nil || string(claims.UserType) != "admin" {
			next(w, r)
			return
		}
		if opsMenuCodeResolver == nil {
			next(w, r)
			return
		}
		codes, err := opsMenuCodeResolver(r.Context(), claims.UserID)
		if err != nil {
			// 查询失败不误伤，放行（下次请求重试）。
			next(w, r)
			return
		}
		if len(codes) == 0 {
			next(w, r)
			return
		}
		for _, c := range codes {
			if c == code {
				next(w, r)
				return
			}
		}
		http.Error(w, "Forbidden", http.StatusForbidden)
	}
}
