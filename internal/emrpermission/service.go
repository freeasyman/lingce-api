package emrpermission

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/internal/tenancy"
	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/jackc/pgx/v5"
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

func (s *Service) Authorize(ctx context.Context, claims *auth.Claims, tenantID int64, ability string) (*Access, error) {
	if claims == nil {
		return nil, fmt.Errorf("invalid token")
	}
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if claims.UserType == auth.UserTypeAdmin {
		return &Access{UserID: claims.UserID, TenantID: tenantID, Scope: "tenant", Admin: true, Abilities: map[string]struct{}{}}, nil
	}
	if claims.TenantID == nil || *claims.TenantID != tenantID {
		return nil, fmt.Errorf("access denied")
	}

	var departmentID *int64
	var enabled bool
	var scope string
	var role string
	var abilitiesRaw []byte
	err := s.store.pool.QueryRow(ctx, `
		SELECT e.department_id, COALESCE(ep.enabled, false), COALESCE(ep.scope, ''), COALESCE(ep.emr_role_code, ''), COALESCE(ep.abilities_json, '[]'::jsonb)
		FROM employees e
		LEFT JOIN emr_permission_assignments ep
		  ON ep.tenant_id=e.tenant_id AND ep.employee_id=e.id
		WHERE e.id=$1 AND e.tenant_id=$2 AND e.deleted_at IS NULL
	`, claims.UserID, tenantID).Scan(&departmentID, &enabled, &scope, &role, &abilitiesRaw)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("employee not found")
		}
		return nil, fmt.Errorf("load emr access: %w", err)
	}
	if !enabled {
		return nil, fmt.Errorf("emr access is disabled")
	}
	abilities := make([]string, 0)
	if len(abilitiesRaw) > 0 {
		if err := json.Unmarshal(abilitiesRaw, &abilities); err != nil {
			return nil, fmt.Errorf("parse emr access: %w", err)
		}
	}
	abilitySet := make(map[string]struct{}, len(abilities))
	for _, item := range abilities {
		if value := strings.TrimSpace(item); value != "" {
			abilitySet[value] = struct{}{}
		}
	}
	access := &Access{UserID: claims.UserID, TenantID: tenantID, DepartmentID: departmentID, Scope: scope, Role: role, Abilities: abilitySet}
	if !access.Has(ability) {
		return nil, fmt.Errorf("emr permission denied")
	}
	return access, nil
}

func (s *Service) CanAccessRecord(ctx context.Context, access *Access, recordID string) (bool, error) {
	if access == nil {
		return false, nil
	}
	var doctorID int64
	var departmentID *int64
	err := s.store.pool.QueryRow(ctx, `
		SELECT doctor_id, department_id
		FROM emr_records
		WHERE id=$1 AND tenant_id=$2
	`, recordID, access.TenantID).Scan(&doctorID, &departmentID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("load emr record access: %w", err)
	}
	return access.CanRecord(doctorID, departmentID), nil
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
	if req.Enabled {
		hasRead := false
		for _, ability := range normalizeAbilities(req.Abilities) {
			if ability == "record.read" {
				hasRead = true
				break
			}
		}
		if !hasRead {
			return fmt.Errorf("record.read is required when emr is enabled")
		}
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
