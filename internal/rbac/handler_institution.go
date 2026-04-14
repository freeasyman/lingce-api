package rbac

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

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

	if isActiveStr := r.URL.Query().Get("is_active"); isActiveStr != "" {
		isActive := isActiveStr == "true"
		req.IsActive = &isActive
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	req.Page = page
	req.PageSize = pageSize

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

// CreateInstitutionMenu handles creating an institution menu
func (h *Handler) CreateInstitutionMenu(w http.ResponseWriter, r *http.Request) {
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

// AssignPermissionsToInstitutionRole handles assigning permissions to an institution role
func (h *Handler) AssignPermissionsToInstitutionRole(w http.ResponseWriter, r *http.Request) {
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

	if err := h.service.ValidateInstitutionRoleMenuScope(r.Context(), *tenantID, req.PermissionIDs); err != nil {
		if errors.Is(err, ErrMenuOutOfPolicy) {
			httputil.WriteForbidden(w, err.Error())
			return
		}
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	if err := h.service.AssignPermissionsToInstitutionRole(r.Context(), id, req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Permissions assigned successfully"})
}

// RemovePermissionsFromInstitutionRole handles removing permissions from an institution role
func (h *Handler) RemovePermissionsFromInstitutionRole(w http.ResponseWriter, r *http.Request) {
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

	if err := h.service.RemovePermissionsFromInstitutionRole(r.Context(), id, req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Permissions removed successfully"})
}

// GetInstitutionRolePermissions handles getting permissions for an institution role
func (h *Handler) GetInstitutionRolePermissions(w http.ResponseWriter, r *http.Request) {
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

	permissions, err := h.service.GetInstitutionRolePermissions(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, permissions)
}

// Employee Role handlers

// GetEmployeeRole handles getting the role for an employee
func (h *Handler) GetEmployeeRole(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid employee ID")
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

	if err := h.service.SetEmployeeRole(r.Context(), id, req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Employee role set successfully"})
}

// RemoveEmployeeRole handles removing the role from an employee
func (h *Handler) RemoveEmployeeRole(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid employee ID")
		return
	}

	if err := h.service.RemoveEmployeeRole(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Employee role removed successfully"})
}
