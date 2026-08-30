package emrpermission

import (
	"context"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/internal/tenancy"
)

type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store}
}

func (s *Service) ListEmployeePermissions(ctx context.Context, tenantID int64) ([]*EmployeePermissionRow, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	return s.store.ListEmployeePermissions(ctx, tenantID)
}

func (s *Service) GetAssignment(ctx context.Context, tenantID, employeeID int64) (*AssignmentResponse, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("employee_id", employeeID); err != nil {
		return nil, err
	}
	assignment, err := s.store.GetAssignmentByEmployeeID(ctx, tenantID, employeeID)
	if err != nil {
		return nil, err
	}
	if assignment == nil {
		return nil, nil
	}
	return toAssignmentResponse(assignment), nil
}

func (s *Service) UpsertAssignment(ctx context.Context, tenantID, employeeID, actorID int64, req UpsertAssignmentRequest) (*AssignmentResponse, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("employee_id", employeeID); err != nil {
		return nil, err
	}
	if err := s.validateUpsertRequest(req); err != nil {
		return nil, err
	}
	exists, err := s.store.EmployeeExistsInTenant(ctx, tenantID, employeeID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("employee not found")
	}

	actor := actorID
	existing, err := s.store.GetAssignmentByEmployeeID(ctx, tenantID, employeeID)
	if err != nil {
		return nil, err
	}

	assignment := &Assignment{
		TenantID:    tenantID,
		EmployeeID:  employeeID,
		Enabled:     req.Enabled,
		EmrRoleCode: strings.TrimSpace(req.EmrRoleCode),
		Scope:       strings.TrimSpace(req.Scope),
		Abilities:   normalizeAbilities(req.Abilities),
		UpdatedBy:   &actor,
	}
	if existing == nil {
		assignment.CreatedBy = &actor
	} else {
		assignment.CreatedBy = existing.CreatedBy
	}

	saved, err := s.store.UpsertAssignment(ctx, assignment)
	if err != nil {
		return nil, err
	}
	return toAssignmentResponse(saved), nil
}

func (s *Service) validateUpsertRequest(req UpsertAssignmentRequest) error {
	role := strings.TrimSpace(req.EmrRoleCode)
	scope := strings.TrimSpace(req.Scope)
	if role == "" {
		return fmt.Errorf("emr_role_code is required")
	}
	if scope == "" {
		return fmt.Errorf("scope is required")
	}
	switch role {
	case "emr_admin", "doctor", "archivist", "readonly":
	default:
		return fmt.Errorf("invalid emr_role_code")
	}
	switch scope {
	case "self", "department", "tenant":
	default:
		return fmt.Errorf("invalid scope")
	}
	if req.Enabled && len(normalizeAbilities(req.Abilities)) == 0 {
		return fmt.Errorf("abilities is required when emr is enabled")
	}
	return nil
}

func toAssignmentResponse(item *Assignment) *AssignmentResponse {
	if item == nil {
		return nil
	}
	return &AssignmentResponse{
		EmployeeID:  item.EmployeeID,
		TenantID:    item.TenantID,
		Enabled:     item.Enabled,
		EmrRoleCode: item.EmrRoleCode,
		Scope:       item.Scope,
		Abilities:   item.Abilities,
		UpdatedAt:   item.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedBy:   item.UpdatedBy,
	}
}
