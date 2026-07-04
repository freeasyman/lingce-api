package rbac

import (
	"context"
	"fmt"
	"sort"

	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store}
}

// Operations Role operations

// ListOperationsRoles retrieves a paginated list of operations roles
func (s *Service) ListOperationsRoles(ctx context.Context, req RoleListRequest) ([]*RoleResponse, int, error) {
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

	roles, total, err := s.store.ListOperationsRoles(ctx, req)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*RoleResponse, len(roles))
	for i, r := range roles {
		responses[i] = toRoleResponse(r)
	}

	return responses, total, nil
}

// GetOperationsRole retrieves an operations role by ID
func (s *Service) GetOperationsRole(ctx context.Context, id int64) (*RoleResponse, error) {
	role, err := s.store.GetOperationsRoleByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return toRoleResponse(role), nil
}

// GetOperationsRoleByCode retrieves an operations role by code.
func (s *Service) GetOperationsRoleByCode(ctx context.Context, code string) (*RoleResponse, error) {
	role, err := s.store.GetOperationsRoleByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	return toRoleResponse(role), nil
}

// CreateOperationsRole creates a new operations role
func (s *Service) CreateOperationsRole(ctx context.Context, req CreateRoleRequest) (*RoleResponse, error) {
	// Validate request
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.Code == "" {
		return nil, fmt.Errorf("code is required")
	}

	role, err := s.store.CreateOperationsRole(ctx, req)
	if err != nil {
		return nil, err
	}

	return toRoleResponse(role), nil
}

// UpdateOperationsRole updates an operations role
func (s *Service) UpdateOperationsRole(ctx context.Context, id int64, req UpdateRoleRequest) (*RoleResponse, error) {
	role, err := s.store.UpdateOperationsRole(ctx, id, req)
	if err != nil {
		return nil, err
	}

	return toRoleResponse(role), nil
}

// DeleteOperationsRole deletes an operations role
func (s *Service) DeleteOperationsRole(ctx context.Context, id int64) error {
	return s.store.DeleteOperationsRole(ctx, id)
}

// AssignPermissionsToRole assigns permissions to a role
func (s *Service) AssignPermissionsToRole(ctx context.Context, roleID int64, req AssignPermissionsRequest) error {
	return s.store.AssignPermissionsToRole(ctx, roleID, req.PermissionIDs)
}

// RemovePermissionsFromRole removes permissions from a role
func (s *Service) RemovePermissionsFromRole(ctx context.Context, roleID int64, req AssignPermissionsRequest) error {
	return s.store.RemovePermissionsFromRole(ctx, roleID, req.PermissionIDs)
}

// GetRolePermissions retrieves permissions for a role
func (s *Service) GetRolePermissions(ctx context.Context, roleID int64) ([]*PermissionResponse, error) {
	permissions, err := s.store.GetRolePermissions(ctx, roleID)
	if err != nil {
		return nil, err
	}

	responses := make([]*PermissionResponse, len(permissions))
	for i, p := range permissions {
		responses[i] = toPermissionResponse(p)
	}

	return responses, nil
}

