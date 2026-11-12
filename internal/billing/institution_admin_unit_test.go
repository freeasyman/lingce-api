package billing

// 机构编辑 + 管理员管理的纯逻辑单测：入参校验、DTO 映射、变更摘要、角色归一。
// 不依赖数据库，随 `go test ./internal/billing/...` 一起跑。

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func strPtr(value string) *string { return &value }
func boolPtr(value bool) *bool    { return &value }

// TestNormalizeUpdateInstitutionRequest 覆盖编辑机构的字段校验。
func TestNormalizeUpdateInstitutionRequest(t *testing.T) {
	valid := UpdateInstitutionRequest{
		Name:         strPtr(" 上海测试口腔 "),
		LegalName:    strPtr(" 上海测试口腔有限公司 "),
		Industry:     strPtr(" 口腔 "),
		ContactName:  strPtr(" 张三 "),
		ContactTitle: strPtr(" 信息科 "),
		ContactPhone: strPtr("13800000000"),
		ContactEmail: strPtr(" test@example.com "),
		ProvinceCode: strPtr("310000"),
		CityCode:     strPtr("310100"),
		DistrictCode: strPtr("310104"),
		CustomerType: strPtr(" GROUP "),
	}
	departmentIDs := []int64{3, 1, 3, 2}
	valid.DepartmentIDs = &departmentIDs

	normalized, err := valid.normalize()
	if err != nil {
		t.Fatalf("合法入参应通过: %v", err)
	}
	if *normalized.Name != "上海测试口腔" || *normalized.ContactEmail != "test@example.com" {
		t.Fatalf("字段未 trim: %+v", normalized)
	}
	if *normalized.CustomerType != CustomerTypeGroup {
		t.Fatalf("customerType 未归一: %q", *normalized.CustomerType)
	}
	if got := *normalized.DepartmentIDs; len(got) != 3 || got[0] != 3 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("departmentIds 未去重/保序: %v", got)
	}

	cases := []struct {
		name    string
		mutate  func(*UpdateInstitutionRequest)
		wantMsg string
	}{
		{"空简称", func(r *UpdateInstitutionRequest) { r.Name = strPtr("  ") }, "机构简称"},
		{"空全称", func(r *UpdateInstitutionRequest) { r.LegalName = strPtr(" ") }, "机构全称"},
		{"空行业", func(r *UpdateInstitutionRequest) { r.Industry = strPtr("") }, "行业"},
		{"空联系人", func(r *UpdateInstitutionRequest) { r.ContactName = strPtr("") }, "联系人"},
		{"手机号格式", func(r *UpdateInstitutionRequest) { r.ContactPhone = strPtr("12345") }, "手机号"},
		{"邮箱格式", func(r *UpdateInstitutionRequest) { r.ContactEmail = strPtr("not-an-email") }, "邮箱"},
		{"客户类型", func(r *UpdateInstitutionRequest) { r.CustomerType = strPtr("vip") }, "customerType"},
		{"空科室", func(r *UpdateInstitutionRequest) { r.DepartmentIDs = &[]int64{} }, "科室"},
		{"非法科室 id", func(r *UpdateInstitutionRequest) { r.DepartmentIDs = &[]int64{0} }, "科室 ID"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			req := valid
			item.mutate(&req)
			if _, err := req.normalize(); err == nil || !strings.Contains(err.Error(), item.wantMsg) {
				t.Fatalf("期望错误包含 %q，实际 %v", item.wantMsg, err)
			}
		})
	}
}

