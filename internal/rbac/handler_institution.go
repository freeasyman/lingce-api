package rbac

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/internal/permission"
	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

// requireInstitutionAdminIdentity 校验「有权修改机构角色/科室默认角色」的身份：
// 平台 admin 直接放行；机构员工必须本身是机构管理员（而不是仅持有 roles/departments 菜单）。
// 返回目标租户；无权返回 (nil, false)。
func (h *Handler) requireInstitutionAdminIdentity(r *http.Request, claims *auth.Claims) (*int64, bool) {
	if claims == nil {
		return nil, false
	}
	if claims.UserType == auth.UserTypeAdmin {
		return h.getTenantID(r), true
	}
	if claims.TenantID == nil || *claims.TenantID <= 0 {
		return nil, false
	}
	isAdmin, err := permission.IsInstitutionAdmin(r.Context(), h.service.store.pool, *claims.TenantID, claims.UserID)
	if err != nil || !isAdmin {
		return nil, false
	}
	tid := *claims.TenantID
	return &tid, true
}

// requireOwnedRoleTarget 校验目标员工/科室属于调用者的租户（平台 admin 亦按对象租户比对）。
func (h *Handler) requireSameTenantAsCaller(w http.ResponseWriter, r *http.Request, callerTenant *int64, objectTenant int64) bool {
	if callerTenant == nil {
		httputil.WriteForbidden(w, "Tenant access required")
		return false
	}
	if *callerTenant != objectTenant {
		httputil.WriteForbidden(w, "Access denied")
		return false
	}
	return true
}

func (h *Handler) ListOpsOrganizations(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}
	items, err := h.service.ListOpsOrganizations(r.Context())
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, map[string]interface{}{"items": items})
}

func (h *Handler) CreateOpsOrganization(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}
	var req CreateOpsOrganizationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	item, err := h.service.CreateOpsOrganization(r.Context(), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) UpdateOpsOrganization(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "Invalid org ID")
		return
	}
	var req UpdateOpsOrganizationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	item, err := h.service.UpdateOpsOrganization(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, item)
}

func (h *Handler) DeleteOpsOrganization(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httputil.WriteBadRequest(w, "Invalid org ID")
		return
	}
	resp, err := h.service.DeleteOpsOrganization(r.Context(), id)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	httputil.WriteSuccess(w, resp)
}

// Operations Admin handlers

// ListOperationsAdmins handles listing operations admins
func (h *Handler) ListOperationsAdmins(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req AdminListRequest
	req.Username = r.URL.Query().Get("username")
	req.Email = r.URL.Query().Get("email")
	if orgIDStr := r.URL.Query().Get("org_id"); orgIDStr != "" {
		if orgID, err := strconv.ParseInt(orgIDStr, 10, 64); err == nil && orgID > 0 {
			req.OrgID = &orgID
		}
	}

	if isActiveStr := r.URL.Query().Get("is_active"); isActiveStr != "" {
		isActive := isActiveStr == "true"
		req.IsActive = &isActive
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize
	if claims := middleware.GetUserClaims(r.Context()); claims != nil {
		req.RequestAdminID = claims.UserID
	}

	admins, total, err := h.service.ListOperationsAdmins(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, admins, int64(total), req.Page, req.PageSize)
}

// CreateOperationsAdmin handles creating an operations admin
func (h *Handler) CreateOperationsAdmin(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req CreateAdminRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	admin, err := h.service.CreateOperationsAdmin(r.Context(), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, admin)
}

// GetOperationsAdmin handles getting an operations admin by ID
func (h *Handler) GetOperationsAdmin(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid admin ID")
		return
	}

	admin, err := h.service.GetOperationsAdmin(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, admin)
}

// UpdateOperationsAdmin handles updating an operations admin
func (h *Handler) UpdateOperationsAdmin(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid admin ID")
		return
	}

	var req UpdateAdminRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	admin, err := h.service.UpdateOperationsAdmin(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, admin)
}

// DeleteOperationsAdmin handles deleting an operations admin
func (h *Handler) DeleteOperationsAdmin(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid admin ID")
		return
	}

	if err := h.service.DeleteOperationsAdmin(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Admin deleted successfully"})
}