// toRoleResponse converts an OperationsRole to RoleResponse
func toRoleResponse(r *OperationsRole) *RoleResponse {
	return &RoleResponse{
		ID:          r.ID,
		Name:        r.Name,
		Code:        r.Code,
		Description: r.Description,
		IsActive:    r.IsActive,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

// toPermissionResponse converts an OperationsPermission to PermissionResponse
func toPermissionResponse(p *OperationsPermission) *PermissionResponse {
	return &PermissionResponse{
		ID:          p.ID,
		Name:        p.Name,
		Code:        p.Code,
		Resource:    p.Resource,
		Action:      p.Action,
		Description: p.Description,
	}
}

// Operations Menu operations

// ListOperationsMenus retrieves all operations menus
func (s *Service) ListOperationsMenus(ctx context.Context, req MenuListRequest) ([]*MenuResponse, error) {
	menus, err := s.store.ListOperationsMenus(ctx, req)
	if err != nil {
		return nil, err
	}

	responses := make([]*MenuResponse, len(menus))
	for i, m := range menus {
		responses[i] = toMenuResponse(m)
	}

	return responses, nil
}

// GetOperationsMenuTree retrieves operations menus as a tree
func (s *Service) GetOperationsMenuTree(ctx context.Context) ([]*MenuResponse, error) {
	menus, err := s.store.ListOperationsMenus(ctx, MenuListRequest{})
	if err != nil {
		return nil, err
	}

	return buildMenuTree(menus), nil
}

// GetAdminEffectiveOperationsMenus retrieves the final effective ops menus for an admin.
func (s *Service) GetAdminEffectiveOperationsMenus(ctx context.Context, adminID int64) (*OperationsEffectiveMenuResponse, error) {
	menus, roleCodes, err := s.store.GetAdminEffectiveMenus(ctx, adminID)
	if err != nil {
		return nil, err
	}

	responses := make([]*MenuResponse, 0, len(menus))
	menuCodes := make([]string, 0, len(menus))
	menuPaths := make([]string, 0, len(menus))
	for _, menu := range menus {
		resp := toMenuResponse(menu)
		responses = append(responses, resp)
		if resp.Code != "" {
			menuCodes = append(menuCodes, resp.Code)
		}
		if resp.Path != nil && *resp.Path != "" {
			menuPaths = append(menuPaths, *resp.Path)
		}
	}

	sort.Strings(roleCodes)
	sort.Strings(menuCodes)
	sort.Strings(menuPaths)

	return &OperationsEffectiveMenuResponse{
		AdminID:      adminID,
		RoleCodes:    roleCodes,
		MenuCodes:    menuCodes,
		MenuPaths:    menuPaths,
		Menus:        buildMenuTree(menus),
		IsFullAccess: hasOpsFullAccess(roleCodes),
	}, nil
}

// GetOperationsMenu retrieves an operations menu by ID
func (s *Service) GetOperationsMenu(ctx context.Context, id int64) (*MenuResponse, error) {
	menu, err := s.store.GetOperationsMenuByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return toMenuResponse(menu), nil
}

// CreateOperationsMenu creates a new operations menu
func (s *Service) CreateOperationsMenu(ctx context.Context, req CreateMenuRequest) (*MenuResponse, error) {
	// Validate request
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.Code == "" {
		return nil, fmt.Errorf("code is required")
	}

	menu, err := s.store.CreateOperationsMenu(ctx, req)
	if err != nil {
		return nil, err
	}

	return toMenuResponse(menu), nil
}

// UpdateOperationsMenu updates an operations menu
func (s *Service) UpdateOperationsMenu(ctx context.Context, id int64, req UpdateMenuRequest) (*MenuResponse, error) {
	menu, err := s.store.UpdateOperationsMenu(ctx, id, req)
	if err != nil {
		return nil, err
	}

	return toMenuResponse(menu), nil
}

// DeleteOperationsMenu deletes an operations menu
func (s *Service) DeleteOperationsMenu(ctx context.Context, id int64) error {
	return s.store.DeleteOperationsMenu(ctx, id)
}

// UpdateMenuSort updates menu sort orders
func (s *Service) UpdateMenuSort(ctx context.Context, req MenuSortRequest) error {
	return s.store.UpdateMenuSort(ctx, req.Items)
}

func (s *Service) SyncOperationsMenus(ctx context.Context, req SyncOperationsMenusRequest) error {
	if len(req.Items) == 0 {
		return fmt.Errorf("items is required")
	}
	for _, item := range req.Items {
		if item.Code == "" {
			return fmt.Errorf("menu code is required")
		}
		if item.Name == "" {
			return fmt.Errorf("menu name is required")
		}
		if item.Path == "" {
			return fmt.Errorf("menu path is required")
		}
	}
	return s.store.SyncOperationsMenus(ctx, req.Items)
}

// GetRoleMenus retrieves menus for a role
func (s *Service) GetRoleMenus(ctx context.Context, roleID int64) ([]*MenuResponse, error) {
	menus, err := s.store.GetRoleMenus(ctx, roleID)
	if err != nil {
		return nil, err
	}

	responses := make([]*MenuResponse, len(menus))
	for i, m := range menus {
		responses[i] = toMenuResponse(m)
	}

	return responses, nil
}

// AssignMenusToRole assigns menus to a role
func (s *Service) AssignMenusToRole(ctx context.Context, roleID int64, req AssignMenusRequest) error {
	return s.store.AssignMenusToRole(ctx, roleID, req.MenuIDs)
}

// toMenuResponse converts an OperationsMenu to MenuResponse
func toMenuResponse(m *OperationsMenu) *MenuResponse {
	return &MenuResponse{
		ID:        m.ID,
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

func hasOpsFullAccess(roleCodes []string) bool {
	for _, code := range roleCodes {
		if code == "ops_super_admin" {
			return true
		}
	}
	return false
}

// buildMenuTree builds a tree structure from flat menu list
func buildMenuTree(menus []*OperationsMenu) []*MenuResponse {
	// Create a map for quick lookup
	menuMap := make(map[int64]*MenuResponse)
	var roots []*MenuResponse

	// First pass: create all nodes
	for _, m := range menus {
		node := toMenuResponse(m)
		node.Children = []*MenuResponse{}
		menuMap[m.ID] = node
	}

	// Second pass: build tree
	for _, m := range menus {
		node := menuMap[m.ID]
		if m.ParentID == nil {
			roots = append(roots, node)
		} else {
			if parent, ok := menuMap[*m.ParentID]; ok {
				parent.Children = append(parent.Children, node)
			}
		}
	}

	return roots
}

// Operations Admin operations

// ListOperationsAdmins retrieves a paginated list of operations admins
func (s *Service) ListOperationsAdmins(ctx context.Context, req AdminListRequest) ([]*AdminResponse, int, error) {
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

	return s.store.ListOperationsAdmins(ctx, req)
}

// GetOperationsAdmin retrieves an operations admin by ID
func (s *Service) GetOperationsAdmin(ctx context.Context, id int64) (*AdminResponse, error) {
	return s.store.GetOperationsAdminByID(ctx, id)
}

// CreateOperationsAdmin creates a new operations admin
func (s *Service) CreateOperationsAdmin(ctx context.Context, req CreateAdminRequest) (*AdminResponse, error) {
	// Validate request
	if req.Username == "" {
		return nil, fmt.Errorf("username is required")
	}
	if req.Password == "" {
		return nil, fmt.Errorf("password is required")
	}
	if len(req.Password) < 6 {
		return nil, fmt.Errorf("password must be at least 6 characters")
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	return s.store.CreateOperationsAdmin(ctx, req, string(hashedPassword))
}

// UpdateOperationsAdmin updates an operations admin
func (s *Service) UpdateOperationsAdmin(ctx context.Context, id int64, req UpdateAdminRequest) (*AdminResponse, error) {
	return s.store.UpdateOperationsAdmin(ctx, id, req)
}

// DeleteOperationsAdmin deletes an operations admin
func (s *Service) DeleteOperationsAdmin(ctx context.Context, id int64) error {
	return s.store.DeleteOperationsAdmin(ctx, id)
}

// ResetAdminPassword resets an admin's password
func (s *Service) ResetAdminPassword(ctx context.Context, id int64, req ResetPasswordRequest) error {
	// Validate request
	if req.NewPassword == "" {
		return fmt.Errorf("new password is required")
	}
	if len(req.NewPassword) < 6 {
		return fmt.Errorf("password must be at least 6 characters")
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	return s.store.ResetAdminPassword(ctx, id, string(hashedPassword))
}