// TestUpdateInstitutionRequestJSONContract 锁定前端 camelCase 字段名。
func TestUpdateInstitutionRequestJSONContract(t *testing.T) {
	raw := `{
		"name": "新机构", "legalName": "新机构有限公司", "industry": "口腔",
		"contactName": "李四", "contactTitle": "院长", "contactPhone": "13900000000",
		"contactEmail": "li@example.com", "provinceCode": "310000", "cityCode": "310100",
		"districtCode": "310104", "departmentIds": [1, 2, 3], "customerType": "group",
		"billingMode": "observe", "code": "CANT-CHANGE"
	}`
	var req UpdateInstitutionRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("解析编辑机构请求失败: %v", err)
	}
	normalized, err := req.normalize()
	if err != nil {
		t.Fatalf("编辑机构请求校验失败: %v", err)
	}
	if *normalized.Name != "新机构" || *normalized.LegalName != "新机构有限公司" ||
		*normalized.ProvinceCode != "310000" || len(*normalized.DepartmentIDs) != 3 {
		t.Fatalf("camelCase 字段未正确解析: %+v", normalized)
	}
	// billingMode / code 不是可写字段，反序列化后不应存在（结构体没有这两个字段）。
	if normalized.hasBaseTenantFields() != true || normalized.hasBillingProfileFields() != true {
		t.Fatalf("字段分组判断不对: %+v", normalized)
	}
}

// TestInstitutionUpdateSummary 变更日志只记录本次显式传入的字段。
func TestInstitutionUpdateSummary(t *testing.T) {
	before := institutionBaseRow{
		ID:       1,
		Name:     "老机构",
		Legal:    "老机构有限公司",
		Industry: "口腔",
		Contact:  "张三",
		Phone:    "13800000000",
		Email:    "old@example.com",
		Province: "310000",
		City:     "310100",
		District: "310104",
	}
	req := UpdateInstitutionRequest{
		Name:          strPtr("新机构"),
		DepartmentIDs: &[]int64{2, 1},
	}
	summaryBefore, summaryAfter := institutionUpdateSummary(before, req, []int64{1, 2})
	if len(summaryBefore) != 2 || len(summaryAfter) != 2 {
		t.Fatalf("摘要字段数不对: before=%v after=%v", summaryBefore, summaryAfter)
	}
	if summaryBefore["name"] != "老机构" || summaryAfter["name"] != "新机构" {
		t.Fatalf("名称摘要不对: %v -> %v", summaryBefore["name"], summaryAfter["name"])
	}
	if _, exists := summaryBefore["legalName"]; exists {
		t.Fatalf("未传字段不应进日志: %v", summaryBefore)
	}
}

// TestNormalizeCreateInstitutionAdminRequest 覆盖新增管理员入参校验。
func TestNormalizeCreateInstitutionAdminRequest(t *testing.T) {
	req := CreateInstitutionAdminRequest{
		Name:  " 王五 ",
		Phone: " 13700000000 ",
		Email: " wang@example.com ",
	}
	normalized, err := req.normalize()
	if err != nil {
		t.Fatalf("合法入参应通过: %v", err)
	}
	if normalized.Name != "王五" || normalized.Phone != "13700000000" || normalized.Role != institutionAdminRoleCode {
		t.Fatalf("字段清洗/默认角色不对: %+v", normalized)
	}

	badCases := []struct {
		name    string
		req     CreateInstitutionAdminRequest
		wantMsg string
	}{
		{"缺姓名", CreateInstitutionAdminRequest{Phone: "13700000000"}, "姓名"},
		{"手机号", CreateInstitutionAdminRequest{Name: "王五", Phone: "abc"}, "手机号"},
		{"邮箱", CreateInstitutionAdminRequest{Name: "王五", Phone: "13700000000", Email: "bad"}, "邮箱"},
		{"角色过长", CreateInstitutionAdminRequest{Name: "王五", Phone: "13700000000", Role: strings.Repeat("a", 65)}, "角色"},
	}
	for _, item := range badCases {
		t.Run(item.name, func(t *testing.T) {
			if _, err := item.req.normalize(); err == nil || !strings.Contains(err.Error(), item.wantMsg) {
				t.Fatalf("期望错误包含 %q，实际 %v", item.wantMsg, err)
			}
		})
	}
}