// ResetAdminPassword handles resetting an admin's password
func (h *Handler) ResetAdminPassword(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid admin ID")
		return
	}

	var req ResetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if err := h.service.ResetAdminPassword(r.Context(), id, req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Password reset successfully"})
}

// Institution Role handlers

// ListInstitutionRoles handles listing institution roles
func (h *Handler) ListInstitutionRoles(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "roles"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	tenantID := h.getTenantID(r)
	if tenantID == nil {
		httputil.WriteForbidden(w, "Tenant access required")
		return
	}

	var req InstitutionRoleListRequest
	req.Name = r.URL.Query().Get("name")
	req.Code = r.URL.Query().Get("code")

	if isActiveStr := r.URL.Query().Get("is_active"); isActiveStr != "" {
		isActive := isActiveStr == "true"
		req.IsActive = &isActive
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize

	roles, total, err := h.service.ListInstitutionRoles(r.Context(), *tenantID, req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, roles, int64(total), req.Page, req.PageSize)
}

// CreateInstitutionRole handles creating an institution role
func (h *Handler) CreateInstitutionRole(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "roles"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	tenantID := h.getTenantID(r)
	if tenantID == nil {
		httputil.WriteForbidden(w, "Tenant access required")
		return
	}

	var req CreateInstitutionRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	role, err := h.service.CreateInstitutionRole(r.Context(), *tenantID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, role)
}

// GetInstitutionRole handles getting an institution role by ID
func (h *Handler) GetInstitutionRole(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "roles"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	tenantID := h.getTenantID(r)
	if tenantID == nil {
		httputil.WriteForbidden(w, "Tenant access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid role ID")
		return
	}

	role, err := h.service.GetInstitutionRole(r.Context(), *tenantID, id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, role)
}

// UpdateInstitutionRole handles updating an institution role
func (h *Handler) UpdateInstitutionRole(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "roles"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	tenantID := h.getTenantID(r)
	if tenantID == nil {
		httputil.WriteForbidden(w, "Tenant access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid role ID")
		return
	}

	var req UpdateInstitutionRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	role, err := h.service.UpdateInstitutionRole(r.Context(), *tenantID, id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, role)
}

// DeleteInstitutionRole handles deleting an institution role
func (h *Handler) DeleteInstitutionRole(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "roles"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	tenantID := h.getTenantID(r)
	if tenantID == nil {
		httputil.WriteForbidden(w, "Tenant access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid role ID")
		return
	}

	if err := h.service.DeleteInstitutionRole(r.Context(), *tenantID, id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Role deleted successfully"})
}

// Institution Menu handlers

// ListInstitutionMenus handles listing institution menus
func (h *Handler) ListInstitutionMenus(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "menus"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	tenantID := h.getTenantID(r)

	var req InstitutionMenuListRequest
	req.Name = r.URL.Query().Get("name")

	if isActiveStr := r.URL.Query().Get("is_active"); isActiveStr != "" {
		isActive := isActiveStr == "true"
		req.IsActive = &isActive
	}

	menus, err := h.service.ListInstitutionMenus(r.Context(), tenantID, req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, menus)
}

// GetInstitutionMenuTree handles getting institution menus as a tree.
func (h *Handler) GetInstitutionMenuTree(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "menus"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	tenantID := h.getTenantID(r)
	if tenantID == nil {
		httputil.WriteForbidden(w, "Tenant access required")
		return
	}

	menus, err := h.service.GetInstitutionMenuTree(r.Context(), tenantID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, menus)
}

// GetCurrentEmployeeEffectiveMenus handles getting the current employee's effective institution menus.
func (h *Handler) GetCurrentEmployeeEffectiveMenus(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if claims == nil || claims.UserType == "admin" || claims.TenantID == nil {
		httputil.WriteForbidden(w, "Institution employee access required")
		return
	}

	resp, err := h.service.GetEmployeeEffectiveMenus(r.Context(), claims.UserID)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, resp)
}

// CreateInstitutionMenu handles creating an institution menu
func (h *Handler) CreateInstitutionMenu(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "menus"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	tenantID := h.getTenantID(r)

	var req CreateInstitutionMenuRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	menu, err := h.service.CreateInstitutionMenu(r.Context(), tenantID, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, menu)
}

// GetInstitutionMenu handles getting an institution menu by ID
func (h *Handler) GetInstitutionMenu(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "menus"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	tenantID := h.getTenantID(r)

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid menu ID")
		return
	}

	menu, err := h.service.GetInstitutionMenu(r.Context(), tenantID, id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, menu)
}

