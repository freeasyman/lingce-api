package permission_test

// 身份判定与默认授权的 PostgreSQL 集成测试。
//
// 运行前置：本地测试库已执行迁移；设置
//
//	TEST_DATABASE_URL='postgresql://yiliiang@127.0.0.1:5432/lingce_billing_test?sslmode=disable' \
//	  go test ./internal/permission/ -count=1 -v
//
// 未设置 URL 时全部 Skip，不影响普通 go test ./...。

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/freeasyman/lingce-api/internal/permission"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("BILLING_TEST_DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL for permission PostgreSQL integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type testFixture struct {
	TenantID int64
	Code     string
}

func newFixture(t *testing.T, pool *pgxpool.Pool) testFixture {
	t.Helper()
	ctx := context.Background()
	code := fmt.Sprintf("perm-test-%d", time.Now().UnixNano())
	var tenantID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, code, is_active) VALUES ($1, $2, 1) RETURNING id
	`, "权限测试机构", code).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	f := testFixture{TenantID: tenantID, Code: code}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM deal_expert_permission_assignments WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM patient_ops_permission_assignments WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM followup_permission_assignments WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM compliance_permission_assignments WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM emr_permission_assignments WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM institution_employee_roles WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM institution_roles WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM employees WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM tenants WHERE id=$1`, tenantID)
	})
	return f
}

var testPhoneSeq atomic.Int64

