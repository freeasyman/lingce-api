package billing_test

// 运营端「编辑机构 + 管理员管理」集成测试（真实 PostgreSQL）。
//
// 运行前置：测试库已跑完迁移（含 20261112_011）：
//
//	BILLING_TEST_DATABASE_URL='postgresql://yiliiang@127.0.0.1:5432/lingce_billing_test?sslmode=disable' \
//	  go test ./internal/billing/ -run 'TestInstitutionUpdateProfile|TestInstitutionAdminLifecycle' -count=1 -v
//
// 与其它 e2e 用例的约定一致：未设置 DSN 时 Skip；用例自建运营组织/渠道/销售/租户，
// 结束按租户清理（含本用例新增的 system_action_logs / employee_login_events）。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/freeasyman/lingce-api/internal/billing"
	"golang.org/x/crypto/bcrypt"
)

// cleanupInstitutionArtifacts 清理本用例写入的日志/登录事件（tenant 数据由 env.cleanup 删）。
func (e *billingTestEnv) cleanupInstitutionArtifacts() {
	e.t.Helper()
	e.t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if len(e.tenantIDs) == 0 {
			return
		}
		if _, err := e.pool.Exec(ctx, `DELETE FROM system_action_logs WHERE tenant_id = ANY($1)`, e.tenantIDs); err != nil {
			e.t.Logf("清理 system_action_logs 失败: %v", err)
		}
		if _, err := e.pool.Exec(ctx, `DELETE FROM employee_login_events WHERE tenant_id = ANY($1)`, e.tenantIDs); err != nil {
			e.t.Logf("清理 employee_login_events 失败: %v", err)
		}
	})
}

// departmentIDsFor 按名称取科室字典 id。
func (e *billingTestEnv) departmentIDsFor(names ...string) []int64 {
	e.t.Helper()
	rows, err := e.pool.Query(e.ctx, `SELECT id FROM billing_department_dict WHERE name = ANY($1)`, names)
	if err != nil {
		e.t.Fatalf("查询科室字典失败: %v", err)
	}
	defer rows.Close()
	ids := make([]int64, 0, len(names))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			e.t.Fatalf("扫描科室 id 失败: %v", err)
		}
		ids = append(ids, id)
	}
	if len(ids) != len(names) {
		e.t.Fatalf("科室字典缺失：期望 %d 个，实际 %d", len(names), len(ids))
	}
	return ids
}

// tenantAdminRow 读一条员工管理员的关键列（绕过 service 校验落库结果）。
func (e *billingTestEnv) tenantAdminRow(adminID int64) (name, phone, email, passwordHash string, isActive, isPrimary bool, sessionVersion int) {
	e.t.Helper()
	if err := e.pool.QueryRow(e.ctx, `
SELECT COALESCE(name, ''), COALESCE(phone, ''), COALESCE(email, ''), COALESCE(password_hash, ''),
       lower(COALESCE(is_active::text, 'true')) IN ('1','t','true','yes'),
       COALESCE(is_primary, FALSE),
       COALESCE(session_version, 1)
FROM employees
WHERE id = $1`, adminID).Scan(&name, &phone, &email, &passwordHash, &isActive, &isPrimary, &sessionVersion); err != nil {
		e.t.Fatalf("读取管理员 %d 失败: %v", adminID, err)
	}
	return
}

// ---------------- 编辑机构档案 ----------------

