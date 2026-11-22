package rbac

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/permission"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

func resolvePortalTenantID(r *http.Request, claims *auth.Claims) (int64, error) {
	if claims.TenantID != nil && *claims.TenantID > 0 {
		return *claims.TenantID, nil
	}
	raw := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if raw == "" {
		return 0, fmt.Errorf("tenant_id required")
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid tenant_id")
	}
	return id, nil
}

func (h *Handler) requirePortalAdmin(r *http.Request, claims *auth.Claims, tenantID int64) bool {
	if claims.UserType == auth.UserTypeAdmin {
		return true
	}
	isAdmin, err := permission.IsInstitutionAdmin(r.Context(), h.service.store.pool, tenantID, claims.UserID)
	return err == nil && isAdmin
}

// ListEmployeeProducts 返回某员工的产品权限（岗位默认 + 手动覆盖后的有效值）。
func (h *Handler) ListEmployeeProducts(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := resolvePortalTenantID(r, claims)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	employeeID, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("employee_id")), 10, 64)
	if err != nil || employeeID <= 0 {
		// 没传 employee_id：批量返回全机构的有效产品（员工产品矩阵视图）。
		items, batchErr := EffectiveTenantEmployeeProducts(r.Context(), h.service.store.pool, tenantID)
		if batchErr != nil {
			httputil.WriteInternalError(w, batchErr.Error())
			return
		}
		httputil.WriteSuccess(w, map[string]any{"items": items})
		return
	}
	role, items, err := EffectiveEmployeeProducts(r.Context(), h.service.store.pool, tenantID, employeeID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{
		"employee_id": employeeID,
		"role_code":   role,
		"is_admin":    role == "admin",
		"items":       items,
	})
}

// SetEmployeeProducts 保存员工的产品权限（只存与岗位默认不同的部分）。
func (h *Handler) SetEmployeeProducts(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	tenantID, err := resolvePortalTenantID(r, claims)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	if !h.requirePortalAdmin(r, claims, tenantID) {
		httputil.WriteForbidden(w, "Institution admin required")
		return
	}
	employeeID, err := strconv.ParseInt(r.PathValue("employee_id"), 10, 64)
	if err != nil || employeeID <= 0 {
		httputil.WriteBadRequest(w, "invalid employee_id")
		return
	}
	var req struct {
		Products []string `json:"products"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "invalid body")
		return
	}
	if err := SetEmployeeProducts(r.Context(), h.service.store.pool, tenantID, employeeID, req.Products, claims.UserID); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	role, items, err := EffectiveEmployeeProducts(r.Context(), h.service.store.pool, tenantID, employeeID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{
		"employee_id": employeeID,
		"role_code":   role,
		"is_admin":    role == "admin",
		"items":       items,
	})
}

// MyProducts 返回当前登录员工可用的产品（门户首页用）。
func (h *Handler) MyProducts(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil {
		httputil.WriteUnauthorized(w, "Invalid token")
		return
	}
	// 平台管理员不属于任何机构：返回全部产品，前端按机构开通情况再过滤。
	if claims.UserType == auth.UserTypeAdmin {
		items := make([]ProductAccess, 0, len(AllPortalProducts))
		for _, item := range AllPortalProducts {
			items = append(items, ProductAccess{ProductCode: item, Enabled: true, Source: "admin"})
		}
		httputil.WriteSuccess(w, map[string]any{"items": items})
		return
	}
	if claims.TenantID == nil || *claims.TenantID <= 0 {
		httputil.WriteBadRequest(w, "tenant required")
		return
	}
	_, items, err := EffectiveEmployeeProducts(r.Context(), h.service.store.pool, *claims.TenantID, claims.UserID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]any{"items": items})
}
