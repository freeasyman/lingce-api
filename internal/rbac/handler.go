package rbac

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/freeasyman/lingce-api/internal/middleware"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/freeasyman/lingce-api/pkg/httputil"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes registers rbac routes
func (h *Handler) RegisterRoutes(mux *http.ServeMux, jwtSecret string) {
	authMw := middleware.Auth(jwtSecret)

	// Role resource routes
	mux.Handle("GET /api/v1/roles", authMw(http.HandlerFunc(h.ListRoles)))
	mux.Handle("POST /api/v1/roles", authMw(http.HandlerFunc(h.CreateRole)))
	mux.Handle("GET /api/v1/roles/{id}", authMw(http.HandlerFunc(h.GetRole)))
	mux.Handle("PUT /api/v1/roles/{id}", authMw(http.HandlerFunc(h.UpdateRole)))
	mux.Handle("DELETE /api/v1/roles/{id}", authMw(http.HandlerFunc(h.DeleteRole)))
	mux.Handle("GET /api/v1/roles/{id}/permissions", authMw(http.HandlerFunc(h.GetRolePermissionsByScope)))
	mux.Handle("POST /api/v1/roles/{id}/permissions", authMw(http.HandlerFunc(h.AssignPermissionsToRoleByScope)))
	mux.Handle("DELETE /api/v1/roles/{id}/permissions", authMw(http.HandlerFunc(h.RemovePermissionsFromRoleByScope)))
	mux.Handle("GET /api/v1/roles/{id}/menus", authMw(http.HandlerFunc(h.GetRoleMenusByScope)))
	mux.Handle("PUT /api/v1/roles/{id}/menus", authMw(http.HandlerFunc(h.AssignMenusToRoleByScope)))
	mux.Handle("POST /api/v1/roles/{id}/menus", authMw(http.HandlerFunc(h.AssignMenusToRoleByScope)))

	// Menu resource routes
	mux.Handle("GET /api/v1/menus", authMw(http.HandlerFunc(h.ListMenus)))
	mux.Handle("POST /api/v1/menus", authMw(http.HandlerFunc(h.CreateMenu)))
	mux.Handle("GET /api/v1/menus/{id}", authMw(http.HandlerFunc(h.GetMenu)))
	mux.Handle("PUT /api/v1/menus/{id}", authMw(http.HandlerFunc(h.UpdateMenu)))
	mux.Handle("DELETE /api/v1/menus/{id}", authMw(http.HandlerFunc(h.DeleteMenu)))
	mux.Handle("GET /api/v1/menus/tree", authMw(http.HandlerFunc(h.GetMenuTreeByScope)))
	mux.Handle("PUT /api/v1/menus/sort", authMw(http.HandlerFunc(h.UpdateMenuSortByScope)))

	// Operations admin routes
	mux.Handle("GET /api/v1/roles/admins", authMw(http.HandlerFunc(h.ListOperationsAdmins)))
	mux.Handle("POST /api/v1/roles/admins", authMw(http.HandlerFunc(h.CreateOperationsAdmin)))
	mux.Handle("GET /api/v1/roles/admins/id/{id}", authMw(http.HandlerFunc(h.GetOperationsAdmin)))
	mux.Handle("PUT /api/v1/roles/admins/id/{id}", authMw(http.HandlerFunc(h.UpdateOperationsAdmin)))
	mux.Handle("DELETE /api/v1/roles/admins/id/{id}", authMw(http.HandlerFunc(h.DeleteOperationsAdmin)))
	mux.Handle("POST /api/v1/roles/admins/id/{id}/actions/reset-password", authMw(http.HandlerFunc(h.ResetAdminPassword)))

	// Institution employee/department role routes
	mux.Handle("GET /api/v1/employees/{id}/role", authMw(http.HandlerFunc(h.GetEmployeeRole)))
	mux.Handle("PUT /api/v1/employees/{id}/role", authMw(http.HandlerFunc(h.SetEmployeeRole)))
	mux.Handle("DELETE /api/v1/employees/{id}/role", authMw(http.HandlerFunc(h.RemoveEmployeeRole)))
	mux.Handle("GET /api/v1/departments/{id}/role", authMw(http.HandlerFunc(h.GetDepartmentRole)))
	mux.Handle("PUT /api/v1/departments/{id}/role", authMw(http.HandlerFunc(h.SetDepartmentRole)))
	mux.Handle("DELETE /api/v1/departments/{id}/role", authMw(http.HandlerFunc(h.RemoveDepartmentRole)))

}

