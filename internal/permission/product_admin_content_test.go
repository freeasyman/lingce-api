package permission_test

// 获客先锋（content）产品管理员：没有独立权限表，走通用指派表
// content_permission_assignments。验证「指定 → 列表可见 → 撤销 → 列表消失」。
//
// 运行前置与 permission_test.go 相同：设置 TEST_DATABASE_URL（已执行迁移）。

import (
	"context"
	"testing"

	"github.com/freeasyman/lingce-api/internal/permission"
)

func TestContentProductAdminRoundTrip(t *testing.T) {
	pool := testPool(t)
	f := newFixture(t, pool)
	ctx := context.Background()

	adminID := f.addEmployee(t, pool, "内容管理员", "admin", "admin", false, true)

	result, err := permission.SetProductAdmin(ctx, pool, permission.ProductContent, f.TenantID, adminID, adminID, true)
	if err != nil {
		t.Fatalf("set content admin: %v", err)
	}
	if result.Status != permission.GrantStatusGranted {
		t.Fatalf("set content admin status = %s, want granted (%#v)", result.Status, result)
	}

	rows, err := permission.ListProductAdmins(ctx, pool, f.TenantID)
	if err != nil {
		t.Fatalf("list product admins: %v", err)
	}
	var contentRows int
	for _, row := range rows {
		if row.Product == permission.ProductContent {
			contentRows++
			if row.EmployeeID != adminID {
				t.Fatalf("content admin employee = %d, want %d", row.EmployeeID, adminID)
			}
			if row.EmployeeName != "内容管理员" {
				t.Fatalf("content admin name = %q, want 内容管理员", row.EmployeeName)
			}
		}
	}
	if contentRows != 1 {
		t.Fatalf("content admin rows = %d, want 1", contentRows)
	}

	if _, err := permission.SetProductAdmin(ctx, pool, permission.ProductContent, f.TenantID, adminID, adminID, false); err != nil {
		t.Fatalf("remove content admin: %v", err)
	}
	rows, err = permission.ListProductAdmins(ctx, pool, f.TenantID)
	if err != nil {
		t.Fatalf("list after remove: %v", err)
	}
	for _, row := range rows {
		if row.Product == permission.ProductContent {
			t.Fatalf("content admin still listed after remove: %#v", row)
		}
	}
}
