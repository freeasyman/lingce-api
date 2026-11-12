package emrpermission

// 机构管理员运行时兜底集成测试：只使用临时 schema，验证
//「机构管理员没有 emr_permission_assignments 行也能进产品，普通医生仍然受限」。
// 设置 EMR_PERMISSION_TEST_DATABASE_URL（或 TEST_DATABASE_URL）后运行。

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/freeasyman/lingce-api/pkg/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newAdminFallbackService(t *testing.T) *Service {
	t.Helper()
	url := os.Getenv("EMR_PERMISSION_TEST_DATABASE_URL")
	if url == "" {
		url = os.Getenv("TEST_DATABASE_URL")
	}
	if url == "" {
		t.Skip("set EMR_PERMISSION_TEST_DATABASE_URL for PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("emrpermission_test_%d", time.Now().UnixNano())
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
			department_id bigint,
			role text,
			deleted_at timestamp
		);
		CREATE TABLE institution_employee_roles (
			id bigserial PRIMARY KEY,
			tenant_id bigint,
			employee_id bigint,
			role_code text
		);
		CREATE TABLE emr_permission_assignments (
			id bigserial PRIMARY KEY,
			tenant_id bigint,
			employee_id bigint,
			enabled boolean,
			emr_role_code text,
			scope text,
			abilities_json jsonb,
			created_by bigint,
			updated_by bigint,
			created_at timestamp,
			updated_at timestamp
		);
		INSERT INTO employees (id, tenant_id, department_id, role, deleted_at) VALUES
			(9110, 5001, 10, NULL, NULL),
			(9107, 5001, 10, 'doctor', NULL),
			(9111, 5001, 20, NULL, NULL);
		INSERT INTO institution_employee_roles (tenant_id, employee_id, role_code) VALUES
			(5001, 9110, 'admin'),
			(5001, 9107, 'doctor');
		INSERT INTO emr_permission_assignments
			(tenant_id, employee_id, enabled, emr_role_code, scope, abilities_json)
		VALUES (5001, 9111, TRUE, 'doctor', 'self', '["record.read"]'::jsonb);
	`)
	if err != nil {
		t.Fatal(err)
	}
	return NewService(NewStore(pool))
}

func TestInstitutionAdminGetsEmrAccessWithoutAssignment(t *testing.T) {
	service := newAdminFallbackService(t)
	ctx := context.Background()
	tenantID := int64(5001)
	claims := &auth.Claims{UserID: 9110, UserType: auth.UserTypeEmployee, TenantID: &tenantID}

	access, err := service.Authorize(ctx, claims, tenantID, "quality.manage")
	if err != nil {
		t.Fatalf("authorize admin: %v", err)
	}
	if !access.Admin || !access.Has("record.read") || !access.CanManagePermissions() {
		t.Fatalf("admin access = %+v, want tenant admin with all abilities", access)
	}
	if !access.CanRecord(9107, nil) {
		t.Fatal("institution admin must be able to access records across the tenant")
	}

	assignment, err := service.GetAssignment(ctx, tenantID, 9110)
	if err != nil {
		t.Fatalf("get admin assignment: %v", err)
	}
	if assignment == nil {
		t.Fatal("admin assignment = nil, want synthetic emr_admin assignment")
	}
	if !assignment.Enabled || assignment.EmrRoleCode != "emr_admin" || assignment.Scope != "tenant" {
		t.Fatalf("admin assignment = %+v, want enabled/emr_admin/tenant", assignment)
	}
	if len(assignment.Abilities) != len(AllAbilities) {
		t.Fatalf("admin abilities = %v, want %d entries", assignment.Abilities, len(AllAbilities))
	}
}

func TestNonAdminStillRestrictedWithoutAssignment(t *testing.T) {
	service := newAdminFallbackService(t)
	ctx := context.Background()
	tenantID := int64(5001)
	claims := &auth.Claims{UserID: 9107, UserType: auth.UserTypeEmployee, TenantID: &tenantID}

	if _, err := service.Authorize(ctx, claims, tenantID, "record.read"); err == nil {
		t.Fatal("doctor without emr assignment must be denied")
	}
	assignment, err := service.GetAssignment(ctx, tenantID, 9107)
	if err != nil {
		t.Fatalf("get doctor assignment: %v", err)
	}
	if assignment != nil {
		t.Fatalf("doctor assignment = %+v, want nil", assignment)
	}
}

func TestExplicitNonAdminAssignmentStillUsed(t *testing.T) {
	service := newAdminFallbackService(t)
	ctx := context.Background()
	tenantID := int64(5001)
	claims := &auth.Claims{UserID: 9111, UserType: auth.UserTypeEmployee, TenantID: &tenantID}

	access, err := service.Authorize(ctx, claims, tenantID, "record.read")
	if err != nil {
		t.Fatalf("authorize explicit doctor: %v", err)
	}
	if access.Admin || !access.Has("record.read") || access.Has("quality.manage") {
		t.Fatalf("explicit doctor access = %+v, want record.read only", access)
	}
}