// TestNormalizeUpdateInstitutionAdminRequest 覆盖修改管理员入参校验。
func TestNormalizeUpdateInstitutionAdminRequest(t *testing.T) {
	req := UpdateInstitutionAdminRequest{Name: strPtr(" 李四 "), Email: strPtr(" li@example.com "), IsActive: boolPtr(false)}
	normalized, err := req.normalize()
	if err != nil {
		t.Fatalf("合法入参应通过: %v", err)
	}
	if *normalized.Name != "李四" || *normalized.Email != "li@example.com" || *normalized.IsActive {
		t.Fatalf("字段清洗不对: %+v", normalized)
	}
	if _, err := (UpdateInstitutionAdminRequest{Name: strPtr(" ")}).normalize(); err == nil {
		t.Fatalf("空姓名应被拒绝")
	}
	if _, err := (UpdateInstitutionAdminRequest{Email: strPtr("bad")}).normalize(); err == nil {
		t.Fatalf("非法邮箱应被拒绝")
	}
	if normalized, err := (UpdateInstitutionAdminRequest{}).normalize(); err != nil || normalized.Name != nil || normalized.Email != nil || normalized.IsActive != nil {
		t.Fatalf("空请求应保持 nil: %+v %v", normalized, err)
	}
}

// TestNormalizeInstitutionAdminRole 角色只接受 admin 系列。
func TestNormalizeInstitutionAdminRole(t *testing.T) {
	for _, raw := range []string{"", "admin", "ADMIN", "管理员", "主管理员"} {
		if code, ok := normalizeInstitutionAdminRole(raw); !ok || code != institutionAdminRoleCode {
			t.Fatalf("角色 %q 应归一为 admin，实际 %q %v", raw, code, ok)
		}
	}
	for _, raw := range []string{"doctor", "咨询师", "ops_super_admin"} {
		if _, ok := normalizeInstitutionAdminRole(raw); ok {
			t.Fatalf("角色 %q 不应被接受", raw)
		}
	}
}

// TestInstitutionAdminRowMapping 锁定详情与列表两种 DTO 的映射。
func TestInstitutionAdminRowMapping(t *testing.T) {
	lastLogin := time.Date(2026, 11, 10, 9, 30, 0, 0, time.UTC)
	rows := []institutionAdminRow{
		{ID: 1, Name: "张敏", Phone: "13800000001", Email: "zhang@example.com",
			RoleCode: "admin", RoleName: "管理员", IsPrimary: true, IsActive: true, LastLoginAt: &lastLogin},
		{ID: 2, Name: "李强", Phone: "13800000002",
			RoleCode: "admin", RoleName: "管理员", IsPrimary: false, IsActive: false},
	}

	accounts := make([]InstitutionAdminAccount, 0, len(rows))
	for _, row := range rows {
		accounts = append(accounts, institutionAdminAccountOf(row))
	}
	if accounts[0].Role != "管理员" || accounts[0].RoleCode != "admin" || !accounts[0].IsPrimary {
		t.Fatalf("主管理员映射不对: %+v", accounts[0])
	}
	if accounts[0].LastLoginAt != "2026-11-10 09:30" {
		t.Fatalf("最后登录时间格式不对: %q", accounts[0].LastLoginAt)
	}
	if accounts[1].LastLoginAt != "" || accounts[1].IsActive {
		t.Fatalf("未登录/停用映射不对: %+v", accounts[1])
	}

	admins := institutionAdminsFromRows(rows)
	if admins[0].Role != "主管理员" || admins[0].LastLogin != "2026-11-10 09:30" {
		t.Fatalf("机构详情管理员映射不对: %+v", admins[0])
	}
	if admins[1].Role != "管理员" || admins[1].LastLogin != "—" {
		t.Fatalf("未登录管理员应展示破折号: %+v", admins[1])
	}
}