// UpdateInstitutionMenu handles updating an institution menu
func (h *Handler) UpdateInstitutionMenu(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "menus"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	tenantID := h.getTenantID(r)

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid menu ID")
		return
	}

	var req UpdateInstitutionMenuRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	menu, err := h.service.UpdateInstitutionMenu(r.Context(), tenantID, id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, menu)
}

// DeleteInstitutionMenu handles deleting an institution menu
func (h *Handler) DeleteInstitutionMenu(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "menus"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	tenantID := h.getTenantID(r)

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid menu ID")
		return
	}

	if err := h.service.DeleteInstitutionMenu(r.Context(), tenantID, id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Menu deleted successfully"})
}

// Institution Permission handlers

// GetInstitutionRoleMenus handles getting menus for an institution role.
func (h *Handler) GetInstitutionRoleMenus(w http.ResponseWriter, r *http.Request) {
	h.GetInstitutionRolePermissions(w, r)
}

// AssignMenusToInstitutionRole handles assigning menus to an institution role.
func (h *Handler) AssignMenusToInstitutionRole(w http.ResponseWriter, r *http.Request) {
	h.AssignPermissionsToInstitutionRole(w, r)
}

// AssignPermissionsToInstitutionRole handles assigning permissions to an institution role
func (h *Handler) AssignPermissionsToInstitutionRole(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "roles"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	tenantID := h.getTenantID(r)
	if tenantID == nil {
		httputil.WriteForbidden(w, "Tenant access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid role ID")
		return
	}

	var raw struct {
		PermissionIDs []int64 `json:"permission_ids"`
		MenuIDs       []int64 `json:"menu_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}
	req := AssignPermissionsRequest{PermissionIDs: raw.PermissionIDs}
	if len(raw.MenuIDs) > 0 {
		req.PermissionIDs = raw.MenuIDs
	}

	if err := h.service.ValidateInstitutionRoleMenuScope(r.Context(), *tenantID, req.PermissionIDs); err != nil {
		if errors.Is(err, ErrMenuOutOfPolicy) {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	if err := h.service.AssignPermissionsToInstitutionRole(r.Context(), *tenantID, id, req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Permissions assigned successfully"})
}

// RemovePermissionsFromInstitutionRole handles removing permissions from an institution role
func (h *Handler) RemovePermissionsFromInstitutionRole(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "roles"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	tenantID := h.getTenantID(r)
	if tenantID == nil {
		httputil.WriteForbidden(w, "Tenant access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid role ID")
		return
	}

	var req AssignPermissionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if err := h.service.RemovePermissionsFromInstitutionRole(r.Context(), *tenantID, id, req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Permissions removed successfully"})
}

// GetInstitutionRolePermissions handles getting permissions for an institution role
func (h *Handler) GetInstitutionRolePermissions(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "roles"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	tenantID := h.getTenantID(r)
	if tenantID == nil {
		httputil.WriteForbidden(w, "Tenant access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid role ID")
		return
	}

	permissions, err := h.service.GetInstitutionRolePermissions(r.Context(), *tenantID, id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, permissions)
}

// Employee Role handlers

// GetEmployeeRole handles getting the role for an employee
func (h *Handler) GetEmployeeRole(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "roles"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid employee ID")
		return
	}

	// P1 补漏：目标员工必须属于本机构。
	targetTenantID, err := h.service.store.getEmployeeTenantID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if err := tenancy.RequireSameTenant(claims, targetTenantID); err != nil {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	role, err := h.service.GetEmployeeRole(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, role)
}

// SetEmployeeRole handles setting the role for an employee
func (h *Handler) SetEmployeeRole(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "roles"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	// P0-6：角色写入必须由机构管理员（或平台 admin）发起，禁止仅凭菜单可见性操作。
	callerTenant, ok := h.requireInstitutionAdminIdentity(r, claims)
	if !ok {
		httputil.WriteForbidden(w, "Institution admin access required")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid employee ID")
		return
	}

	var req SetEmployeeRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	// P0-6：目标员工必须属于本机构。
	targetTenantID, err := h.service.store.getEmployeeTenantID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if !h.requireSameTenantAsCaller(w, r, callerTenant, targetTenantID) {
		return
	}
	// P0-6：admin 角色只允许平台运营分配，防止机构管理员/员工自提权。
	if claims.UserType != auth.UserTypeAdmin && req.RoleID > 0 {
		roleCode, codeErr := h.service.store.institutionRoleCode(r.Context(), req.RoleID, targetTenantID)
		if codeErr == nil && strings.EqualFold(strings.TrimSpace(roleCode), "admin") {
			httputil.WriteForbidden(w, "admin role is not assignable")
			return
		}
	}

	if err := h.service.SetEmployeeRole(r.Context(), id, req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Employee role set successfully"})
}

// RemoveEmployeeRole handles removing the role from an employee
func (h *Handler) RemoveEmployeeRole(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "roles"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	// P0-6：同 SetEmployeeRole。
	callerTenant, ok := h.requireInstitutionAdminIdentity(r, claims)
	if !ok {
		httputil.WriteForbidden(w, "Institution admin access required")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid employee ID")
		return
	}
	targetTenantID, err := h.service.store.getEmployeeTenantID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if !h.requireSameTenantAsCaller(w, r, callerTenant, targetTenantID) {
		return
	}

	if err := h.service.RemoveEmployeeRole(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Employee role removed successfully"})
}

// GetDepartmentRole handles getting default role for a department.
func (h *Handler) GetDepartmentRole(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "departments"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	tenantID := h.getTenantID(r)
	if tenantID == nil {
		httputil.WriteForbidden(w, "Tenant access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid department ID")
		return
	}

	// P1 补漏：目标科室必须属于本机构。
	targetTenantID, err := h.service.store.departmentTenantID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if !h.requireSameTenantAsCaller(w, r, tenantID, targetTenantID) {
		return
	}

	role, err := h.service.GetDepartmentRole(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, role)
}

// SetDepartmentRole handles setting default role for a department.
func (h *Handler) SetDepartmentRole(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "departments"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	// P0-7：同 SetEmployeeRole——机构管理员身份 + 对象租户校验 + 禁分配 admin 角色。
	callerTenant, ok := h.requireInstitutionAdminIdentity(r, claims)
	if !ok {
		httputil.WriteForbidden(w, "Institution admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid department ID")
		return
	}

	var req SetDepartmentRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	targetTenantID, err := h.service.store.departmentTenantID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if !h.requireSameTenantAsCaller(w, r, callerTenant, targetTenantID) {
		return
	}
	if claims.UserType != auth.UserTypeAdmin {
		if req.RoleCode != nil && strings.EqualFold(strings.TrimSpace(*req.RoleCode), "admin") {
			httputil.WriteForbidden(w, "admin role is not assignable")
			return
		}
		if req.RoleID != nil && *req.RoleID > 0 {
			roleCode, codeErr := h.service.store.institutionRoleCode(r.Context(), *req.RoleID, targetTenantID)
			if codeErr == nil && strings.EqualFold(strings.TrimSpace(roleCode), "admin") {
				httputil.WriteForbidden(w, "admin role is not assignable")
				return
			}
		}
	}

	if err := h.service.SetDepartmentRole(r.Context(), id, req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Department role set successfully"})
}

// RemoveDepartmentRole handles removing default role from a department.
func (h *Handler) RemoveDepartmentRole(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetUserClaims(r.Context())
	if err := h.requireInstitutionMenuAccess(r.Context(), claims, "departments"); err != nil {
		httputil.WriteForbidden(w, err.Error())
		return
	}
	// P0-7：同 SetEmployeeRole。
	callerTenant, ok := h.requireInstitutionAdminIdentity(r, claims)
	if !ok {
		httputil.WriteForbidden(w, "Institution admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid department ID")
		return
	}
	targetTenantID, err := h.service.store.departmentTenantID(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}
	if !h.requireSameTenantAsCaller(w, r, callerTenant, targetTenantID) {
		return
	}

	if err := h.service.RemoveDepartmentRole(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Department role removed successfully"})
}
