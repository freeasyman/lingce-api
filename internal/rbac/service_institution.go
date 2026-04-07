package rbac

import (
	"context"
	"fmt"
)

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

// ListInstitutionMenus retrieves all institution menus
func (s *Service) ListInstitutionMenus(ctx context.Context, tenantID *int64, req InstitutionMenuListRequest) ([]*InstitutionMenuResponse, error) {
	menus, err := s.store.ListInstitutionMenus(ctx, tenantID, req)
	if err != nil {
		return nil, err
	}

	responses := make([]*InstitutionMenuResponse, len(menus))
	for i, m := range menus {
		responses[i] = toInstitutionMenuResponse(m)
	}

	return responses, nil
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
		ID:        m.ID,
		TenantID:  m.TenantID,
		Name:      m.Name,
		Code:      m.Code,
		Path:      m.Path,
		Icon:      m.Icon,
		ParentID:  m.ParentID,
		SortOrder: m.SortOrder,
		IsActive:  m.IsActive,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
}

// Institution Permission operations

// AssignPermissionsToInstitutionRole assigns permissions to an institution role
func (s *Service) AssignPermissionsToInstitutionRole(ctx context.Context, roleID int64, req AssignPermissionsRequest) error {
	return s.store.AssignPermissionsToInstitutionRole(ctx, roleID, req.PermissionIDs)
}

// RemovePermissionsFromInstitutionRole removes permissions from an institution role
func (s *Service) RemovePermissionsFromInstitutionRole(ctx context.Context, roleID int64, req AssignPermissionsRequest) error {
	return s.store.RemovePermissionsFromInstitutionRole(ctx, roleID, req.PermissionIDs)
}

// GetInstitutionRolePermissions retrieves permissions for an institution role
func (s *Service) GetInstitutionRolePermissions(ctx context.Context, roleID int64) ([]*PermissionResponse, error) {
	permissions, err := s.store.GetInstitutionRolePermissions(ctx, roleID)
	if err != nil {
		return nil, err
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

// SetEmployeeRole sets the role for an employee
func (s *Service) SetEmployeeRole(ctx context.Context, employeeID int64, req SetEmployeeRoleRequest) error {
	return s.store.SetEmployeeRole(ctx, employeeID, req.RoleID)
}

// RemoveEmployeeRole removes the role from an employee
func (s *Service) RemoveEmployeeRole(ctx context.Context, employeeID int64) error {
	return s.store.RemoveEmployeeRole(ctx, employeeID)
}
