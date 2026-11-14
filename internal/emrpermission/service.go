package emrpermission

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/internal/permission"
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
	items, err := s.store.ListEmployeePermissions(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	// 机构管理员即使在 emr_permission_assignments 里没有行，也按管理员口径展示。
	adminIDs, err := permission.ListInstitutionAdminIDs(ctx, s.store.pool, tenantID)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if _, ok := adminIDs[item.EmployeeID]; ok {
			item.EmrPermission = adminAssignmentView(tenantID, item.EmployeeID, item.EmrPermission)
		}
	}
	return items, nil
}

func (s *Service) GetAssignment(ctx context.Context, tenantID, employeeID int64) (*AssignmentResponse, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("employee_id", employeeID); err != nil {
		return nil, err
	}
	// 机构管理员始终按管理员权限返回，不再要求单独写 emr_permission_assignments。
	// 已有行也以管理员身份为准，避免「运行时放行、页面显示无权限」。
	if admin, err := permission.IsInstitutionAdmin(ctx, s.store.pool, tenantID, employeeID); err != nil {
		return nil, err
	} else if admin {
		// 有现存行时保留 id/时间/操作人等元数据，只把生效值收敛为管理员；
		// 没有行时返回虚拟管理员权限。
		assignment, err := s.store.GetAssignmentByEmployeeID(ctx, tenantID, employeeID)
		if err != nil {
			return nil, err
		}
		return toAssignmentResponse(adminAssignmentView(tenantID, employeeID, assignment)), nil
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

// adminAssignmentView 把机构管理员的生效权限收敛为「启用 + emr_admin + 全机构」。
// 有现存行时复用其 id/created_by/created_at 等元数据，避免权限页显示成虚构行。
func adminAssignmentView(tenantID, employeeID int64, existing *Assignment) *Assignment {
	if existing == nil {
		return DefaultAdminAssignment(tenantID, employeeID)
	}
	existing.TenantID = tenantID
	existing.EmployeeID = employeeID
	existing.Enabled = true
	existing.EmrRoleCode = "emr_admin"
	existing.Scope = "tenant"
	existing.Abilities = append([]string{}, AllAbilities...)
	return existing
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

	// 机构管理员（employees.role 或机构角色表的 admin）默认就是听写病历管理员。
	identity, err := permission.LoadInstitutionIdentity(ctx, s.store.pool, tenantID, claims.UserID)
	if err != nil {
		return nil, fmt.Errorf("load emr access: %w", err)
	}
	if identity == nil {
		return nil, fmt.Errorf("employee not found")
	}
	if identity.Admin {
		return newAdminAccess(claims.UserID, tenantID), nil
	}

	var departmentIDs []int64
	var enabled bool
	var scope string
	var role string
	var abilitiesRaw []byte
	err = s.store.pool.QueryRow(ctx, `
		SELECT COALESCE((
			SELECT array_agg(ed.department_id ORDER BY ed.department_id)
			FROM employee_departments ed
			WHERE ed.employee_id=e.id AND ed.tenant_id=e.tenant_id
		), '{}'::bigint[]),
		COALESCE(ep.enabled, false), COALESCE(ep.scope, ''), COALESCE(ep.emr_role_code, ''), COALESCE(ep.abilities_json, '[]'::jsonb)
		FROM employees e
		LEFT JOIN emr_permission_assignments ep
		  ON ep.tenant_id=e.tenant_id AND ep.employee_id=e.id
		WHERE e.id=$1 AND e.tenant_id=$2 AND e.deleted_at IS NULL
	`, claims.UserID, tenantID).Scan(&departmentIDs, &enabled, &scope, &role, &abilitiesRaw)
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
	access := &Access{UserID: claims.UserID, TenantID: tenantID, DepartmentIDs: departmentIDs, Scope: scope, Role: role, Abilities: abilitySet}
	if !access.Has(ability) {
		return nil, fmt.Errorf("emr permission denied")
	}
	return access, nil
}

func newAdminAccess(userID, tenantID int64) *Access {
	abilities := make(map[string]struct{}, len(AllAbilities))
	for _, ability := range AllAbilities {
		abilities[ability] = struct{}{}
	}
	return &Access{
		UserID:    userID,
		TenantID:  tenantID,
		Scope:     "tenant",
		Role:      "emr_admin",
		Admin:     true,
		Abilities: abilities,
	}
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

func (s *Service) UpsertAssignment(ctx context.Context, tenantID, employeeID, actorID int64, req UpsertAssignmentRequest, snapshot ...func(*AssignmentResponse, *AssignmentResponse)) (*AssignmentResponse, error) {
	if err := tenancy.RequirePositiveID("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := tenancy.RequirePositiveID("employee_id", employeeID); err != nil {
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

	if len(snapshot) > 0 {
		snapshot[0](toAssignmentResponse(existing), nil)
	}
	if err := s.validateUpsertRequest(req); err != nil {
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
	if len(snapshot) > 0 {
		snapshot[0](toAssignmentResponse(existing), toAssignmentResponse(saved))
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
