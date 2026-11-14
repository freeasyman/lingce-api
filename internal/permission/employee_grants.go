package permission

import (
	"context"
	"encoding/json"
	"fmt"
)

// 给「有该产品」的普通员工补的基础权限：只给本人范围、不含管理能力。
// 产品管理员可在各自产品里再细化，重复执行不会覆盖已有配置。
var (
	emrMemberAbilities = []string{
		"record.read", "record.create", "record.edit", "record.submit", "record.history.read",
	}
	followupMemberAbilities = []string{
		"followup.task.read", "followup.task.execute",
	}
	complianceMemberAbilities = []string{
		"compliance.finding.read", "compliance.rule.read", "compliance.run.read",
	}
	dealMemberAbilities = []string{
		"deal.consultation.read", "deal.task.read", "deal.task.execute", "deal.workbench.read",
	}
	patientOpsMemberAbilities = []string{
		"patient_ops.tasks.read_all",
	}
)

// EnsureEmployeeProductGrants 给指定产品补基础权限（幂等：已有记录不动）。
func EnsureEmployeeProductGrants(ctx context.Context, q QueryExecer, tenantID, employeeID, actorID int64, products []string) error {
	if q == nil || tenantID <= 0 || employeeID <= 0 {
		return nil
	}
	for _, product := range products {
		if err := ensureMemberGrant(ctx, q, product, tenantID, employeeID, actorID); err != nil {
			return err
		}
	}
	return nil
}

func ensureMemberGrant(ctx context.Context, q QueryExecer, product string, tenantID, employeeID, actorID int64) error {
	switch product {
	case ProductEMR:
		if !grantTableExists(ctx, q, "emr_permission_assignments") {
			return nil
		}
		raw, err := json.Marshal(emrMemberAbilities)
		if err != nil {
			return err
		}
		_, err = q.Exec(ctx, `
INSERT INTO emr_permission_assignments (tenant_id, employee_id, enabled, emr_role_code, scope, abilities_json, created_by, updated_by)
VALUES ($1, $2, TRUE, 'doctor', 'self', $3::jsonb, $4, $4)
ON CONFLICT (tenant_id, employee_id) DO NOTHING`, tenantID, employeeID, string(raw), actorID)
		if err != nil {
			return fmt.Errorf("ensure emr member grant: %w", err)
		}
	case ProductFollowup:
		if !grantTableExists(ctx, q, "followup_permission_assignments") {
			return nil
		}
		raw, err := json.Marshal(followupMemberAbilities)
		if err != nil {
			return err
		}
		_, err = q.Exec(ctx, `
INSERT INTO followup_permission_assignments (tenant_id, employee_id, enabled, role_code, abilities_json, created_by, updated_by)
VALUES ($1, $2, TRUE, 'followup_staff', $3::jsonb, $4, $4)
ON CONFLICT (tenant_id, employee_id) DO NOTHING`, tenantID, employeeID, string(raw), actorID)
		if err != nil {
			return fmt.Errorf("ensure followup member grant: %w", err)
		}
	case ProductCompliance:
		if !grantTableExists(ctx, q, "compliance_permission_assignments") {
			return nil
		}
		raw, err := json.Marshal(complianceMemberAbilities)
		if err != nil {
			return err
		}
		_, err = q.Exec(ctx, `
INSERT INTO compliance_permission_assignments (tenant_id, employee_id, enabled, role_code, abilities_json, created_by, updated_by)
VALUES ($1, $2, TRUE, 'compliance_staff', $3::jsonb, $4, $4)
ON CONFLICT (tenant_id, employee_id) DO NOTHING`, tenantID, employeeID, string(raw), actorID)
		if err != nil {
			return fmt.Errorf("ensure compliance member grant: %w", err)
		}
	case ProductDealExpert:
		if !grantTableExists(ctx, q, "deal_expert_permission_assignments") {
			return nil
		}
		raw, err := json.Marshal(dealMemberAbilities)
		if err != nil {
			return err
		}
		_, err = q.Exec(ctx, `
INSERT INTO deal_expert_permission_assignments (tenant_id, employee_id, enabled, role_code, abilities_json, data_scope, created_by, updated_by)
VALUES ($1, $2, TRUE, 'deal_staff', $3::jsonb, 'self', $4, $4)
ON CONFLICT (tenant_id, employee_id) DO NOTHING`, tenantID, employeeID, string(raw), actorID)
		if err != nil {
			return fmt.Errorf("ensure deal member grant: %w", err)
		}
	case ProductPatientOps:
		if !grantTableExists(ctx, q, "patient_ops_permission_assignments") {
			return nil
		}
		raw, err := json.Marshal(patientOpsMemberAbilities)
		if err != nil {
			return err
		}
		_, err = q.Exec(ctx, `
INSERT INTO patient_ops_permission_assignments (tenant_id, employee_id, enabled, role_code, abilities_json, created_by, updated_by)
VALUES ($1, $2, TRUE, 'patient_ops_staff', $3::jsonb, $4, $4)
ON CONFLICT (tenant_id, employee_id) DO NOTHING`, tenantID, employeeID, string(raw), actorID)
		if err != nil {
			return fmt.Errorf("ensure patient ops member grant: %w", err)
		}
	}
	return nil
}

// grantTableExists 兼容不同环境里产品权限表可能尚未建的情况。
func grantTableExists(ctx context.Context, q QueryExecer, table string) bool {
	var exists bool
	if err := q.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+table).Scan(&exists); err != nil {
		return false
	}
	return exists
}
