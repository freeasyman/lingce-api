package permission_test

// 机构建档默认授权的集成测试：真实调用 tenant.Store.CreateTenant，
// 验证「新建机构的主管理员在五个有权限表的产品上默认就是管理员」。
// 内容获客没有独立人员权限表，不在数据层发放（运行时也不需要单独判定）。

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/freeasyman/lingce-api/internal/tenant"
)

func TestCreateTenantGrantsProductAdminToDefaultAdmin(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	code := fmt.Sprintf("perm-create-%d", time.Now().UnixNano())
	phone := fmt.Sprintf("198%08d", time.Now().UnixNano()%100_000_000)
	name := "建档默认授权测试机构"

	created, err := tenant.NewStore(pool).CreateTenant(ctx, tenant.CreateTenantRequest{
		Name:         name,
		Code:         code,
		ContactName:  stringPtr("默认管理员"),
		ContactPhone: &phone,
	})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM deal_expert_permission_assignments WHERE tenant_id=$1`, created.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM patient_ops_permission_assignments WHERE tenant_id=$1`, created.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM followup_permission_assignments WHERE tenant_id=$1`, created.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM compliance_permission_assignments WHERE tenant_id=$1`, created.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM emr_permission_assignments WHERE tenant_id=$1`, created.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM institution_role_menus WHERE tenant_id=$1`, created.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM institution_employee_roles WHERE tenant_id=$1`, created.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM institution_department_roles WHERE tenant_id=$1`, created.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM employees WHERE tenant_id=$1`, created.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM departments WHERE tenant_id=$1`, created.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM institution_roles WHERE tenant_id=$1`, created.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM tenants WHERE id=$1`, created.ID)
	})

	var adminID int64
	if err := pool.QueryRow(ctx, `
		SELECT id FROM employees
		WHERE tenant_id=$1 AND phone=$2 AND deleted_at IS NULL
	`, created.ID, phone).Scan(&adminID); err != nil {
		t.Fatalf("load default admin: %v", err)
	}

	for _, tc := range []struct {
		table      string
		roleColumn string
		roleCode   string
	}{
		{table: "emr_permission_assignments", roleColumn: "emr_role_code", roleCode: "emr_admin"},
		{table: "compliance_permission_assignments", roleColumn: "role_code", roleCode: "compliance_admin"},
		{table: "followup_permission_assignments", roleColumn: "role_code", roleCode: "followup_admin"},
		{table: "patient_ops_permission_assignments", roleColumn: "role_code", roleCode: "patient_ops_admin"},
		{table: "deal_expert_permission_assignments", roleColumn: "role_code", roleCode: "deal_admin"},
	} {
		var enabled bool
		var roleCode string
		var abilities int
		query := fmt.Sprintf(`
			SELECT enabled, %s, jsonb_array_length(abilities_json)
			FROM %s WHERE tenant_id=$1 AND employee_id=$2
		`, tc.roleColumn, tc.table)
		if err := pool.QueryRow(ctx, query, created.ID, adminID).Scan(&enabled, &roleCode, &abilities); err != nil {
			t.Fatalf("%s: default admin grant missing: %v", tc.table, err)
		}
		if !enabled || roleCode != tc.roleCode || abilities == 0 {
			t.Fatalf("%s: enabled=%v role=%s abilities=%d, want enabled/%s/non-empty", tc.table, enabled, roleCode, abilities, tc.roleCode)
		}
	}
}

func stringPtr(value string) *string { return &value }