func TestInstitutionUpdateProfile(t *testing.T) {
	env := newBillingTestEnv(t)
	env.cleanupInstitutionArtifacts()
	ctx := env.ctx

	channelName, salesName := env.seedChannelWithSales("编辑")
	institution := env.createInstitution("编辑前门诊", "口腔", []string{"口腔科"}, channelName, salesName)

	var originalCode, originalMode, originalAccountMode string
	if err := env.pool.QueryRow(ctx, `
SELECT code, billing_mode, account_mode FROM tenants WHERE id = $1`, institution.ID).
		Scan(&originalCode, &originalMode, &originalAccountMode); err != nil {
		t.Fatalf("读取机构原始字段失败: %v", err)
	}

	name := "编辑后门诊"
	legalName := "编辑后门诊有限公司"
	industry := "眼科"
	contactName := "王编辑"
	contactTitle := "院长"
	contactPhone := env.nextPhone("13")
	contactEmail := "edit@example.com"
	provinceCode, cityCode, districtCode := "320000", "320100", "320102"
	customerType := "group"
	departmentIDs := env.departmentIDsFor("眼科", "咨询客服")

	updated, err := env.svc.UpdateInstitution(ctx, institution.ID, billing.UpdateInstitutionRequest{
		Name:          &name,
		LegalName:     &legalName,
		Industry:      &industry,
		ContactName:   &contactName,
		ContactTitle:  &contactTitle,
		ContactPhone:  &contactPhone,
		ContactEmail:  &contactEmail,
		ProvinceCode:  &provinceCode,
		CityCode:      &cityCode,
		DistrictCode:  &districtCode,
		DepartmentIDs: &departmentIDs,
		CustomerType:  &customerType,
	}, env.adminID)
	if err != nil {
		t.Fatalf("编辑机构失败: %v", err)
	}
	if updated.Name != name || updated.Legal != legalName || updated.Industry != industry {
		t.Fatalf("名称/全称/行业未生效: %+v", updated)
	}
	if updated.Contact != contactName || updated.Title != contactTitle ||
		updated.Phone != contactPhone || updated.Email != contactEmail {
		t.Fatalf("联系人字段未生效: %+v", updated)
	}
	if updated.Region.Province != provinceCode || updated.Region.City != cityCode || updated.Region.District != districtCode {
		t.Fatalf("省市区未生效: %+v", updated.Region)
	}
	if got := strings.Join(updated.Departments, ","); !strings.Contains(got, "眼科") || !strings.Contains(got, "咨询客服") {
		t.Fatalf("涉及科室未全量替换: %v", updated.Departments)
	}

	// 落库校验：code / billing_mode / account_mode 不可变；customer_type 已更新。
	var code, billingMode, accountMode, storedCustomerType, storedLegal string
	if err := env.pool.QueryRow(ctx, `
SELECT code, billing_mode, account_mode, customer_type, COALESCE(legal_name, '')
FROM tenants WHERE id = $1`, institution.ID).
		Scan(&code, &billingMode, &accountMode, &storedCustomerType, &storedLegal); err != nil {
		t.Fatalf("读取机构落库字段失败: %v", err)
	}
	if code != originalCode || billingMode != originalMode || accountMode != originalAccountMode {
		t.Fatalf("code/billing_mode/account_mode 不应被编辑: code=%s mode=%s/%s account=%s/%s",
			code, billingMode, originalMode, accountMode, originalAccountMode)
	}
	if storedLegal != legalName || storedCustomerType != customerType {
		t.Fatalf("计费档案列未落库: legal=%q customer_type=%q", storedLegal, storedCustomerType)
	}

	var departmentCount int
	if err := env.pool.QueryRow(ctx, `SELECT COUNT(*) FROM tenant_departments WHERE tenant_id = $1`, institution.ID).Scan(&departmentCount); err != nil {
		t.Fatalf("统计科室失败: %v", err)
	}
	if departmentCount != len(departmentIDs) {
		t.Fatalf("tenant_departments 应全量替换为 %d 条，实际 %d", len(departmentIDs), departmentCount)
	}

	// 变更日志已写（actor=运营账号，target=机构）。
	var logCount int
	if err := env.pool.QueryRow(ctx, `
SELECT COUNT(*) FROM system_action_logs
WHERE tenant_id = $1 AND action_code = 'institution.update' AND object_type = 'institution'`, institution.ID).Scan(&logCount); err != nil {
		t.Fatalf("查询变更日志失败: %v", err)
	}
	if logCount == 0 {
		t.Fatalf("编辑机构应写入 system_action_logs")
	}

	// 行业不在字典 -> 400。
	badIndustry := "不存在的行业"
	if _, err := env.svc.UpdateInstitution(ctx, institution.ID, billing.UpdateInstitutionRequest{Industry: &badIndustry}, env.adminID); err == nil {
		t.Fatalf("非法行业应被拒绝")
	} else {
		var validation *billing.ValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("非法行业应为校验错误，实际 %T %v", err, err)
		}
	}

	// 科室不存在 -> 400。
	badDepartmentIDs := []int64{999999999}
	if _, err := env.svc.UpdateInstitution(ctx, institution.ID, billing.UpdateInstitutionRequest{DepartmentIDs: &badDepartmentIDs}, env.adminID); err == nil {
		t.Fatalf("不存在的科室应被拒绝")
	}

	// 手机号冲突 -> 409（ConflictError 携带可读提示）。
	other := env.createInstitution("撞号门诊", "口腔", []string{"口腔科"}, channelName, salesName)
	if _, err := env.svc.UpdateInstitution(ctx, institution.ID, billing.UpdateInstitutionRequest{ContactPhone: &other.Phone}, env.adminID); err == nil {
		t.Fatalf("重复联系人手机号应冲突")
	} else {
		var conflict *billing.ConflictError
		if !errors.As(err, &conflict) || !errors.Is(err, billing.ErrInstitutionPhoneConflict) {
			t.Fatalf("重复手机号应为手机号冲突，实际 %T %v", err, err)
		}
		if !strings.Contains(err.Error(), "该手机号已被") {
			t.Fatalf("冲突提示不友好: %v", err)
		}
	}

	// 机构不存在 -> 404。
	if _, err := env.svc.UpdateInstitution(ctx, 999999999, billing.UpdateInstitutionRequest{Name: &name}, env.adminID); !errors.Is(err, billing.ErrInstitutionNotFound) {
		t.Fatalf("不存在的机构应 404，实际 %v", err)
	}

	// 越权（他组织运营账号）-> 403。
	otherChannelName := "他组织渠道-" + env.suffix
	var otherOrgID int64
	if err := env.pool.QueryRow(ctx, `
INSERT INTO ops_organizations (name, type, status) VALUES ($1, 'agency', 'active') RETURNING id`, otherChannelName).Scan(&otherOrgID); err != nil {
		t.Fatalf("创建其他组织失败: %v", err)
	}
	env.channelIDs = append(env.channelIDs, otherOrgID)
	var otherAdminID int64
	if err := env.pool.QueryRow(ctx, `
INSERT INTO operations_admins (name, phone, username, password_hash, org_id, is_active, session_version, created_at, updated_at)
VALUES ($1, $2::varchar, $2::text, 'x', $3, 1, 1, NOW(), NOW())
RETURNING id`, "他组织运营-"+env.suffix, env.nextPhone("19"), otherOrgID).Scan(&otherAdminID); err != nil {
		t.Fatalf("创建其他运营账号失败: %v", err)
	}
	env.salesIDs = append(env.salesIDs, otherAdminID)
	if _, err := env.svc.UpdateInstitution(ctx, institution.ID, billing.UpdateInstitutionRequest{Name: &name}, otherAdminID); !errors.Is(err, billing.ErrNoPermission) {
		t.Fatalf("越权编辑应 403，实际 %v", err)
	}
}