func (h *Handler) roleScope(r *http.Request) string {
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		return "ops"
	}
	return scope
}

func (h *Handler) ListRoles(w http.ResponseWriter, r *http.Request) {
	if h.roleScope(r) == "institution" {
		h.ListInstitutionRoles(w, r)
		return
	}
	h.ListOperationsRoles(w, r)
}

func (h *Handler) CreateRole(w http.ResponseWriter, r *http.Request) {
	if h.roleScope(r) == "institution" {
		h.CreateInstitutionRole(w, r)
		return
	}
	h.CreateOperationsRole(w, r)
}

func (h *Handler) GetRole(w http.ResponseWriter, r *http.Request) {
	if h.roleScope(r) == "institution" {
		h.GetInstitutionRole(w, r)
		return
	}
	h.GetOperationsRoleByIdentifier(w, r)
}

func (h *Handler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	if h.roleScope(r) == "institution" {
		h.UpdateInstitutionRole(w, r)
		return
	}
	h.UpdateOperationsRoleByIdentifier(w, r)
}

func (h *Handler) DeleteRole(w http.ResponseWriter, r *http.Request) {
	if h.roleScope(r) == "institution" {
		h.DeleteInstitutionRole(w, r)
		return
	}
	h.DeleteOperationsRoleByIdentifier(w, r)
}

func (h *Handler) GetRolePermissionsByScope(w http.ResponseWriter, r *http.Request) {
	if h.roleScope(r) == "institution" {
		h.GetInstitutionRolePermissions(w, r)
		return
	}
	h.GetOperationsRolePermissionsByIdentifier(w, r)
}

func (h *Handler) AssignPermissionsToRoleByScope(w http.ResponseWriter, r *http.Request) {
	if h.roleScope(r) == "institution" {
		h.AssignPermissionsToInstitutionRole(w, r)
		return
	}
	h.AssignOperationsPermissionsByIdentifier(w, r)
}

func (h *Handler) RemovePermissionsFromRoleByScope(w http.ResponseWriter, r *http.Request) {
	if h.roleScope(r) == "institution" {
		h.RemovePermissionsFromInstitutionRole(w, r)
		return
	}
	h.RemoveOperationsPermissionsByIdentifier(w, r)
}

func (h *Handler) GetRoleMenusByScope(w http.ResponseWriter, r *http.Request) {
	if h.roleScope(r) == "institution" {
		h.GetInstitutionRolePermissions(w, r)
		return
	}
	h.GetRoleMenus(w, r)
}

func (h *Handler) AssignMenusToRoleByScope(w http.ResponseWriter, r *http.Request) {
	if h.roleScope(r) == "institution" {
		h.AssignPermissionsToInstitutionRole(w, r)
		return
	}
	h.AssignMenusToRole(w, r)
}

func (h *Handler) ListMenus(w http.ResponseWriter, r *http.Request) {
	if h.roleScope(r) == "institution" {
		h.ListInstitutionMenus(w, r)
		return
	}
	h.ListOperationsMenus(w, r)
}

func (h *Handler) CreateMenu(w http.ResponseWriter, r *http.Request) {
	if h.roleScope(r) == "institution" {
		h.CreateInstitutionMenu(w, r)
		return
	}
	h.CreateOperationsMenu(w, r)
}

func (h *Handler) GetMenu(w http.ResponseWriter, r *http.Request) {
	if h.roleScope(r) == "institution" {
		h.GetInstitutionMenu(w, r)
		return
	}
	h.GetOperationsMenu(w, r)
}

func (h *Handler) UpdateMenu(w http.ResponseWriter, r *http.Request) {
	if h.roleScope(r) == "institution" {
		h.UpdateInstitutionMenu(w, r)
		return
	}
	h.UpdateOperationsMenu(w, r)
}

