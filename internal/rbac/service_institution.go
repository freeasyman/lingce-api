package rbac

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
)

var ErrMenuOutOfPolicy = errors.New("menu out of tenant policy")

// Institution Role operations

// ListInstitutionRoles retrieves a paginated list of institution roles
func (s *Service) ListInstitutionRoles(ctx context.Context, tenantID int64, req InstitutionRoleListRequest) ([]*InstitutionRoleResponse, int, error) {
	// Set default pagination
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}

	roles, total, err := s.store.ListInstitutionRoles(ctx, tenantID, req)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*InstitutionRoleResponse, len(roles))
	for i, r := range roles {
		responses[i] = toInstitutionRoleResponse(r)
	}

	return responses, total, nil
}

// GetInstitutionRole retrieves an institution role by ID
func (s *Service) GetInstitutionRole(ctx context.Context, tenantID, id int64) (*InstitutionRoleResponse, error) {
	role, err := s.store.GetInstitutionRoleByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	return toInstitutionRoleResponse(role), nil
}

// CreateInstitutionRole creates a new institution role
func (s *Service) CreateInstitutionRole(ctx context.Context, tenantID int64, req CreateInstitutionRoleRequest) (*InstitutionRoleResponse, error) {
	// Validate request
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.Code == "" {
		return nil, fmt.Errorf("code is required")
	}

	role, err := s.store.CreateInstitutionRole(ctx, tenantID, req)
	if err != nil {
		return nil, err
	}

	return toInstitutionRoleResponse(role), nil
}

// UpdateInstitutionRole updates an institution role
func (s *Service) UpdateInstitutionRole(ctx context.Context, tenantID, id int64, req UpdateInstitutionRoleRequest) (*InstitutionRoleResponse, error) {
	role, err := s.store.UpdateInstitutionRole(ctx, tenantID, id, req)
	if err != nil {
		return nil, err
	}

	return toInstitutionRoleResponse(role), nil
}

// DeleteInstitutionRole deletes an institution role
func (s *Service) DeleteInstitutionRole(ctx context.Context, tenantID, id int64) error {
	return s.store.DeleteInstitutionRole(ctx, tenantID, id)
}

