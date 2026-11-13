package his

import (
	"context"
	"errors"
	"testing"
)

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// 租户对角色的模块配置必须“替换”默认模板，而不是与默认模板取并集。
func TestIntegrationRoleModuleTenantOverride(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	const tenantID, employeeID = int64(1), int64(10)

	if err := svc.UpdateStaffRoles(ctx, tenantID, employeeID, []string{"doctor"}); err != nil {
		t.Fatalf("assign doctor role: %v", err)
	}
	modules, err := svc.ModulesForEmployee(ctx, tenantID, employeeID)
	if err != nil {
		t.Fatalf("modules for employee: %v", err)
	}
	if !containsString(modules, "stats") || !containsString(modules, "clinic") {
		t.Fatalf("default doctor modules = %v, want stats/clinic", modules)
	}

	saved, err := svc.UpdateRoleModules(ctx, tenantID, "doctor", []string{"stats", "home", "clinic", "patients", "stats"})
	if err != nil {
		t.Fatalf("update role modules: %v", err)
	}
	if len(saved) != 4 {
		t.Fatalf("saved modules = %v, want 4 deduped modules", saved)
	}

	// 取消「统计」：租户覆盖生效后，员工视角不应再看到 stats。
	if _, err := svc.UpdateRoleModules(ctx, tenantID, "doctor", []string{"home", "clinic", "patients"}); err != nil {
		t.Fatalf("remove stats: %v", err)
	}
	modules, err = svc.ModulesForEmployee(ctx, tenantID, employeeID)
	if err != nil {
		t.Fatalf("modules after override: %v", err)
	}
	if containsString(modules, "stats") {
		t.Fatalf("modules = %v, want stats removed by tenant override", modules)
	}
	if len(modules) != 3 {
		t.Fatalf("modules = %v, want exactly 3 modules", modules)
	}

	config, err := svc.RoleModuleConfig(ctx, tenantID)
	if err != nil {
		t.Fatalf("role module config: %v", err)
	}
	if got := config["doctor"]; containsString(got, "stats") || len(got) != 3 {
		t.Fatalf("config doctor = %v, want tenant override without stats", got)
	}
	if got := config["nurse"]; !containsString(got, "execute") {
		t.Fatalf("config nurse = %v, want default template", got)
	}
	if got := config["admin"]; len(got) != 14 {
		t.Fatalf("config admin = %v, want all 14 modules (13 nav + dictionary)", got)
	}

	if _, err := svc.UpdateRoleModules(ctx, tenantID, "admin", []string{"home"}); !errors.Is(err, ErrAdminRoleLocked) {
		t.Fatalf("admin update err = %v, want ErrAdminRoleLocked", err)
	}
	if _, err := svc.UpdateRoleModules(ctx, tenantID, "pharmacist", []string{"home"}); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("legacy role err = %v, want ErrInvalidRole", err)
	}
	if _, err := svc.UpdateRoleModules(ctx, tenantID, "doctor", []string{"home", "settings"}); !errors.Is(err, ErrInvalidModule) {
		t.Fatalf("invalid module err = %v, want ErrInvalidModule", err)
	}
	if _, err := svc.UpdateRoleModules(ctx, tenantID, "doctor", nil); !errors.Is(err, ErrEmptyRoleModules) {
		t.Fatalf("empty modules err = %v, want ErrEmptyRoleModules", err)
	}
}

// dictionary 是独立权限点：默认只有管理员有，租户可在角色模块里显式授予其他角色。
func TestIntegrationDictionaryPermission(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	const tenantID, employeeID = int64(1), int64(10)

	if err := svc.UpdateStaffRoles(ctx, tenantID, employeeID, []string{"doctor"}); err != nil {
		t.Fatalf("assign doctor role: %v", err)
	}
	if err := svc.RequireDictionary(ctx, tenantID, employeeID); !errors.Is(err, ErrDictionaryForbidden) {
		t.Fatalf("doctor without dictionary err = %v, want ErrDictionaryForbidden", err)
	}

	if _, err := svc.UpdateRoleModules(ctx, tenantID, "doctor", []string{"home", "dictionary"}); err != nil {
		t.Fatalf("grant dictionary: %v", err)
	}
	if err := svc.RequireDictionary(ctx, tenantID, employeeID); err != nil {
		t.Fatalf("doctor with dictionary should pass: %v", err)
	}

	if err := svc.UpdateStaffRoles(ctx, tenantID, employeeID, []string{"admin"}); err != nil {
		t.Fatalf("assign admin role: %v", err)
	}
	if err := svc.RequireDictionary(ctx, tenantID, employeeID); err != nil {
		t.Fatalf("admin should always pass: %v", err)
	}
}

