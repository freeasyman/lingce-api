package dealexpertpermission

// 权限判定集成测试：只使用临时 schema，验证「机构管理员」同时认
// employees.role 和 institution_employee_roles.role_code 两个来源，
// 且 doctor / consultant 等其它角色不会被误判为管理员。
// 设置 DEAL_EXPERT_PERMISSION_TEST_DATABASE_URL（或 TEST_DATABASE_URL）后运行。
//
// 署名：Codex
// 时间：2026-11-12

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testPermissionService(t *testing.T) *Service {
	t.Helper()
	url := os.Getenv("DEAL_EXPERT_PERMISSION_TEST_DATABASE_URL")
	if url == "" {
		url = os.Getenv("TEST_DATABASE_URL")
	}
	if url == "" {
		t.Skip("set DEAL_EXPERT_PERMISSION_TEST_DATABASE_URL for PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("dealexpertpermission_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); admin.Close() })

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	_, err = pool.Exec(ctx, `
		CREATE TABLE employees (
			id bigint PRIMARY KEY,
			tenant_id bigint,
			role text,
			deleted_at timestamp
		);
		CREATE TABLE institution_employee_roles (
			id bigserial PRIMARY KEY,
			tenant_id bigint,
			employee_id bigint,
			role_code text
		);
		CREATE TABLE deal_expert_permission_assignments (
			id bigserial PRIMARY KEY,
			tenant_id bigint,
			employee_id bigint,
			enabled boolean,
			role_code text,
			abilities_json jsonb,
			data_scope text,
			visible_employee_ids bigint[],
			created_by bigint,
			updated_by bigint,
			created_at timestamp,
			updated_at timestamp
		);
		INSERT INTO employees (id, tenant_id, role, deleted_at) VALUES
			(650, 5001, NULL, NULL),
			(651, 5001, NULL, NULL),
			(652, 5001, 'admin', NULL),
			(653, 5001, NULL, NULL);
		INSERT INTO institution_employee_roles (tenant_id, employee_id, role_code) VALUES
			(5001, 650, 'admin'),
			(5001, 651, 'doctor'),
			(5001, 653, 'consultant');
	`)
	if err != nil {
		t.Fatal(err)
	}
	return NewService(pool)
}

func TestAdminRecognizedFromInstitutionRoles(t *testing.T) {
	service := testPermissionService(t)
	ctx := context.Background()
	tenantID := int64(5001)
	employeeID := int64(650)

	// 核心回归：employees.role 为空，只有 institution_employee_roles=admin。
	admin, err := service.IsInstitutionAdmin(ctx, tenantID, employeeID)
	if err != nil {
		t.Fatal(err)
	}
	if !admin {
		t.Fatal("employee with institution role admin must be recognized as admin")
	}
	visibility, err := service.VisibilityFor(ctx, tenantID, employeeID)
	if err != nil {
		t.Fatal(err)
	}
	if visibility == nil || !visibility.All || visibility.Scope != DataScopeTenant {
		t.Fatalf("institution admin visibility = %#v, want tenant/all", visibility)
	}

	// 权限管理入口同样按管理员处理，不能只在数据范围里修。
	claims := &auth.Claims{UserID: employeeID, UserType: auth.UserTypeEmployee, TenantID: &tenantID}
	access, err := service.AccessFor(ctx, claims, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if !access.Admin || !access.CanManagePermissions() || access.DataScope != DataScopeTenant {
		t.Fatalf("institution admin access = %+v, want admin/tenant", access)
	}
}

func TestOtherInstitutionRolesAreNotAdmin(t *testing.T) {
	service := testPermissionService(t)
	ctx := context.Background()
	tenantID := int64(5001)

	for _, employeeID := range []int64{651, 653} {
		admin, err := service.IsInstitutionAdmin(ctx, tenantID, employeeID)
		if err != nil {
			t.Fatal(err)
		}
		if admin {
			t.Fatalf("employee %d with doctor/consultant role must not be admin", employeeID)
		}
		visibility, err := service.VisibilityFor(ctx, tenantID, employeeID)
		if err != nil {
			t.Fatal(err)
		}
		if visibility == nil || visibility.All || visibility.Scope != DataScopeSelf {
			t.Fatalf("employee %d visibility = %#v, want self", employeeID, visibility)
		}
		claims := &auth.Claims{UserID: employeeID, UserType: auth.UserTypeEmployee, TenantID: &tenantID}
		access, err := service.AccessFor(ctx, claims, tenantID)
		if err != nil {
			t.Fatal(err)
		}
		if access.Admin || access.CanManagePermissions() {
			t.Fatalf("employee %d access = %+v, doctor/consultant must not manage permissions", employeeID, access)
		}
	}
}

func TestEmployeeRoleAdminStillRecognized(t *testing.T) {
	service := testPermissionService(t)
	ctx := context.Background()
	admin, err := service.IsInstitutionAdmin(ctx, 5001, 652)
	if err != nil {
		t.Fatal(err)
	}
	if !admin {
		t.Fatal("employees.role=admin must stay recognized")
	}
	visibility, err := service.VisibilityFor(ctx, 5001, 652)
	if err != nil {
		t.Fatal(err)
	}
	if visibility == nil || !visibility.All {
		t.Fatalf("employees.role=admin visibility = %#v, want tenant/all", visibility)
	}
}