func (h *Handler) DeleteMenu(w http.ResponseWriter, r *http.Request) {
	if h.roleScope(r) == "institution" {
		h.DeleteInstitutionMenu(w, r)
		return
	}
	h.DeleteOperationsMenu(w, r)
}

func (h *Handler) GetMenuTreeByScope(w http.ResponseWriter, r *http.Request) {
	if h.roleScope(r) == "institution" {
		httputil.WriteBadRequest(w, "institution scope tree is not supported")
		return
	}
	h.GetOperationsMenuTree(w, r)
}

func (h *Handler) UpdateMenuSortByScope(w http.ResponseWriter, r *http.Request) {
	if h.roleScope(r) == "institution" {
		httputil.WriteBadRequest(w, "institution scope sort is not supported")
		return
	}
	h.UpdateMenuSort(w, r)
}

// isAdmin checks if the current user is an admin
func (h *Handler) isAdmin(r *http.Request) bool {
	claims := middleware.GetUserClaims(r.Context())
	return claims != nil && claims.UserType == auth.UserTypeAdmin
}

// getTenantID gets the tenant ID from the current user
func (h *Handler) getTenantID(r *http.Request) *int64 {
	if q := r.URL.Query().Get("tenant_id"); q != "" {
		if id, err := strconv.ParseInt(q, 10, 64); err == nil && id > 0 {
			return &id
		}
	}
	claims := middleware.GetUserClaims(r.Context())
	if claims != nil && claims.TenantID != nil {
		return claims.TenantID
	}
	return nil
}

// Operations Role handlers

// ListOperationsRoles handles listing operations roles
func (h *Handler) ListOperationsRoles(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req RoleListRequest
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

	roles, total, err := h.service.ListOperationsRoles(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WritePaginated(w, roles, int64(total), req.Page, req.PageSize)
}

// CreateOperationsRole handles creating an operations role
func (h *Handler) CreateOperationsRole(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req CreateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	role, err := h.service.CreateOperationsRole(r.Context(), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, role)
}

// GetOperationsRole handles getting an operations role by ID
func (h *Handler) GetOperationsRole(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid role ID")
		return
	}

	role, err := h.service.GetOperationsRole(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, role)
}

// UpdateOperationsRole handles updating an operations role
func (h *Handler) UpdateOperationsRole(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid role ID")
		return
	}

	var req UpdateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	role, err := h.service.UpdateOperationsRole(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, role)
}

// DeleteOperationsRole handles deleting an operations role
func (h *Handler) DeleteOperationsRole(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid role ID")
		return
	}

	if err := h.service.DeleteOperationsRole(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Role deleted successfully"})
}

// AssignPermissionsToRole handles assigning permissions to a role
func (h *Handler) AssignPermissionsToRole(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
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

	if err := h.service.AssignPermissionsToRole(r.Context(), id, req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Permissions assigned successfully"})
}

// RemovePermissionsFromRole handles removing permissions from a role
func (h *Handler) RemovePermissionsFromRole(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
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

	if err := h.service.RemovePermissionsFromRole(r.Context(), id, req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Permissions removed successfully"})
}

// GetRolePermissions handles getting permissions for a role
func (h *Handler) GetRolePermissions(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid role ID")
		return
	}

	permissions, err := h.service.GetRolePermissions(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, permissions)
}

// Operations Menu handlers

// ListOperationsMenus handles listing operations menus
func (h *Handler) ListOperationsMenus(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req MenuListRequest
	req.Name = r.URL.Query().Get("name")

	if isActiveStr := r.URL.Query().Get("is_active"); isActiveStr != "" {
		isActive := isActiveStr == "true"
		req.IsActive = &isActive
	}

	menus, err := h.service.ListOperationsMenus(r.Context(), req)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, menus)
}

// GetOperationsMenuTree handles getting operations menus as a tree
func (h *Handler) GetOperationsMenuTree(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	tree, err := h.service.GetOperationsMenuTree(r.Context())
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, tree)
}

// CreateOperationsMenu handles creating an operations menu
func (h *Handler) CreateOperationsMenu(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req CreateMenuRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	menu, err := h.service.CreateOperationsMenu(r.Context(), req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, menu)
}