// ---------------- 管理员管理 ----------------

func TestInstitutionAdminLifecycle(t *testing.T) {
	env := newBillingTestEnv(t)
	env.cleanupInstitutionArtifacts()
	ctx := env.ctx

	channelName, salesName := env.seedChannelWithSales("管理员")
	institution := env.createInstitution("管理员测试门诊", "口腔", []string{"口腔科"}, channelName, salesName)

	// 1. 建档自带一个启用的主管理员。
	list, err := env.svc.ListInstitutionAdmins(ctx, institution.ID, env.adminID)
	if err != nil {
		t.Fatalf("查询管理员列表失败: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("新建机构应有 1 个管理员，实际 %d", len(list.Items))
	}
	primary := list.Items[0]
	if !primary.IsPrimary || !primary.IsActive || primary.RoleCode != "admin" {
		t.Fatalf("建档管理员应是启用主管理员: %+v", primary)
	}
	if primary.Phone != institution.Phone {
		t.Fatalf("主管理员手机号应与建档联系人一致: %s != %s", primary.Phone, institution.Phone)
	}

	// 最后登录时间从 employee_login_events 读。
	if _, err := env.pool.Exec(ctx, `
INSERT INTO employee_login_events (tenant_id, employee_id, login_at, login_source, ip, user_agent, created_at)
VALUES ($1, $2, NOW(), 'institution_web', '127.0.0.1', 'test', NOW())`, institution.ID, primary.ID); err != nil {
		t.Fatalf("写入登录事件失败: %v", err)
	}
	list, err = env.svc.ListInstitutionAdmins(ctx, institution.ID, env.adminID)
	if err != nil {
		t.Fatalf("再次查询管理员列表失败: %v", err)
	}
	if list.Items[0].LastLoginAt == "" {
		t.Fatalf("有登录事件时 lastLoginAt 不应为空: %+v", list.Items[0])
	}

	// 1b. 兜底自愈：历史数据没有 is_primary 时，读列表会按「联系人手机号 / 最早管理员」补一个。
	if _, err := env.pool.Exec(ctx, `UPDATE employees SET is_primary = FALSE WHERE tenant_id = $1`, institution.ID); err != nil {
		t.Fatalf("清除主管理员标记失败: %v", err)
	}
	healed, err := env.svc.ListInstitutionAdmins(ctx, institution.ID, env.adminID)
	if err != nil {
		t.Fatalf("自愈后查询管理员失败: %v", err)
	}
	if len(healed.Items) != 1 || !healed.Items[0].IsPrimary || healed.Items[0].ID != primary.ID {
		t.Fatalf("缺少主管理员时应自动补回同一个人: %+v", healed.Items)
	}

	// 2. 新增管理员：默认非主管理员，密码是默认口令，角色 admin。
	newPhone := env.nextPhone("16")
	created, err := env.svc.CreateInstitutionAdmin(ctx, institution.ID, billing.CreateInstitutionAdminRequest{
		Name:  "新管理员",
		Phone: newPhone,
		Email: "new-admin@example.com",
	}, env.adminID)
	if err != nil {
		t.Fatalf("新增管理员失败: %v", err)
	}
	if created.ID <= 0 || created.IsPrimary || !created.IsActive || created.RoleCode != "admin" {
		t.Fatalf("新增管理员字段不对: %+v", created)
	}
	name, phone, email, passwordHash, isActive, isPrimary, sessionVersion := env.tenantAdminRow(created.ID)
	if name != "新管理员" || phone != newPhone || email != "new-admin@example.com" || !isActive || isPrimary {
		t.Fatalf("新增管理员落库不对: name=%s phone=%s email=%s active=%v primary=%v", name, phone, email, isActive, isPrimary)
	}
	// 默认口令沿用 tenant.defaultTenantAdminPassword（"123456"），响应不回传明文。
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte("123456")); err != nil {
		t.Fatalf("新增管理员默认口令不对: %v", err)
	}
	if sessionVersion != 1 {
		t.Fatalf("新增管理员 session_version 应为 1，实际 %d", sessionVersion)
	}
	var roleCode string
	if err := env.pool.QueryRow(ctx, `
SELECT COALESCE(role_code, '') FROM institution_employee_roles WHERE tenant_id = $1 AND employee_id = $2`,
		institution.ID, created.ID).Scan(&roleCode); err != nil {
		t.Fatalf("读取管理员角色失败: %v", err)
	}
	if roleCode != "admin" {
		t.Fatalf("管理员角色应为 admin，实际 %q", roleCode)
	}

	// 3. 手机号冲突（机构员工 + 运营账号都算全局唯一）。
	if _, err := env.svc.CreateInstitutionAdmin(ctx, institution.ID, billing.CreateInstitutionAdminRequest{
		Name: "撞号管理员", Phone: newPhone,
	}, env.adminID); err == nil {
		t.Fatalf("重复手机号应冲突")
	} else {
		var conflict *billing.ConflictError
		if !errors.As(err, &conflict) || !errors.Is(err, billing.ErrInstitutionAdminPhoneConflict) {
			t.Fatalf("重复手机号应为管理员手机号冲突，实际 %T %v", err, err)
		}
		if !strings.Contains(err.Error(), "该手机号已被") {
			t.Fatalf("冲突提示不友好: %v", err)
		}
	}
	var salesPhone string
	if err := env.pool.QueryRow(ctx, `
SELECT COALESCE(a.phone, '')
FROM operations_admins a
JOIN ops_organizations o ON o.id = a.org_id
WHERE o.name = $1
ORDER BY a.id DESC
LIMIT 1`, channelName).Scan(&salesPhone); err != nil {
		t.Fatalf("读取销售手机号失败: %v", err)
	}
	if salesPhone != "" {
		if _, err := env.svc.CreateInstitutionAdmin(ctx, institution.ID, billing.CreateInstitutionAdminRequest{
			Name: "运营撞号", Phone: salesPhone,
		}, env.adminID); err == nil || !errors.Is(err, billing.ErrInstitutionAdminPhoneConflict) {
			t.Fatalf("运营账号手机号也应冲突，实际 %v", err)
		}
	}

	// 非法角色 -> 400。
	if _, err := env.svc.CreateInstitutionAdmin(ctx, institution.ID, billing.CreateInstitutionAdminRequest{
		Name: "非法角色", Phone: env.nextPhone("16"), Role: "doctor",
	}, env.adminID); err == nil {
		t.Fatalf("非法角色应被拒绝")
	}

	// 4. 修改基本信息。
	updatedName, updatedEmail := "改名管理员", "renamed@example.com"
	updated, err := env.svc.UpdateInstitutionAdmin(ctx, institution.ID, created.ID, billing.UpdateInstitutionAdminRequest{
		Name:  &updatedName,
		Email: &updatedEmail,
	}, env.adminID)
	if err != nil {
		t.Fatalf("修改管理员失败: %v", err)
	}
	if updated.Name != updatedName || updated.Email != updatedEmail {
		t.Fatalf("管理员修改未生效: %+v", updated)
	}

	// 5. 重置密码：默认口令 + session_version+1，响应不含密码。
	if _, err := env.pool.Exec(ctx, `UPDATE employees SET password_hash = 'stale-hash', session_version = 7 WHERE id = $1`, created.ID); err != nil {
		t.Fatalf("准备重置密码失败: %v", err)
	}
	reset, err := env.svc.ResetInstitutionAdminPassword(ctx, institution.ID, created.ID, env.adminID)
	if err != nil {
		t.Fatalf("重置密码失败: %v", err)
	}
	if strings.Contains(fmt.Sprintf("%+v", reset), "123456") {
		t.Fatalf("重置密码响应不应包含明文: %+v", reset)
	}
	_, _, _, passwordHash, _, _, sessionVersion = env.tenantAdminRow(created.ID)
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte("123456")); err != nil {
		t.Fatalf("重置后密码应为默认口令: %v", err)
	}
	if sessionVersion != 8 {
		t.Fatalf("重置密码应 bump session_version（7 -> 8），实际 %d", sessionVersion)
	}

	// 6. 不能停用最后一个启用的主管理员。
	inactive := false
	if _, err := env.svc.UpdateInstitutionAdmin(ctx, institution.ID, primary.ID,
		billing.UpdateInstitutionAdminRequest{IsActive: &inactive}, env.adminID); err == nil {
		t.Fatalf("停用唯一主管理员应被拒绝")
	} else if !strings.Contains(err.Error(), "主管理员") {
		t.Fatalf("停用主管理员提示不对: %v", err)
	}

	// 7. 设为主管理员：旧主管理员自动降级。
	promoted, err := env.svc.SetInstitutionAdminPrimary(ctx, institution.ID, created.ID, env.adminID)
	if err != nil {
		t.Fatalf("设为主管理员失败: %v", err)
	}
	if !promoted.IsPrimary {
		t.Fatalf("设置后 isPrimary 应为 true: %+v", promoted)
	}
	_, _, _, _, _, primaryFlag, _ := env.tenantAdminRow(primary.ID)
	if primaryFlag {
		t.Fatalf("旧主管理员应被降级")
	}
	list, err = env.svc.ListInstitutionAdmins(ctx, institution.ID, env.adminID)
	if err != nil {
		t.Fatalf("设主管理员后查询失败: %v", err)
	}
	primaryCount := 0
	for _, item := range list.Items {
		if item.IsPrimary {
			primaryCount++
		}
	}
	if primaryCount != 1 || !list.Items[0].IsPrimary {
		t.Fatalf("同一机构应只有一个主管理员且排在最前: %+v", list.Items)
	}

	// 8. 旧主管理员（已降级）可以停用；停用会 bump session_version。
	_, _, _, _, _, _, oldVersion := env.tenantAdminRow(primary.ID)
	disabled, err := env.svc.UpdateInstitutionAdmin(ctx, institution.ID, primary.ID,
		billing.UpdateInstitutionAdminRequest{IsActive: &inactive}, env.adminID)
	if err != nil {
		t.Fatalf("停用已降级管理员失败: %v", err)
	}
	if disabled.IsActive {
		t.Fatalf("停用后 isActive 应为 false")
	}
	_, _, _, _, activeFlag, _, newVersion := env.tenantAdminRow(primary.ID)
	if activeFlag || newVersion != oldVersion+1 {
		t.Fatalf("停用应置 is_active=0 且 bump session_version: active=%v version=%d->%d", activeFlag, oldVersion, newVersion)
	}

	// 9. 新主管理员是最后一个，仍然不能停用。
	if _, err := env.svc.UpdateInstitutionAdmin(ctx, institution.ID, created.ID,
		billing.UpdateInstitutionAdminRequest{IsActive: &inactive}, env.adminID); err == nil {
		t.Fatalf("停用最后一个主管理员应被拒绝")
	}

	// 10. 停用的管理员不能设为主管理员。
	third, err := env.svc.CreateInstitutionAdmin(ctx, institution.ID, billing.CreateInstitutionAdminRequest{
		Name: "待停用管理员", Phone: env.nextPhone("16"),
	}, env.adminID)
	if err != nil {
		t.Fatalf("创建第三个管理员失败: %v", err)
	}
	if _, err := env.svc.UpdateInstitutionAdmin(ctx, institution.ID, third.ID,
		billing.UpdateInstitutionAdminRequest{IsActive: &inactive}, env.adminID); err != nil {
		t.Fatalf("停用非主管理员失败: %v", err)
	}
	if _, err := env.svc.SetInstitutionAdminPrimary(ctx, institution.ID, third.ID, env.adminID); err == nil ||
		!strings.Contains(err.Error(), "停用") {
		t.Fatalf("停用管理员设为主管理员应被拒绝，实际 %v", err)
	}

	// 11. adminId 必须属于该机构：跨机构 404。
	other := env.createInstitution("另一家门诊", "口腔", []string{"口腔科"}, channelName, salesName)
	otherList, err := env.svc.ListInstitutionAdmins(ctx, other.ID, env.adminID)
	if err != nil || len(otherList.Items) != 1 {
		t.Fatalf("另一家机构管理员列表异常: %+v %v", otherList, err)
	}
	if _, err := env.svc.UpdateInstitutionAdmin(ctx, institution.ID, otherList.Items[0].ID,
		billing.UpdateInstitutionAdminRequest{Name: &updatedName}, env.adminID); !errors.Is(err, billing.ErrInstitutionAdminNotFound) {
		t.Fatalf("跨机构改管理员应 404，实际 %v", err)
	}
	if _, err := env.svc.ResetInstitutionAdminPassword(ctx, institution.ID, otherList.Items[0].ID, env.adminID); !errors.Is(err, billing.ErrInstitutionAdminNotFound) {
		t.Fatalf("跨机构重置密码应 404，实际 %v", err)
	}
	if _, err := env.svc.SetInstitutionAdminPrimary(ctx, institution.ID, otherList.Items[0].ID, env.adminID); !errors.Is(err, billing.ErrInstitutionAdminNotFound) {
		t.Fatalf("跨机构设主管理员应 404，实际 %v", err)
	}

	// 12. 数据权限：他组织运营账号一律 403。
	otherChannelName := "他组织渠道-" + env.suffix
	var otherOrgID int64
	if err := env.pool.QueryRow(ctx, `
INSERT INTO ops_organizations (name, type, status) VALUES ($1, 'agency', 'active') RETURNING id`, otherChannelName).Scan(&otherOrgID); err != nil {
		t.Fatalf("创建其他组织失败: %v", err)
	}
	env.channelIDs = append(env.channelIDs, otherOrgID)
	var otherAdminID int64
	if err := env.pool.QueryRow(ctx, `
INSERT INTO operations_admins (name, phone, username, password_hash, org_id, is_active, session_version, created_at, updated_at)
VALUES ($1, $2::varchar, $2::text, 'x', $3, 1, 1, NOW(), NOW())
RETURNING id`, "他组织运营-"+env.suffix, env.nextPhone("19"), otherOrgID).Scan(&otherAdminID); err != nil {
		t.Fatalf("创建其他运营账号失败: %v", err)
	}
	env.salesIDs = append(env.salesIDs, otherAdminID)

	if _, err := env.svc.ListInstitutionAdmins(ctx, institution.ID, otherAdminID); !errors.Is(err, billing.ErrNoPermission) {
		t.Fatalf("越权读管理员应 403，实际 %v", err)
	}
	if _, err := env.svc.CreateInstitutionAdmin(ctx, institution.ID, billing.CreateInstitutionAdminRequest{
		Name: "越权新增", Phone: env.nextPhone("16"),
	}, otherAdminID); !errors.Is(err, billing.ErrNoPermission) {
		t.Fatalf("越权新增管理员应 403，实际 %v", err)
	}
	if _, err := env.svc.ResetInstitutionAdminPassword(ctx, institution.ID, created.ID, otherAdminID); !errors.Is(err, billing.ErrNoPermission) {
		t.Fatalf("越权重置密码应 403，实际 %v", err)
	}
}
