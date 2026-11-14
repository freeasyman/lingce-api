package rbac

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/permission"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// SetProductAdminRequest 是门户「指定/撤销产品管理员」的请求体。
type SetProductAdminRequest struct {
	IsAdmin bool `json:"is_admin"`
}

// ListProductAdmins 返回本机构各产品的管理员员工。
func (h *Handler) ListProductAdmins(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	if claims.TenantID == nil || *claims.TenantID <= 0 {
		httputil.WriteBadRequest(w, "tenant required")
		return
	}
	rows, err := permission.ListProductAdmins(r.Context(), h.service.store.pool, *claims.TenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, rows)
}

// SetProductAdmin 将某员工设为/撤销某产品的管理员（复用 permission.SetProductAdmin）。
// 权限：平台管理员可跨机构；机构管理员只能操作本机构员工。
func (h *Handler) SetProductAdmin(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	if claims.TenantID == nil || *claims.TenantID <= 0 {
		httputil.WriteBadRequest(w, "tenant required")
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		isAdmin, err := permission.IsInstitutionAdmin(r.Context(), h.service.store.pool, *claims.TenantID, claims.UserID)
		if err != nil || !isAdmin {
			httputil.WriteForbidden(w, "Institution admin required")
			return
		}
	}

	product := r.PathValue("product")
	employeeID, err := strconv.ParseInt(r.PathValue("employee_id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "invalid employee_id")
		return
	}

	var req SetProductAdminRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid body")
		return
	}

	result, err := permission.SetProductAdmin(r.Context(), h.service.store.pool, product, *claims.TenantID, employeeID, claims.UserID, req.IsAdmin)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	// 基础动作日志：谁、何时、给谁、设/撤了哪个产品的管理员
	_, _ = h.service.store.pool.Exec(r.Context(), `
		INSERT INTO system_action_logs (tenant_id, actor_id, log_type, action_code, action_name, object_type, object_id, result)
		VALUES ($1, $2, 'institution', $3, $3, 'product_admin', $4::text, 'success')
	`, *claims.TenantID, claims.UserID, "product.admin.set", product+"|"+strconv.FormatInt(employeeID, 10))
	httputil.WriteSuccess(w, result)
}