func TestIntegrationDeleteScheduleRules(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	const tenantID = int64(1)

	empty := createTestSchedule(t, svc, tenantID, "10:00")
	if err := svc.DeleteSchedule(ctx, tenantID, empty.ID); err != nil {
		t.Fatalf("delete empty schedule: %v", err)
	}
	if _, err := svc.GetSchedule(ctx, tenantID, empty.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted schedule err = %v, want ErrNotFound", err)
	}
	if err := svc.DeleteSchedule(ctx, tenantID, empty.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete err = %v, want ErrNotFound", err)
	}

	occupied, err := svc.CreateSchedule(ctx, tenantID, CreateScheduleRequest{
		DoctorID: empty.DoctorID, WorkDate: "2026-11-21", Period: "morning",
		StartTime: "09:00", EndTime: "10:00", SlotIntervalMinutes: 15,
	})
	if err != nil {
		t.Fatalf("create occupied schedule: %v", err)
	}
	patient := registerTestPatient(t, svc, tenantID, "排班删除测试", "13900009999")
	registration, err := svc.CreateRegistration(ctx, tenantID, 10, CreateRegistrationRequest{
		CustomerID: patient.CustomerID, ScheduleID: occupied.ID, RegType: "walkin", Fee: 10,
	})
	if err != nil {
		t.Fatalf("create registration: %v", err)
	}

	if err := svc.DeleteSchedule(ctx, tenantID, occupied.ID); !errors.Is(err, ErrScheduleOccupied) {
		t.Fatalf("delete occupied err = %v, want ErrScheduleOccupied", err)
	}
	if _, err := svc.GetSchedule(ctx, tenantID, occupied.ID); err != nil {
		t.Fatalf("occupied schedule should remain: %v", err)
	}
	slots, err := svc.ListSlots(ctx, tenantID, occupied.ID)
	if err != nil || len(slots) == 0 {
		t.Fatalf("slots after rejected delete = %d, err = %v, want > 0", len(slots), err)
	}

	// 退号释放号源后允许删除。
	if _, err := svc.RefundRegistration(ctx, tenantID, registration.ID); err != nil {
		t.Fatalf("refund registration: %v", err)
	}
	if err := svc.DeleteSchedule(ctx, tenantID, occupied.ID); err != nil {
		t.Fatalf("delete after refund: %v", err)
	}
	if _, err := svc.GetSchedule(ctx, tenantID, occupied.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("schedule should be deleted, err = %v", err)
	}
}

func TestIntegrationUpdateChargeItem(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	const tenantID = int64(1)

	item, err := svc.CreateChargeItem(ctx, tenantID, CreateChargeItemRequest{
		Code: " T900 ", Name: " 管理页测试项目 ", Category: "检查", Unit: "次", Price: 10,
	})
	if err != nil {
		t.Fatalf("create charge item: %v", err)
	}
	if item.Code != "T900" || item.Name != "管理页测试项目" || item.Status != "active" {
		t.Fatalf("created item = %+v, want trimmed code/name and active status", item)
	}
	if _, err := svc.CreateChargeItem(ctx, tenantID, CreateChargeItemRequest{Code: "T900", Name: "重复编码", Price: 1}); !errors.Is(err, ErrChargeItemCodeExists) {
		t.Fatalf("duplicate code err = %v, want ErrChargeItemCodeExists", err)
	}

	name, status, price := " 改价后 ", "inactive", 18.5
	updated, err := svc.UpdateChargeItem(ctx, tenantID, item.ID, UpdateChargeItemRequest{
		Name: &name, Status: &status, Price: &price,
	})
	if err != nil {
		t.Fatalf("update charge item: %v", err)
	}
	if updated.Name != "改价后" || updated.Status != "inactive" || updated.Price != 18.5 {
		t.Fatalf("updated item = %+v, want name/status/price applied", updated)
	}
	items, err := svc.ListChargeItems(ctx, tenantID)
	if err != nil {
		t.Fatalf("list charge items: %v", err)
	}
	found := false
	for _, candidate := range items {
		if candidate.ID == item.ID {
			found = true
			if candidate.Price != 18.5 || candidate.Status != "inactive" {
				t.Fatalf("listed item = %+v, want price 18.5 inactive", candidate)
			}
		}
	}
	if !found {
		t.Fatalf("updated item %d missing from list", item.ID)
	}

	if _, err := svc.UpdateChargeItem(ctx, tenantID, item.ID, UpdateChargeItemRequest{Status: strPtr("deleted")}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid status err = %v, want ErrInvalidRequest", err)
	}
	if _, err := svc.UpdateChargeItem(ctx, tenantID, 999999, UpdateChargeItemRequest{Price: &price}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing item err = %v, want ErrNotFound", err)
	}
}

func TestIntegrationSetEmployeeActive(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	const tenantID, employeeID = int64(1), int64(10)

	active, err := svc.SetEmployeeActive(ctx, tenantID, employeeID, false)
	if err != nil {
		t.Fatalf("disable employee: %v", err)
	}
	if active {
		t.Fatalf("active = true, want false")
	}
	rows, err := svc.ListEmployeesWithRoles(ctx, tenantID)
	if err != nil {
		t.Fatalf("list employees: %v", err)
	}
	if len(rows) != 1 || rows[0].IsActive {
		t.Fatalf("employees = %+v, want single inactive employee", rows)
	}
	active, err = svc.SetEmployeeActive(ctx, tenantID, employeeID, true)
	if err != nil || !active {
		t.Fatalf("enable employee: active = %v, err = %v", active, err)
	}
	if _, err := svc.SetEmployeeActive(ctx, tenantID, 999999, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing employee err = %v, want ErrNotFound", err)
	}
}