// GetOperationsMenu handles getting an operations menu by ID
func (h *Handler) GetOperationsMenu(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid menu ID")
		return
	}

	menu, err := h.service.GetOperationsMenu(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, menu)
}

// UpdateOperationsMenu handles updating an operations menu
func (h *Handler) UpdateOperationsMenu(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid menu ID")
		return
	}

	var req UpdateMenuRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	menu, err := h.service.UpdateOperationsMenu(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, menu)
}

// DeleteOperationsMenu handles deleting an operations menu
func (h *Handler) DeleteOperationsMenu(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid menu ID")
		return
	}

	if err := h.service.DeleteOperationsMenu(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Menu deleted successfully"})
}

// UpdateMenuSort handles updating menu sort orders
func (h *Handler) UpdateMenuSort(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	var req MenuSortRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if err := h.service.UpdateMenuSort(r.Context(), req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Menu sort updated successfully"})
}

// GetRoleMenus handles getting menus for a role
func (h *Handler) GetRoleMenus(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	roleIdentifier := r.PathValue("id")
	id, err := h.parseRoleID(r.Context(), roleIdentifier)
	if err != nil {
		// Compatibility: role code may be preconfigured in frontend but not yet in DB.
		if _, parseErr := strconv.ParseInt(roleIdentifier, 10, 64); parseErr != nil {
			httputil.WriteSuccess(w, []MenuResponse{})
			return
		}
		httputil.WriteBadRequest(w, "Invalid role ID")
		return
	}

	menus, err := h.service.GetRoleMenus(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, menus)
}

// AssignMenusToRole handles assigning menus to a role
func (h *Handler) AssignMenusToRole(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := h.parseRoleID(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid role ID")
		return
	}

	var req AssignMenusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if err := h.service.AssignMenusToRole(r.Context(), id, req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Menus assigned successfully"})
}

func (h *Handler) parseRoleID(ctx context.Context, roleIdentifier string) (int64, error) {
	if id, err := strconv.ParseInt(roleIdentifier, 10, 64); err == nil {
		return id, nil
	}
	role, err := h.service.GetOperationsRoleByCode(ctx, roleIdentifier)
	if err != nil {
		return 0, err
	}
	return role.ID, nil
}

func (h *Handler) GetOperationsRoleByIdentifier(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := h.parseRoleID(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid role ID")
		return
	}

	role, err := h.service.GetOperationsRole(r.Context(), id)
	if err != nil {
		httputil.WriteNotFound(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, role)
}

func (h *Handler) UpdateOperationsRoleByIdentifier(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := h.parseRoleID(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid role ID")
		return
	}

	var req UpdateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	role, err := h.service.UpdateOperationsRole(r.Context(), id, req)
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, role)
}

func (h *Handler) DeleteOperationsRoleByIdentifier(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := h.parseRoleID(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid role ID")
		return
	}

	if err := h.service.DeleteOperationsRole(r.Context(), id); err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Role deleted successfully"})
}

func (h *Handler) GetOperationsRolePermissionsByIdentifier(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := h.parseRoleID(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid role ID")
		return
	}

	permissions, err := h.service.GetRolePermissions(r.Context(), id)
	if err != nil {
		httputil.WriteInternalError(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, permissions)
}

func (h *Handler) AssignOperationsPermissionsByIdentifier(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := h.parseRoleID(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid role ID")
		return
	}

	var req AssignPermissionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if err := h.service.AssignPermissionsToRole(r.Context(), id, req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Permissions assigned successfully"})
}

func (h *Handler) RemoveOperationsPermissionsByIdentifier(w http.ResponseWriter, r *http.Request) {
	if !h.isAdmin(r) {
		httputil.WriteForbidden(w, "Admin access required")
		return
	}

	id, err := h.parseRoleID(r.Context(), r.PathValue("id"))
	if err != nil {
		httputil.WriteBadRequest(w, "Invalid role ID")
		return
	}

	var req AssignPermissionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteBadRequest(w, "Invalid request body")
		return
	}

	if err := h.service.RemovePermissionsFromRole(r.Context(), id, req); err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}

	httputil.WriteSuccess(w, map[string]string{"message": "Permissions removed successfully"})
}