// toInstitutionRoleResponse converts an InstitutionRole to InstitutionRoleResponse
func toInstitutionRoleResponse(r *InstitutionRole) *InstitutionRoleResponse {
	return &InstitutionRoleResponse{
		ID:          r.ID,
		TenantID:    r.TenantID,
		Name:        r.Name,
		Code:        r.Code,
		Description: r.Description,
		IsActive:    r.IsActive,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

// Institution Menu operations

func (s *Service) listEffectiveInstitutionMenus(ctx context.Context, tenantID *int64, req InstitutionMenuListRequest) ([]*InstitutionMenu, error) {
	menus, err := s.store.ListInstitutionMenus(ctx, tenantID, req)
	if err != nil {
		return nil, err
	}
	if tenantID == nil {
		return menus, nil
	}

	unrestricted, allowedCodes, err := s.store.GetTenantAllowedMenuCodes(ctx, *tenantID)
	if err != nil {
		return nil, err
	}
	if unrestricted {
		return menus, nil
	}
	return filterMenusByAllowedCodes(menus, allowedCodes), nil
}

func orderedMenuCodes(menus []*InstitutionMenu, allowed map[string]struct{}) []string {
	if len(allowed) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(allowed))
	seen := make(map[string]struct{}, len(allowed))
	for _, menu := range menus {
		if _, ok := allowed[menu.Code]; !ok {
			continue
		}
		if _, exists := seen[menu.Code]; exists {
			continue
		}
		seen[menu.Code] = struct{}{}
		out = append(out, menu.Code)
	}
	return out
}

func filterPermissionsByAllowedCodes(permissions []*InstitutionPermission, allowed map[string]struct{}) []*InstitutionPermission {
	if len(allowed) == 0 {
		return []*InstitutionPermission{}
	}
	out := make([]*InstitutionPermission, 0, len(permissions))
	for _, permission := range permissions {
		if _, ok := allowed[permission.Code]; ok {
			out = append(out, permission)
		}
	}
	return out
}

func (s *Service) getTenantMenuScope(ctx context.Context, tenantID int64) (map[int64]struct{}, map[int64]struct{}, error) {
	menus, err := s.store.ListInstitutionMenus(ctx, &tenantID, InstitutionMenuListRequest{})
	if err != nil {
		return nil, nil, err
	}
	allByID := make(map[int64]struct{}, len(menus))
	for _, menu := range menus {
		allByID[menu.ID] = struct{}{}
	}

	unrestricted, allowedCodes, err := s.store.GetTenantAllowedMenuCodes(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	effectiveMenus := menus
	if !unrestricted {
		effectiveMenus = filterMenusByAllowedCodes(menus, allowedCodes)
	}
	effectiveByID := make(map[int64]struct{}, len(effectiveMenus))
	for _, menu := range effectiveMenus {
		effectiveByID[menu.ID] = struct{}{}
	}
	return allByID, effectiveByID, nil
}

// ListInstitutionMenus retrieves all institution menus
func (s *Service) ListInstitutionMenus(ctx context.Context, tenantID *int64, req InstitutionMenuListRequest) ([]*InstitutionMenuResponse, error) {
	menus, err := s.listEffectiveInstitutionMenus(ctx, tenantID, req)
	if err != nil {
		return nil, err
	}

	responses := make([]*InstitutionMenuResponse, len(menus))
	for i, m := range menus {
		responses[i] = toInstitutionMenuResponse(m)
	}

	return responses, nil
}

func filterMenusByAllowedCodes(menus []*InstitutionMenu, allowed map[string]struct{}) []*InstitutionMenu {
	if len(allowed) == 0 {
		return []*InstitutionMenu{}
	}

	byID := make(map[int64]*InstitutionMenu, len(menus))
	selected := make(map[int64]struct{}, len(menus))
	for _, m := range menus {
		byID[m.ID] = m
		if _, ok := allowed[m.Code]; ok {
			selected[m.ID] = struct{}{}
		}
	}

	changed := true
	for changed {
		changed = false
		for id := range selected {
			menu := byID[id]
			if menu == nil || menu.ParentID == nil {
				continue
			}
			if _, ok := selected[*menu.ParentID]; !ok {
				if _, exists := byID[*menu.ParentID]; exists {
					selected[*menu.ParentID] = struct{}{}
					changed = true
				}
			}
		}
	}

	out := make([]*InstitutionMenu, 0, len(selected))
	for _, m := range menus {
		if _, ok := selected[m.ID]; ok {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SortOrder == out[j].SortOrder {
			return out[i].ID < out[j].ID
		}
		return out[i].SortOrder < out[j].SortOrder
	})
	return out
}

func (s *Service) getEmployeeEffectiveInstitutionMenus(ctx context.Context, employeeID int64) (int64, string, []*InstitutionMenu, []string, error) {
	tenantID, err := s.store.getEmployeeTenantID(ctx, employeeID)
	if err != nil {
		return 0, "", nil, nil, err
	}

	activeOnly := true
	menus, err := s.store.ListInstitutionMenus(ctx, &tenantID, InstitutionMenuListRequest{IsActive: &activeOnly})
	if err != nil {
		return 0, "", nil, nil, err
	}

	role, err := s.store.GetEmployeeRole(ctx, employeeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return tenantID, "", []*InstitutionMenu{}, []string{}, nil
		}
		return 0, "", nil, nil, err
	}

	roleCode := normalizeInstitutionRoleCode(role.Code)
	if roleCode == "" {
		return tenantID, "", []*InstitutionMenu{}, []string{}, nil
	}

	roleGrantedCodes, err := s.store.GetRoleGrantedMenuCodes(ctx, tenantID, roleCode)
	if err != nil {
		return 0, "", nil, nil, err
	}
	if len(roleGrantedCodes) == 0 {
		return tenantID, roleCode, []*InstitutionMenu{}, []string{}, nil
	}

	unrestricted, tenantAllowedCodes, err := s.store.GetTenantAllowedMenuCodes(ctx, tenantID)
	if err != nil {
		return 0, "", nil, nil, err
	}

	effectiveDirectCodes := make(map[string]struct{})
	for _, menu := range menus {
		menuCode := menu.Code
		if _, granted := roleGrantedCodes[menuCode]; !granted {
			continue
		}
		if !unrestricted {
			if _, allowed := tenantAllowedCodes[menuCode]; !allowed {
				continue
			}
		}
		effectiveDirectCodes[menuCode] = struct{}{}
	}

	if len(effectiveDirectCodes) == 0 {
		return tenantID, roleCode, []*InstitutionMenu{}, []string{}, nil
	}

	effectiveMenus := filterMenusByAllowedCodes(menus, effectiveDirectCodes)
	return tenantID, roleCode, effectiveMenus, orderedMenuCodes(menus, effectiveDirectCodes), nil
}

func buildInstitutionMenuTree(menus []*InstitutionMenu) []*InstitutionMenuResponse {
	menuMap := make(map[int64]*InstitutionMenuResponse, len(menus))
	roots := make([]*InstitutionMenuResponse, 0)

	for _, m := range menus {
		node := toInstitutionMenuResponse(m)
		node.Children = []*InstitutionMenuResponse{}
		menuMap[m.ID] = node
	}

	for _, m := range menus {
		node := menuMap[m.ID]
		if m.ParentID == nil {
			roots = append(roots, node)
			continue
		}
		if parent, ok := menuMap[*m.ParentID]; ok {
			parent.Children = append(parent.Children, node)
		}
	}

	return roots
}

// GetInstitutionMenuTree retrieves institution menus as a tree.
func (s *Service) GetInstitutionMenuTree(ctx context.Context, tenantID *int64) ([]*InstitutionMenuResponse, error) {
	menus, err := s.listEffectiveInstitutionMenus(ctx, tenantID, InstitutionMenuListRequest{})
	if err != nil {
		return nil, err
	}
	return buildInstitutionMenuTree(menus), nil
}

// GetInstitutionMenu retrieves an institution menu by ID
func (s *Service) GetInstitutionMenu(ctx context.Context, tenantID *int64, id int64) (*InstitutionMenuResponse, error) {
	menu, err := s.store.GetInstitutionMenuByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	return toInstitutionMenuResponse(menu), nil
}

// CreateInstitutionMenu creates a new institution menu
func (s *Service) CreateInstitutionMenu(ctx context.Context, tenantID *int64, req CreateInstitutionMenuRequest) (*InstitutionMenuResponse, error) {
	// Validate request
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.Code == "" {
		return nil, fmt.Errorf("code is required")
	}

	menu, err := s.store.CreateInstitutionMenu(ctx, tenantID, req)
	if err != nil {
		return nil, err
	}

	return toInstitutionMenuResponse(menu), nil
}

// UpdateInstitutionMenu updates an institution menu
func (s *Service) UpdateInstitutionMenu(ctx context.Context, tenantID *int64, id int64, req UpdateInstitutionMenuRequest) (*InstitutionMenuResponse, error) {
	menu, err := s.store.UpdateInstitutionMenu(ctx, tenantID, id, req)
	if err != nil {
		return nil, err
	}

	return toInstitutionMenuResponse(menu), nil
}

// DeleteInstitutionMenu deletes an institution menu
func (s *Service) DeleteInstitutionMenu(ctx context.Context, tenantID *int64, id int64) error {
	return s.store.DeleteInstitutionMenu(ctx, tenantID, id)
}

// toInstitutionMenuResponse converts an InstitutionMenu to InstitutionMenuResponse
func toInstitutionMenuResponse(m *InstitutionMenu) *InstitutionMenuResponse {
	return &InstitutionMenuResponse{
		ID:                  m.ID,
		TenantID:            m.TenantID,
		Name:                m.Name,
		Code:                m.Code,
		Path:                m.Path,
		Icon:                m.Icon,
		ParentID:            m.ParentID,
		SortOrder:           m.SortOrder,
		IsActive:            m.IsActive,
		IsFeatureAssignable: m.IsFeatureAssignable,
		IsDefaultForAdmin:   m.IsDefaultForAdmin,
		FeatureCode:         m.FeatureCode,
		FeatureName:         m.FeatureName,
		CreatedAt:           m.CreatedAt,
		UpdatedAt:           m.UpdatedAt,
	}
}

// Institution Permission operations

// AssignPermissionsToInstitutionRole assigns permissions to an institution role
func (s *Service) AssignPermissionsToInstitutionRole(ctx context.Context, tenantID, roleID int64, req AssignPermissionsRequest) error {
	return s.store.AssignPermissionsToInstitutionRole(ctx, tenantID, roleID, req.PermissionIDs)
}

// RemovePermissionsFromInstitutionRole removes permissions from an institution role
func (s *Service) RemovePermissionsFromInstitutionRole(ctx context.Context, tenantID, roleID int64, req AssignPermissionsRequest) error {
	return s.store.RemovePermissionsFromInstitutionRole(ctx, tenantID, roleID, req.PermissionIDs)
}

// GetInstitutionRolePermissions retrieves permissions for an institution role
func (s *Service) GetInstitutionRolePermissions(ctx context.Context, tenantID, roleID int64) ([]*PermissionResponse, error) {
	permissions, err := s.store.GetInstitutionRolePermissions(ctx, tenantID, roleID)
	if err != nil {
		return nil, err
	}

	unrestricted, allowedCodes, err := s.store.GetTenantAllowedMenuCodes(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if !unrestricted {
		permissions = filterPermissionsByAllowedCodes(permissions, allowedCodes)
	}

	responses := make([]*PermissionResponse, len(permissions))
	for i, p := range permissions {
		responses[i] = toInstitutionPermissionResponse(p)
	}

	return responses, nil
}

// toInstitutionPermissionResponse converts an InstitutionPermission to PermissionResponse
func toInstitutionPermissionResponse(p *InstitutionPermission) *PermissionResponse {
	return &PermissionResponse{
		ID:          p.ID,
		Name:        p.Name,
		Code:        p.Code,
		Resource:    p.Resource,
		Action:      p.Action,
		Description: p.Description,
	}
}

// Employee Role operations

// GetEmployeeRole retrieves the role for an employee
func (s *Service) GetEmployeeRole(ctx context.Context, employeeID int64) (*EmployeeRoleResponse, error) {
	role, err := s.store.GetEmployeeRole(ctx, employeeID)
	if err != nil {
		// If no role found, return empty response
		return &EmployeeRoleResponse{
			EmployeeID: employeeID,
			Roles:      []InstitutionRoleResponse{},
		}, nil
	}

	return &EmployeeRoleResponse{
		EmployeeID: employeeID,
		Roles:      []InstitutionRoleResponse{*toInstitutionRoleResponse(role)},
	}, nil
}

// GetEmployeeEffectiveMenus retrieves the final effective institution menus for an employee.
func (s *Service) GetEmployeeEffectiveMenus(ctx context.Context, employeeID int64) (*EmployeeEffectiveMenuResponse, error) {
	tenantID, roleCode, menus, codes, err := s.getEmployeeEffectiveInstitutionMenus(ctx, employeeID)
	if err != nil {
		return nil, err
	}

	responses := make([]*InstitutionMenuResponse, len(menus))
	for i, menu := range menus {
		responses[i] = toInstitutionMenuResponse(menu)
	}

	return &EmployeeEffectiveMenuResponse{
		EmployeeID: employeeID,
		TenantID:   tenantID,
		RoleCode:   roleCode,
		MenuCodes:  codes,
		Menus:      responses,
	}, nil
}

// SetEmployeeRole sets the role for an employee
func (s *Service) SetEmployeeRole(ctx context.Context, employeeID int64, req SetEmployeeRoleRequest) error {
	return s.store.SetEmployeeRole(ctx, employeeID, req.RoleID)
}

// RemoveEmployeeRole removes the role from an employee
func (s *Service) RemoveEmployeeRole(ctx context.Context, employeeID int64) error {
	return s.store.RemoveEmployeeRole(ctx, employeeID)
}

// GetDepartmentRole retrieves the default role for a department.
func (s *Service) GetDepartmentRole(ctx context.Context, departmentID int64) (*DepartmentRoleResponse, error) {
	role, err := s.store.GetDepartmentRole(ctx, departmentID)
	if err != nil {
		return &DepartmentRoleResponse{
			DepartmentID: departmentID,
			Roles:        []InstitutionRoleResponse{},
		}, nil
	}
	return &DepartmentRoleResponse{
		DepartmentID: departmentID,
		Roles:        []InstitutionRoleResponse{*toInstitutionRoleResponse(role)},
	}, nil
}

// SetDepartmentRole sets the default role for a department.
func (s *Service) SetDepartmentRole(ctx context.Context, departmentID int64, req SetDepartmentRoleRequest) error {
	if req.RoleID == nil && (req.RoleCode == nil || *req.RoleCode == "") {
		return fmt.Errorf("role_id or role_code is required")
	}
	return s.store.SetDepartmentRole(ctx, departmentID, req)
}

// RemoveDepartmentRole removes the default role for a department.
func (s *Service) RemoveDepartmentRole(ctx context.Context, departmentID int64) error {
	return s.store.RemoveDepartmentRole(ctx, departmentID)
}

func (s *Service) ValidateInstitutionRoleMenuScope(ctx context.Context, tenantID int64, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	allByID, effectiveByID, err := s.getTenantMenuScope(ctx, tenantID)
	if err != nil {
		return err
	}

	for _, id := range ids {
		if _, exists := allByID[id]; !exists {
			continue
		}
		if _, ok := effectiveByID[id]; !ok {
			return fmt.Errorf("%w: %d", ErrMenuOutOfPolicy, id)
		}
	}
	return nil
}