func (f testFixture) addEmployee(t *testing.T, pool *pgxpool.Pool, name, role, roleCode string, deleted bool, primary bool) int64 {
	t.Helper()
	ctx := context.Background()
	phone := fmt.Sprintf("199%08d", testPhoneSeq.Add(1)%100_000_000)
	var employeeID int64
	var deletedAt any
	if deleted {
		deletedAt = time.Now()
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO employees (tenant_id, name, full_name, phone, password_hash, role, is_primary, is_active, deleted_at)
		VALUES ($1, $2::varchar, $2::text, $3, 'x', $4, $5, 1, $6)
		RETURNING id
	`, f.TenantID, name, phone, nullIfEmpty(role), primary, deletedAt).Scan(&employeeID); err != nil {
		t.Fatalf("create employee %s: %v", name, err)
	}
	if roleCode != "" {
		var roleID int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO institution_roles (tenant_id, name, code, is_active)
			VALUES ($1, $2, $2, true)
			ON CONFLICT (tenant_id, code) WHERE deleted_at IS NULL DO UPDATE SET updated_at = NOW()
			RETURNING id
		`, f.TenantID, roleCode).Scan(&roleID); err != nil {
			t.Fatalf("create institution role %s: %v", roleCode, err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO institution_employee_roles (employee_id, role_id, tenant_id, role_code)
			VALUES ($1, $2, $3, $4)
		`, employeeID, roleID, f.TenantID, roleCode); err != nil {
			t.Fatalf("assign institution role %s: %v", roleCode, err)
		}
	}
	return employeeID
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func TestLoadInstitutionIdentity(t *testing.T) {
	pool := testPool(t)
	f := newFixture(t, pool)
	ctx := context.Background()

	roleAdmin := f.addEmployee(t, pool, "角色表管理员", "", "admin", false, false)
	employeeRoleAdmin := f.addEmployee(t, pool, "employees.role 管理员", "admin", "", false, false)
	doctor := f.addEmployee(t, pool, "医生", "doctor", "doctor", false, false)
	consultant := f.addEmployee(t, pool, "咨询师", "consultant", "consultant", false, false)
	deletedAdmin := f.addEmployee(t, pool, "已删除管理员", "", "admin", true, false)

	cases := []struct {
		name       string
		employeeID int64
		wantAdmin  bool
		wantNil    bool
	}{
		{name: "institution role admin", employeeID: roleAdmin, wantAdmin: true},
		{name: "employees.role admin", employeeID: employeeRoleAdmin, wantAdmin: true},
		{name: "doctor", employeeID: doctor, wantAdmin: false},
		{name: "consultant", employeeID: consultant, wantAdmin: false},
		{name: "deleted admin", employeeID: deletedAdmin, wantNil: true},
		{name: "unknown employee", employeeID: 99999999, wantNil: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			identity, err := permission.LoadInstitutionIdentity(ctx, pool, f.TenantID, tc.employeeID)
			if err != nil {
				t.Fatalf("load identity: %v", err)
			}
			if tc.wantNil {
				if identity != nil {
					t.Fatalf("identity = %#v, want nil", identity)
				}
				return
			}
			if identity == nil {
				t.Fatal("identity = nil")
			}
			if identity.Admin != tc.wantAdmin {
				t.Fatalf("admin = %v, want %v", identity.Admin, tc.wantAdmin)
			}
		})
	}
}

func TestIsAdminRoleCodeExactMatch(t *testing.T) {
	for _, value := range []string{"admin", "Admin", " ADMIN "} {
		if !permission.IsAdminRoleCode(value) {
			t.Fatalf("IsAdminRoleCode(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"", "管理员", "doctor", "consultant", "nurse", "institution_admin", "admin_x", "super_admin"} {
		if permission.IsAdminRoleCode(value) {
			t.Fatalf("IsAdminRoleCode(%q) = true, want false", value)
		}
	}
}

func TestEnsureInstitutionAdminGrantsIdempotent(t *testing.T) {
	pool := testPool(t)
	f := newFixture(t, pool)
	ctx := context.Background()
	employeeID := f.addEmployee(t, pool, "主管理员", "", "admin", false, true)

	dry, err := permission.EnsureInstitutionAdminGrants(ctx, pool, f.TenantID, employeeID, employeeID, true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if got := countStatus(dry, permission.GrantStatusWouldGrant); got != 6 {
		t.Fatalf("dry run would_grant = %d, want 6 (%#v)", got, dry)
	}
	if got := countStatus(dry, permission.GrantStatusSkipped); got != 0 {
		t.Fatalf("dry run skipped = %d, want 0 (%#v)", got, dry)
	}

	first, err := permission.EnsureInstitutionAdminGrants(ctx, pool, f.TenantID, employeeID, employeeID, false)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := countStatus(first, permission.GrantStatusGranted); got != 6 {
		t.Fatalf("apply granted = %d, want 6 (%#v)", got, first)
	}

	second, err := permission.EnsureInstitutionAdminGrants(ctx, pool, f.TenantID, employeeID, employeeID, false)
	if err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	if got := countStatus(second, permission.GrantStatusExists); got != 6 {
		t.Fatalf("re-apply exists = %d, want 6 (%#v)", got, second)
	}

	for _, table := range []string{
		"emr_permission_assignments",
		"compliance_permission_assignments",
		"followup_permission_assignments",
		"patient_ops_permission_assignments",
		"deal_expert_permission_assignments",
		"content_permission_assignments",
	} {
		var count int
		if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE tenant_id=$1 AND employee_id=$2`, table), f.TenantID, employeeID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("%s rows = %d, want 1", table, count)
		}
	}
}

func TestEnsureInstitutionAdminGrantsDoesNotOverwriteManualConfig(t *testing.T) {
	pool := testPool(t)
	f := newFixture(t, pool)
	ctx := context.Background()
	employeeID := f.addEmployee(t, pool, "管理员", "", "admin", false, true)

	// 人工把合规权限停用并降为 staff；默认发放不能覆盖。
	if _, err := pool.Exec(ctx, `
		INSERT INTO compliance_permission_assignments
			(tenant_id, employee_id, enabled, role_code, abilities_json, created_by, updated_by)
		VALUES ($1, $2, FALSE, 'compliance_staff', '["compliance.finding.read"]'::jsonb, 0, 0)
	`, f.TenantID, employeeID); err != nil {
		t.Fatalf("seed manual compliance permission: %v", err)
	}
	// 人工把听写病历范围设为本科室；默认发放同样不能覆盖。
	if _, err := pool.Exec(ctx, `
		INSERT INTO emr_permission_assignments
			(tenant_id, employee_id, enabled, emr_role_code, scope, abilities_json, created_by, updated_by)
		VALUES ($1, $2, TRUE, 'doctor', 'department', '["record.read","record.edit"]'::jsonb, 0, 0)
	`, f.TenantID, employeeID); err != nil {
		t.Fatalf("seed manual emr permission: %v", err)
	}

	results, err := permission.EnsureInstitutionAdminGrants(ctx, pool, f.TenantID, employeeID, employeeID, false)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	byProduct := map[string]permission.GrantStatus{}
	for _, item := range results {
		byProduct[item.Product] = item.Status
	}
	if byProduct[permission.ProductCompliance] != permission.GrantStatusExists {
		t.Fatalf("compliance status = %s, want exists", byProduct[permission.ProductCompliance])
	}
	if byProduct[permission.ProductEMR] != permission.GrantStatusExists {
		t.Fatalf("emr status = %s, want exists", byProduct[permission.ProductEMR])
	}

	var complianceEnabled bool
	var complianceRole string
	var complianceAbilities string
	if err := pool.QueryRow(ctx, `
		SELECT enabled, role_code, abilities_json::text
		FROM compliance_permission_assignments WHERE tenant_id=$1 AND employee_id=$2
	`, f.TenantID, employeeID).Scan(&complianceEnabled, &complianceRole, &complianceAbilities); err != nil {
		t.Fatalf("read compliance row: %v", err)
	}
	if complianceEnabled || complianceRole != "compliance_staff" || complianceAbilities != `["compliance.finding.read"]` {
		t.Fatalf("manual compliance config overwritten: enabled=%v role=%s abilities=%s", complianceEnabled, complianceRole, complianceAbilities)
	}

	var emrRole, emrScope string
	var emrAbilities string
	if err := pool.QueryRow(ctx, `
		SELECT emr_role_code, scope, abilities_json::text
		FROM emr_permission_assignments WHERE tenant_id=$1 AND employee_id=$2
	`, f.TenantID, employeeID).Scan(&emrRole, &emrScope, &emrAbilities); err != nil {
		t.Fatalf("read emr row: %v", err)
	}
	if emrRole != "doctor" || emrScope != "department" || emrAbilities != `["record.read", "record.edit"]` {
		t.Fatalf("manual emr config overwritten: role=%s scope=%s abilities=%s", emrRole, emrScope, emrAbilities)
	}
}

func TestFindInstitutionPrimaryAdmin(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	t.Run("explicit primary", func(t *testing.T) {
		f := newFixture(t, pool)
		primary := f.addEmployee(t, pool, "主管理员", "", "admin", false, true)
		f.addEmployee(t, pool, "普通管理员", "", "admin", false, false)
		admin, err := permission.FindInstitutionPrimaryAdmin(ctx, pool, f.TenantID)
		if err != nil {
			t.Fatalf("find primary: %v", err)
		}
		if admin == nil || admin.EmployeeID != primary || admin.Source != "is_primary" {
			t.Fatalf("admin = %#v, want employee %d from is_primary", admin, primary)
		}
	})

	t.Run("fallback admin role", func(t *testing.T) {
		f := newFixture(t, pool)
		adminID := f.addEmployee(t, pool, "角色管理员", "", "admin", false, false)
		admin, err := permission.FindInstitutionPrimaryAdmin(ctx, pool, f.TenantID)
		if err != nil {
			t.Fatalf("find fallback: %v", err)
		}
		if admin == nil || admin.EmployeeID != adminID || admin.Source != "admin_role" {
			t.Fatalf("admin = %#v, want employee %d from admin_role", admin, adminID)
		}
	})

	t.Run("no admin", func(t *testing.T) {
		f := newFixture(t, pool)
		f.addEmployee(t, pool, "医生", "doctor", "doctor", false, false)
		admin, err := permission.FindInstitutionPrimaryAdmin(ctx, pool, f.TenantID)
		if err != nil {
			t.Fatalf("find admin: %v", err)
		}
		if admin != nil {
			t.Fatalf("admin = %#v, want nil", admin)
		}
	})
}

func countStatus(results []permission.GrantResult, status permission.GrantStatus) int {
	count := 0
	for _, item := range results {
		if item.Status == status {
			count++
		}
	}
	return count
}
